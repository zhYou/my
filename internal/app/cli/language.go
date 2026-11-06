package cli

import (
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/zhYou/my/internal/base/fault"
	"github.com/zhYou/my/internal/base/paths"
)

func canonicalLanguage(value string) (string, bool) {
	switch value {
	case "zh", languageChinese:
		return languageChinese, true
	case languageEnglish:
		return languageEnglish, true
	default:
		return "", false
	}
}

func usageProblem(code string, params fault.Params) error {
	return (fault.Definition{Code: fault.Code("cli." + code), Kind: fault.Usage}).New(fault.WithParams(params))
}

func languageEnvironment(opts Options) string {
	lookup := opts.LookupEnv
	if lookup == nil {
		lookup = os.LookupEnv
	}
	value, ok := lookup(paths.EnvPrefix(opts.Name) + "_LANG")
	if !ok {
		return ""
	}
	return value
}

// selectLanguage 依据命令树选项元数据跳过值，不把值中的 --lang 当选项。
func selectLanguage(root *cobra.Command, args []string, env string) (string, error) {
	index, completion := completionRequest(root, args)
	if completion {
		// 最后一个 token 正在输入，不参与语言选择。
		end := len(args)
		if end > index+1 {
			end--
		}
		complete := append([]string(nil), args[:index]...)
		args = append(complete, args[index+1:end]...)
		if _, ok := canonicalLanguage(env); !ok {
			env = languageEnglish
		}
	}
	selected := env
	if selected == "" {
		selected = languageEnglish
	}
	// 与 Execute 使用相同的命令查找，子命令选项也可能位于命令名之前。
	// Find 不执行选项 Set；查找错误留给实际执行路径处理。
	//nolint:errcheck // 仅预选错误呈现语言，查找错误留给实际 Execute 路径处理。
	current, scan, _ := root.Find(args)
	findFlag := func(name string, short bool) *pflag.Flag {
		return commandFlag(current, name, short)
	}

	for i := 0; i < len(scan); i++ {
		arg := scan[i]
		if arg == "--" {
			break
		}
		if after, ok := strings.CutPrefix(arg, "--"); ok {
			name, value, hasValue := strings.Cut(after, "=")
			flag := findFlag(name, false)
			if flag == nil {
				continue
			}
			if !hasValue && flag.NoOptDefVal == "" {
				if i+1 == len(scan) {
					if completion {
						break
					}
					continue
				}
				i++
				value = scan[i]
			}
			if name == flagLanguage {
				if _, valid := canonicalLanguage(value); !completion || valid {
					selected = value
				}
			}
		} else if strings.HasPrefix(arg, "-") && arg != "-" {
			short := strings.TrimPrefix(arg, "-")
			for j := 0; j < len(short); j++ {
				f := findFlag(short[j:j+1], true)
				if f != nil && f.NoOptDefVal == "" {
					if j+1 == len(short) {
						i++
					}
					break
				}
			}
		}
	}
	if lang, ok := canonicalLanguage(selected); ok {
		return lang, nil
	}
	fallback := languageEnglish
	if lang, ok := canonicalLanguage(env); ok {
		fallback = lang
	}
	return fallback, usageProblem("language", fault.Params{paramValue: strconv.Quote(selected)})
}

type languageValue struct {
	call        *invocation
	root        *cobra.Command
	preselected bool
	completing  bool
}

func (v *languageValue) String() string {
	return v.call.localizer.Language()
}

func (v *languageValue) Type() string {
	return "string"
}

func (v *languageValue) Set(s string) error {
	lang, ok := canonicalLanguage(s)
	if !ok {
		if v.completing {
			return nil
		}
		return usageProblem("language", fault.Params{paramValue: strconv.Quote(s)})
	}
	// Run 已按完整 argv 选择最后生效的语言；解析中间值时只校验。
	if v.preselected {
		return nil
	}
	if err := v.call.localizeTree(v.root, lang); err != nil {
		return fault.Wrap(fault.Runtime, "", err)
	}
	return nil
}

// completionRequest 只识别顶层协议命令，使用选项元数据跳过参数值。
func completionRequest(root *cobra.Command, args []string) (int, bool) {
	//nolint:errcheck // 这里只定位补全协议命令，实际参数错误由独立解析树及 Execute 处理。
	target, _, _ := root.Find(args)
	if target == nil {
		target = root
	}
	lookup := func(name string, short bool) *pflag.Flag {
		return commandFlag(target, name, short)
	}

	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			return 0, false
		}
		switch {
		case strings.HasPrefix(arg, "--"):
			name, _, equals := strings.Cut(arg[2:], "=")
			flag := lookup(name, false)
			if flag == nil {
				return 0, false
			}
			if !equals && flag.NoOptDefVal == "" {
				i++
			}
		case strings.HasPrefix(arg, "-") && arg != "-":
			for j := 1; j < len(arg); j++ {
				flag := lookup(arg[j:j+1], true)
				if flag == nil {
					return 0, false
				}
				// -h=false 等带等号的 bool 值不是后续短选项。
				if j+1 < len(arg) && arg[j+1] == '=' {
					break
				}
				if flag.NoOptDefVal == "" {
					if j+1 == len(arg) {
						i++
					}
					break
				}
			}
		default:
			return i, arg == cobra.ShellCompRequestCmd || arg == cobra.ShellCompNoDescRequestCmd
		}
	}
	return 0, false
}

func commandFlag(target *cobra.Command, name string, short bool) *pflag.Flag {
	for cmd := target; cmd != nil; cmd = cmd.Parent() {
		for _, set := range []*pflag.FlagSet{cmd.Flags(), cmd.PersistentFlags()} {
			var flag *pflag.Flag
			if short {
				flag = set.ShorthandLookup(name)
			} else {
				flag = set.Lookup(name)
			}
			if flag != nil {
				return flag
			}
		}
	}
	return nil
}
