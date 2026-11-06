package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestChineseUsageAndErrors(t *testing.T) {
	for _, tc := range []struct {
		args []string
		code int
		want string
	}{
		{[]string{"--help"}, 0, "用法："},
		{[]string{"extra"}, 2, "不接受位置参数"},
		{[]string{"--unknown"}, 2, "解析参数失败"},
		{[]string{"--name"}, 2, "解析参数失败"},
		{nil, 1, "名称必须"},
	} {
		var out, errOut bytes.Buffer
		code := run(append([]string{"init"}, tc.args...), t.TempDir(), &out, &errOut)
		if code != tc.code || !strings.Contains(out.String()+errOut.String(), tc.want) {
			t.Errorf("参数=%v 退出码=%d 标准输出=%q 标准错误=%q", tc.args, code, out.String(), errOut.String())
		}
		if code != 0 && (out.Len() != 0 || !strings.HasPrefix(errOut.String(), "错误：")) {
			t.Errorf("错误输出约定不符：标准输出=%q 标准错误=%q", out.String(), errOut.String())
		}
	}
}
