package locales

import (
	"embed"
	"io/fs"
	"sync"

	"github.com/zhYou/my/internal/base/messages"
)

//go:embed active.en.toml active.zh-CN.toml
var catalog embed.FS

// Catalog 返回不可变的内置目录。
func Catalog() fs.FS {
	return catalog
}

var loadBundle = sync.OnceValues(func() (*messages.Bundle, error) {
	return messages.NewBundle(Catalog())
})

// LoadBundle 共享首次加载的结果和错误；Localizer 按调用单独创建。
func LoadBundle() (*messages.Bundle, error) {
	return loadBundle()
}
