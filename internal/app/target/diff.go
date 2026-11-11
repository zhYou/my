package target

import (
	"github.com/aymanbagabas/go-udiff"
)

// UnifiedTextDiff 生成非秘密文本的统一 diff。
func UnifiedTextDiff(name string, before, after []byte) string {
	return udiff.Unified(name+"/local", name+"/remote", string(before), string(after))
}
