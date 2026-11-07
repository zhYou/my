// Package datarepo 通过 GitHub API 读取私有数据仓快照，并以预期 HEAD 提交变更。
//
// 凭据、HTTP 传输和诊断依赖由调用方注入；不维护 Git 工作树或缓存，
// 不可信响应通过敏感原因保护保留错误链，提交结果未知时不自动重发。
package datarepo
