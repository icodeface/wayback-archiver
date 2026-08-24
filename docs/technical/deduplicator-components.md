# 归档组件职责

服务端将原 `Deduplicator` 的资源处理职责拆为可独立测试的组件。`Deduplicator` 目前作为兼容 façade 保留旧方法，新增代码应通过组件访问器或直接构造组件。

| 组件 | 输入 | 输出 | 责任 |
| --- | --- | --- | --- |
| `ResourceDownloader` | `ResourceDownloadRequest` | `ResourceDownloadResult` | SSRF 校验后的 HTTP 下载、条件请求、有限重试、内存/临时文件结果 |
| `ResourceDeduplicator` | `ResourceProcessRequest` | `ResourceProcessResult` | URL/内容哈希去重、资源记录和文件复用 |
| `ResourceCache` | URL + `CachedResource` | 缓存命中/淘汰状态 | freshness、ETag、Last-Modified 元数据缓存，TTL 和容量淘汰 |
| `HTMLRewriter` | `HTMLRewriteRequest` | `HTMLRewriteResult` | HTML/CSS 资源提取、归档路径重写、资源 ID 汇总 |
| `PageArchiver` | `CaptureRequest` | page/action 或 error | 页面创建、更新、异步任务和失败回滚编排 |
| `ConcurrencyManager` | worker 数及 key | unlock 函数 | 全局下载并发、页面创建锁、资源 URL 锁 |

## 依赖方向

`PageArchiver` 调用 `HTMLRewriter`；`HTMLRewriter` 使用 `ResourceDeduplicator`；后者通过 `ResourceDownloader` 获取内容，并使用 `ResourceCache` 和数据库完成复用判断。所有下载路径共享 `ConcurrencyManager` 的信号量。

组件的 request/result 类型不暴露数据库内部模型，便于编写纯单元测试并在未来替换存储实现。
