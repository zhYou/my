# 安全问题报告

请勿在公开 Issue 或 PR 中披露尚未修复的漏洞、密钥或可利用细节。

私密报告入口：[GitHub 私密漏洞报告](https://github.com/zhYou/my/security/advisories/new)。

本仓库已启用 GitHub Private vulnerability reporting。维护责任人和报告接收人为 **zhYou**。
当前处于工程初始化阶段，尚未发布正式版本；报告请注明对应提交。

报告请包含受影响版本、复现条件、影响与最小示例；不要发送实际密钥或无关个人数据。
维护者收到报告后确认影响、准备修复并协调披露，响应窗口与支持版本由目标项目自行公布。

工具扫描可运行 `mise exec -- govulncheck ./...`。漏洞数据库访问失败同样视为检查失败。
维护责任和托管设置见[维护清单](docs/maintenance.md)。
