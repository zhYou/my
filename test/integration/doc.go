// Package integration 通过真实子进程验证 CLI 契约和消息目录故障边界。
//
// 测试隔离应用 HOME、配置与状态；损坏消息目录场景通过 Go overlay 重建测试
// 程序，不修改真实语言资源或用户诊断数据。
package integration
