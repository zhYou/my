// Package iostreams 定义显式注入的输入输出流及共享 writer 的串行包装。
//
// 缺省输入使用 EOF，缺省输出丢弃；本包不访问或关闭进程标准流。
// 多个调用者共享原始 writer 时，需要协调使用同一写锁。
package iostreams
