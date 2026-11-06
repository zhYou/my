// Package locales 内嵌集中维护的双语 TOML 消息目录，并提供冻结的共享 Bundle。
//
// 每次调用从 Bundle 创建独立 Localizer；不支持运行时翻译覆盖或系统区域推断。
package locales
