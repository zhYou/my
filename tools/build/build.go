package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/zhYou/my/tools/internal/scaffold"
	"github.com/zhYou/my/tools/internal/toolrun"
)

func build() error {
	project, err := scaffold.Inspect(".")
	if err != nil {
		return err
	}
	//nolint:gosec // G301：这是公开构建产物或机器契约文件的既定权限，不包含凭据。
	if err := os.MkdirAll("bin", 0o755); err != nil {
		return err
	}
	name := project.Name
	if runtime.GOOS == windowsPlatform {
		name += executableSuffix
	}
	version, commit := developmentVersion, developmentCommit
	if data, err := exec.Command(gitProgram, "describe", "--tags", "--always", "--dirty").Output(); err == nil {
		version = strings.TrimSpace(string(data))
	}
	if data, err := exec.Command(gitProgram, gitRevParse, "--short", "HEAD").Output(); err == nil {
		commit = strings.TrimSpace(string(data))
	}
	flags := fmt.Sprintf(
		"-s -w -X main.version=%s -X main.commit=%s -X main.date=%s",
		version,
		commit,
		time.Now().UTC().Format(time.RFC3339),
	)
	return toolrun.Run(
		"",
		nil,
		"go",
		buildCommand,
		"-trimpath",
		"-ldflags",
		flags,
		"-o",
		filepath.Join("bin", name),
		"./"+project.CommandPath,
	)
}
