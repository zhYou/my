// Package privatefs 提供受管状态文件的安全读取、私有目录校验与原子写入。
//
// 读取不会创建目录，拒绝符号链接、特殊文件及过宽的 Unix 权限，并限制读取大小。
// 调用方决定逻辑锁和业务错误；原子写入拥有临时文件，返回关闭或清理错误。
// Windows 同样校验类型，但不把 Unix mode 位视为 ACL 证明。
package privatefs
