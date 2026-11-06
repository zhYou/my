package scaffold

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/spf13/afero"
	"golang.org/x/mod/semver"
)

// Origin 保存模板来源及生成项目的身份。
type Origin struct {
	FormatVersion   int    `json:"format_version"`
	Template        string `json:"template"`
	TemplateVersion string `json:"template_version"`
	SourceCommit    string `json:"source_commit"`
	Module          string `json:"module"`
	Name            string `json:"name"`
}

// ReadOrigin 读取并校验项目来源记录。
func ReadOrigin(fs afero.Fs, root string) (Origin, error) {
	var origin Origin
	i := Initializer{FS: fs}
	path, err := i.safePath(root, originFile)
	if err != nil {
		return origin, err
	}
	data, err := afero.ReadFile(fs, path)
	if err != nil {
		return origin, err
	}
	if err := json.Unmarshal(data, &origin); err != nil {
		return origin, err
	}
	if origin.FormatVersion != 1 || origin.Template != TemplateModule || !semver.IsValid("v"+origin.TemplateVersion) ||
		origin.SourceCommit == "" ||
		origin.Module == "" ||
		origin.Name == "" {
		return origin, fmt.Errorf("模板来源记录无效")
	}
	return origin, nil
}

func makeOrigin(root, module, name string) (Origin, error) {
	i := (&Initializer{}).defaults()
	return i.origin(context.Background(), root, module, name)
}

func (i *Initializer) origin(ctx context.Context, root, module, name string) (Origin, error) {
	path, err := i.safePath(root, ".template-version")
	if err != nil {
		return Origin{}, err
	}
	data, err := afero.ReadFile(i.FS, path)
	if err != nil {
		return Origin{}, err
	}
	version := strings.TrimSpace(string(data))
	if !semver.IsValid("v" + version) {
		return Origin{}, fmt.Errorf("模板版本无效：%q", version)
	}
	origin := Origin{1, TemplateModule, version, "unknown", module, name}
	if _, ok := i.FS.(*afero.OsFs); !ok {
		return origin, nil
	}
	run := func(args ...string) string {
		out, err := i.RunCommand(ctx, "git", append([]string{"-C", root}, args...)...)
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(out))
	}
	remote := strings.TrimSuffix(run("remote", "get-url", "origin"), ".git")
	if remote != "https://github.com/zhYou/go-cli-starter" && remote != "git@github.com:zhYou/go-cli-starter" {
		return origin, nil
	}
	if run(gitShow, "HEAD:.template-version") != version ||
		!strings.Contains(run(gitShow, "HEAD:go.mod"), "module "+TemplateModule) ||
		!strings.Contains(
			run(gitShow, "HEAD:cmd/"+TemplateName+"/main.go"),
			"appName = "+fmt.Sprintf("%q", TemplateName),
		) {
		return origin, nil
	}
	commit := run("rev-parse", "HEAD")
	if regexp.MustCompile(`^[0-9a-f]{40,64}$`).MatchString(commit) {
		origin.SourceCommit = commit
	}
	return origin, nil
}

func (i *Initializer) originChange(ctx context.Context, root, module, name string) (*change, error) {
	if _, err := i.FS.Stat(filepath.Join(root, ".template-version")); os.IsNotExist(err) {
		return nil, nil
	}
	path, err := i.safePath(root, originFile)
	if err != nil {
		return nil, err
	}
	if _, err := i.FS.Stat(path); err == nil {
		return nil, fmt.Errorf("原始模板中已有来源记录，拒绝覆盖")
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	origin, err := i.origin(ctx, root, module, name)
	if err != nil {
		return nil, err
	}
	data, err := json.MarshalIndent(origin, "", "  ")
	if err != nil {
		return nil, err
	}
	return &change{path, append(data, '\n'), 0o644}, nil
}
