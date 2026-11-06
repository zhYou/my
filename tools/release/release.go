package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

func releaseFile(dist, name string) (string, error) {
	if name == "" || filepath.Base(name) != name ||
		!regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`).MatchString(name) ||
		name == ".." {
		return "", fmt.Errorf("不安全的发布路径 %q", name)
	}
	path := filepath.Join(dist, name)
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("发布产物必须为常规文件：%s", name)
	}
	return path, nil
}

// releaseSnapshot 仅在全部清单资产、六目标归档和 SPDX 校验成功后返回。
type releaseSnapshot struct {
	version   string
	checksums map[string]string
}

func checkReleaseSnapshot(dist string) error {
	_, err := validateReleaseSnapshot(dist)
	return err
}

func validateReleaseSnapshot(dist string) (_ *releaseSnapshot, resultErr error) {
	path, err := releaseFile(dist, "checksums.txt")
	if err != nil {
		return nil, err
	}
	//nolint:gosec // G304：开发任务只读取调用者指定的项目目录或本地构建 fixture，不作为服务接收外部路径。
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() {
		resultErr = errors.Join(resultErr, file.Close())
	}()
	checksums := map[string]string{}
	scanner := bufio.NewScanner(file)
	linePattern := regexp.MustCompile(`^([0-9a-f]{64}) [ *]([^\r\n]+)$`)
	for scanner.Scan() {
		line := scanner.Text()
		match := linePattern.FindStringSubmatch(line)
		if len(match) != 3 {
			return nil, fmt.Errorf("无效的 SHA-256 校验行：%q", line)
		}
		name := match[2]
		if _, ok := checksums[name]; ok {
			return nil, fmt.Errorf("校验清单包含重复路径：%s", name)
		}
		path, err := releaseFile(dist, name)
		if err != nil {
			return nil, err
		}
		//nolint:gosec // G304：开发任务只读取调用者指定的项目目录或本地构建 fixture，不作为服务接收外部路径。
		input, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		hash := sha256.New()
		_, copyErr := io.Copy(hash, input)
		closeErr := input.Close()
		if copyErr != nil {
			return nil, copyErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
		if hex.EncodeToString(hash.Sum(nil)) != match[1] {
			return nil, fmt.Errorf("发布产物摘要不符：%s", name)
		}
		checksums[name] = match[1]
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	pattern := regexp.MustCompile(
		"^" + regexp.QuoteMeta(appName) + `_(.+)_(linux|darwin|windows)_(amd64|arm64)\.(tar\.gz|zip)$`,
	)
	targets := map[string]bool{}
	version := ""
	for name := range checksums {
		match := pattern.FindStringSubmatch(name)
		if len(match) == 0 {
			continue
		}
		target := match[2] + "_" + match[3]
		if targets[target] {
			return nil, fmt.Errorf("目标归档重复：%s", target)
		}
		targets[target] = true
		if version == "" {
			version = match[1]
		} else if version != match[1] {
			return nil, fmt.Errorf("发布归档版本不一致")
		}
		if match[2] == windowsPlatform && match[4] != "zip" || match[2] != windowsPlatform && match[4] != "tar.gz" {
			return nil, fmt.Errorf("归档格式与平台不符：%s", name)
		}
		binary := appName
		if match[2] == windowsPlatform {
			binary += executableSuffix
		}
		if err := validateArchive(filepath.Join(dist, name), binary); err != nil {
			return nil, fmt.Errorf("发布归档 %s 无效：%w", name, err)
		}
		sbom := name + ".spdx.json"
		if _, ok := checksums[sbom]; !ok {
			return nil, fmt.Errorf("归档缺少已校验 SBOM：%s", name)
		}
		if err := validateSPDX(filepath.Join(dist, sbom), name); err != nil {
			return nil, err
		}
	}
	if len(targets) != 6 {
		return nil, fmt.Errorf("必须包含六目标归档及其 SBOM，实际 %d 个", len(targets))
	}
	return &releaseSnapshot{version: version, checksums: checksums}, nil
}

func validateSPDX(path, archive string) error {
	//nolint:gosec // G304：开发任务只读取调用者指定的项目目录或本地构建 fixture，不作为服务接收外部路径。
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var doc struct {
		Version   string `json:"spdxVersion"`
		ID        string `json:"SPDXID"`
		Name      string `json:"name"`
		Namespace string `json:"documentNamespace"`
		Creation  struct {
			Creators []string `json:"creators"`
			Created  string   `json:"created"`
		} `json:"creationInfo"`
		Packages []struct {
			ID   string `json:"SPDXID"`
			Name string `json:"name"`
		} `json:"packages"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return err
	}
	uri, err := url.Parse(doc.Namespace)
	if err != nil || !uri.IsAbs() || !strings.HasPrefix(doc.Version, "SPDX-2.") || doc.ID != "SPDXRef-DOCUMENT" ||
		doc.Name != archive ||
		len(doc.Creation.Creators) == 0 ||
		doc.Creation.Created == "" ||
		len(doc.Packages) == 0 {
		return fmt.Errorf("归档 %s 的 SPDX 文档结构或身份无效", archive)
	}
	for _, p := range doc.Packages {
		if p.ID == "" || p.Name == "" {
			return fmt.Errorf("归档 %s 的 SPDX 包缺少标识", archive)
		}
	}
	return nil
}
