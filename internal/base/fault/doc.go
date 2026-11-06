// Package fault 提供包含领域码、分类、参数与原始原因的结构化错误。
//
// 错误保留标准错误链，调用方可通过 errors.Is 和 errors.As 检查原因，
// 再由入口根据分类决定呈现方式与退出码。
package fault
