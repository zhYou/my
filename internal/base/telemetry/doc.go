// Package telemetry 为命令提供显式创建、独立关闭的 OpenTelemetry Trace 管线。
//
// 默认关闭采集，file 模式仅写本地诊断 JSON，使用私有 provider。
// 本包隔离远端 exporter 环境并有界收尾，不替换全局 provider。
package telemetry
