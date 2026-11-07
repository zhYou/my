// Package machine 管理机器登记、当前设备绑定及数据仓初始化。
//
// 远端快照与提交、本机状态、时钟和观测依赖显式注入；先确认远端再更新本机，
// 部分完成通过 fault.Diagnostic 报告，重复调用核实远端事实后幂等补齐。
package machine
