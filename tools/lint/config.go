package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
	"github.com/zhYou/my/internal/app/config"
)

const (
	checkPathError  = "检查 %s: %w"
	notRegularError = "%s 不是普通文件"
)

// checkConfig 检查受管应用配置的格式与内容。root 是仓库根目录。
func checkConfig(root string) error {
	root, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("解析仓库路径: %w", err)
	}

	// 示例是模板提供的应用配置，不能随目录一起消失。
	example := filepath.Join(root, "configs", "config.example.toml")
	info, err := os.Lstat(example)
	if err != nil {
		return fmt.Errorf(checkPathError, example, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf(notRegularError, example)
	}

	if err := checkDirectory(root, "configs", false, false); err != nil {
		return err
	}
	if err := checkDirectory(root, filepath.Join("internal", "locales"), true, true); err != nil {
		return err
	}
	return checkRegisteredRootFiles(root)
}

func checkDirectory(root, relative string, optional, locale bool) error {
	dir := filepath.Join(root, relative)
	info, err := os.Lstat(dir)
	if err != nil {
		if optional && errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return fmt.Errorf(checkPathError, dir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s 不是普通目录", dir)
	}
	fixtureDir := filepath.Join(dir, testdataDirectory)
	return filepath.WalkDir(dir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return fmt.Errorf(checkPathError, path, walkErr)
		}
		if entry.IsDir() {
			if path == fixtureDir {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf(checkPathError, path, err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf(notRegularError, path)
		}
		return checkFile(path, locale)
	})
}

func checkRegisteredRootFiles(root string) error {
	entries, err := os.ReadDir(root)
	if err != nil {
		return fmt.Errorf("读取仓库 %s: %w", root, err)
	}
	for _, entry := range entries {
		name := strings.ToLower(entry.Name())
		switch name {
		case "config.toml", "config.json", "config.yaml", "config.yml",
			"config.example.toml", "config.example.json", "config.example.yaml", "config.example.yml":
			path := filepath.Join(root, entry.Name())
			info, err := entry.Info()
			if err != nil {
				return fmt.Errorf(checkPathError, path, err)
			}
			if !info.Mode().IsRegular() {
				return fmt.Errorf(notRegularError, path)
			}
			if err := checkFile(path, false); err != nil {
				return err
			}
		}
	}
	return nil
}

func checkFile(path string, locale bool) error {
	ext := strings.ToLower(filepath.Ext(path))
	if locale && ext == ".go" {
		return nil
	}
	if ext != ".toml" {
		return fmt.Errorf("%s: 受管配置仅支持 TOML", path)
	}
	if !locale {
		_, err := config.Load(context.Background(), config.Options{
			Path:     path,
			Explicit: true,
			LookupEnv: func(string) (string, bool) {
				return "", false
			},
		})
		if err != nil {
			return fmt.Errorf("应用配置 %s 无效: %w", path, err)
		}
		return nil
	}
	//nolint:gosec // G304：路径来自调用者指定的仓库及受管目录遍历。
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("读取 %s: %w", path, err)
	}
	var values map[string]any
	if err := toml.Unmarshal(data, &values); err != nil {
		return fmt.Errorf("语言目录 %s 的 TOML 无效: %w", path, err)
	}
	return nil
}
