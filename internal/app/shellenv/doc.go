// Package shellenv 管理数据仓权威的普通和秘密环境变量，以及按机器隔离的离线缓存。
//
// Set 和 Remove 通过 Repository 的固定快照与 CAS 发布，再原子更新调用者提供的 Cache；
// List 只返回名称、敏感性与适用机器。秘密值只进入加密边界；普通变更保留既有秘密版本。
// 身份只在实际加解密时加载，SyncCache 可以仅下载密文。
//
// Activate 只读缓存，先完整校验再渲染 shell 赋值，不访问网络或创建诊断文件。
// 调用者负责把输出接入自己的 shell 启动文件，本包不修改系统或运行中应用的环境。
package shellenv
