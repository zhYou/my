package cli

import (
	"fmt"
	"strconv"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/zhYou/my/internal/base/messages"
)

type textMessage struct {
	id     string
	params map[string]any
}

type commandTexts struct {
	short   textMessage
	long    textMessage
	use     textMessage
	example textMessage
}

type commandTextBinding struct {
	command *cobra.Command
	name    string
	texts   commandTexts
}

type flagTextBinding struct {
	flag *pflag.Flag
	text textMessage
}

func message(id string) textMessage {
	return textMessage{id: id}
}

func (call *invocation) bindCommand(cmd *cobra.Command, texts commandTexts) {
	call.commandTexts = append(call.commandTexts, commandTextBinding{command: cmd, name: cmd.Name(), texts: texts})
}

func (call *invocation) bindFlag(flag *pflag.Flag, text textMessage) {
	call.flagTexts = append(call.flagTexts, flagTextBinding{flag: flag, text: text})
}

// localizeTree 先完整渲染，再将文案应用到本次调用的具体命令和选项。
func (call *invocation) localizeTree(root *cobra.Command, language string) error {
	localizer := call.bundle.Localizer(language)
	format := func(text textMessage) (string, error) {
		if text.id == "" {
			return "", nil
		}
		return localizer.Format(text.id, messages.FormatOptions{Params: text.params})
	}
	type renderedCommand struct {
		short   string
		long    string
		use     string
		example string
	}
	commands := make([]renderedCommand, len(call.commandTexts))
	for i, binding := range call.commandTexts {
		fields := []struct {
			text textMessage
			dest *string
		}{
			{binding.texts.short, &commands[i].short},
			{binding.texts.long, &commands[i].long},
			{binding.texts.use, &commands[i].use},
			{binding.texts.example, &commands[i].example},
		}
		for _, field := range fields {
			text, err := format(field.text)
			if err != nil {
				return err
			}
			*field.dest = text
		}
		if binding.texts.use.id != "" {
			candidate := &cobra.Command{Use: commands[i].use}
			if name := candidate.Name(); name == "" || name != binding.name {
				return fmt.Errorf("message %q changes command name %q", binding.texts.use.id, binding.name)
			}
		}
	}
	flags := make([]string, len(call.flagTexts))
	for i, binding := range call.flagTexts {
		text, err := format(binding.text)
		if err != nil {
			return err
		}
		flags[i] = text
	}
	type versionBinding struct {
		command *cobra.Command
		text    string
	}
	var versions []versionBinding
	var collectVersions func(*cobra.Command) error
	collectVersions = func(cmd *cobra.Command) error {
		if cmd == root || cmd.Version != "" {
			version, err := localizer.Format(
				"cli.version_template",
				messages.FormatOptions{Params: map[string]any{paramName: cmd.Name(), "Version": cmd.Version}},
			)
			if err != nil {
				return err
			}
			versions = append(versions, versionBinding{command: cmd, text: version})
		}
		for _, child := range cmd.Commands() {
			if err := collectVersions(child); err != nil {
				return err
			}
		}
		return nil
	}
	if err := collectVersions(root); err != nil {
		return err
	}
	for i, binding := range call.commandTexts {
		if binding.texts.short.id != "" {
			binding.command.Short = commands[i].short
		}
		if binding.texts.long.id != "" {
			binding.command.Long = commands[i].long
		}
		if binding.texts.use.id != "" {
			binding.command.Use = commands[i].use
		}
		if binding.texts.example.id != "" {
			binding.command.Example = commands[i].example
		}
	}
	for i, binding := range call.flagTexts {
		binding.flag.Usage = flags[i]
	}
	// Cobra 解析固定字符串字面量；渲染结果中的分隔符不会被再次解释。
	for _, version := range versions {
		version.command.SetVersionTemplate("{{" + strconv.Quote(version.text+"\n") + "}}")
	}
	call.localizer = localizer
	return nil
}
