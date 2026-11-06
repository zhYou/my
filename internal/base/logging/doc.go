// Package logging 提供每次命令独立的结构化日志，封装 zap 后端。
//
// 日志通过显式 writer 和本地化依赖输出，结构化事件保持稳定机器字段。
// 调用方通过错误报告回调处理诊断故障，不在日志层退出进程。
package logging
