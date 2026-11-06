// Package session 在应用入口组装日志、Trace 和本地诊断文件的生命周期。
//
// Session 登记并等待后台工作，收尾时记录结果、结束根 span、排空 Trace 并关闭
// 自有资源。借用的 logger 和 IO 不由 Session 同步或关闭。
package session
