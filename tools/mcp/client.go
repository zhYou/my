package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/zhYou/my/internal/base/iostreams"
	cmdexec "github.com/zhYou/my/internal/command"
)

type mcpClient struct {
	encoder *json.Encoder
	decoder *json.Decoder
	id      int
	cause   func() error
}

func startMCP(parent context.Context, root, task string, env ...string) (*mcpClient, func() error, error) {
	// 使用操作系统管道，执行器直接传递句柄；避免借用 io.Pipe 的复制 goroutine 等待环。
	requests, input, err := os.Pipe()
	if err != nil {
		return nil, nil, err
	}
	output, responses, err := os.Pipe()
	if err != nil {
		return nil, nil, errors.Join(err, requests.Close(), input.Close())
	}
	ctx, cancel := context.WithCancel(parent)
	done := make(chan error, 1)
	go func() {
		err := cmdexec.Run(ctx, cmdexec.Invocation{Program: miseProgram, Args: []string{"run", quietFlag, task}},
			cmdexec.Execution{Dir: root, IO: iostreams.IOStreams{In: requests, Out: responses, ErrOut: os.Stderr}}.
				WithEnv(env...))
		// 不论启动失败、提前退出或取消，都关闭父进程保留的子端，解除协议读写。
		done <- errors.Join(err, requests.Close(), responses.Close())
	}()
	closeClient := func() error {
		drained := make(chan error, 1)
		go func() {
			// 停止请求后排空尾部消息，避免服务处理 EOF 时因输出填满而阻塞。
			_, err := io.Copy(io.Discard, output)
			drained <- err
		}()
		inputErr := input.Close()
		timer := time.NewTimer(10 * time.Second)
		defer timer.Stop()
		var runErr error
		select {
		case runErr = <-done:
		case <-timer.C:
			cancel()
			runErr = <-done
		}
		cancel()
		return errors.Join(inputErr, runErr, <-drained, output.Close())
	}
	return &mcpClient{
		encoder: json.NewEncoder(input), decoder: json.NewDecoder(output),
		cause: func() error {
			return context.Cause(ctx)
		},
	}, closeClient, nil
}

func (c *mcpClient) request(method string, params any) (result json.RawMessage, resultErr error) {
	defer func() {
		if resultErr != nil && c.cause != nil {
			resultErr = errors.Join(resultErr, c.cause())
		}
	}()
	c.id++
	if err := c.encoder.Encode(
		map[string]any{"jsonrpc": "2.0", "id": c.id, "method": method, "params": params},
	); err != nil {
		return nil, err
	}
	for {
		var response struct {
			ID     int             `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  json.RawMessage `json:"error"`
		}
		if err := c.decoder.Decode(&response); err != nil {
			return nil, fmt.Errorf("读取 %s 响应：%w", method, err)
		}
		if response.ID != c.id {
			continue
		}
		if len(response.Error) != 0 && string(response.Error) != "null" {
			return nil, fmt.Errorf("%s 调用失败：%s", method, response.Error)
		}
		return response.Result, nil
	}
}

func (c *mcpClient) initialize(requiredTools ...string) error {
	if _, err := c.request("initialize", map[string]any{
		"protocolVersion": "2025-03-26", "capabilities": map[string]any{},
		"clientInfo": map[string]string{"name": "template-check", mcpVersionField: "1"},
	}); err != nil {
		return err
	}
	if err := c.encoder.Encode(map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"}); err != nil {
		return err
	}
	required := make(map[string]bool, len(requiredTools))
	for _, name := range requiredTools {
		required[name] = false
	}
	params := map[string]any{}
	for {
		raw, err := c.request("tools/list", params)
		if err != nil {
			return err
		}
		var catalog struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
			Next string `json:"nextCursor"`
		}
		if err := json.Unmarshal(raw, &catalog); err != nil {
			return err
		}
		for _, tool := range catalog.Tools {
			required[tool.Name] = true
		}
		if catalog.Next == "" {
			break
		}
		params["cursor"] = catalog.Next
	}
	for name, found := range required {
		if !found {
			return fmt.Errorf("缺少 MCP 必要工具 %s", name)
		}
	}
	return nil
}

func (c *mcpClient) tool(name string, args map[string]any) (string, error) {
	raw, err := c.request("tools/call", map[string]any{"name": name, "arguments": args})
	if err != nil {
		return "", err
	}
	text, err := toolText(raw)
	if err != nil {
		return "", fmt.Errorf("%s：%w", name, err)
	}
	return text, nil
}

func (c *mcpClient) activate(path, session string) error {
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil {
		return err
	}
	text, err := c.tool("activate_project", map[string]any{"project": canonical, "session": session})
	if err != nil {
		return err
	}
	if !strings.Contains(text, canonical) {
		return fmt.Errorf("激活结果未确认实际 checkout %s：%s", canonical, text)
	}
	return nil
}

func (c *mcpClient) overview(path, marker string) error {
	text, err := c.tool("get_symbols_overview", map[string]any{relativePathField: path, "depth": 1})
	if err != nil {
		return err
	}
	if !strings.Contains(text, marker) {
		return fmt.Errorf("%s 未返回预期符号 %s：%s", path, marker, text)
	}
	return nil
}

func toolText(raw json.RawMessage) (string, error) {
	var result struct {
		IsError bool `json:"isError"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", err
	}
	var parts []string
	for _, content := range result.Content {
		if content.Type == "text" {
			parts = append(parts, content.Text)
		}
	}
	text := strings.Join(parts, "\n")
	if result.IsError {
		return "", fmt.Errorf("工具执行失败：%s", text)
	}
	return text, nil
}

func sessionFromInstructions(text string) (string, error) {
	match := regexp.MustCompile("Your Serena session id is `([^`]+)`").FindStringSubmatch(text)
	if len(match) != 2 {
		return "", errors.New("instructions 未提供 Serena session id")
	}
	return match[1], nil
}
