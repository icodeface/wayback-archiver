# 快照与更新机制

本文档描述浏览器端捕获页面快照、持续更新、以及服务端处理的完整流程。

## 整体架构

```
浏览器 (Tampermonkey)                    服务端 (Go + PostgreSQL)
─────────────────────                    ──────────────────────
页面加载 → 捕获快照 → POST /api/archive → ProcessCapture()
                ↓                              ↓
         DOM 监听 + 定时上传              哈希去重 / 资源处理 / URL 重写
                ↓                              ↓
         PUT /api/archive/:id  ────→    UpdateCapture()（全量覆盖）
```

## 一、初次快照

### 触发时机

页面加载后延迟 `DOM_STABILITY_DELAY`（2 秒），然后开始捕获。

### 流程

```
initializeArchiver()
  ├─ 立即启动 collectorObserver（收集虚拟滚动移除的节点）
  ├─ 等待 DOM_STABILITY_DELAY (2s)
  └─ prepareCapture()
       ├─ waitForDOMStable()     等待 DOM 不再变化（超时 10s，稳定阈值 1s）
       ├─ 收集 cookies           通过 GM_cookie 获取 HttpOnly cookies
       ├─ serializeCSSOMToDOM()  将动态注入的 CSS 规则写入 <style> 标签
       ├─ inlineLayoutStyles()   克隆 DOM，内联 computed style 到 style 属性
       ├─ mergeInto()            合并 DOMCollector 收集的虚拟滚动节点
       └─ 组装 CaptureData       { url, title, html, resources, headers }
```

### 关键设计决策

1. **不冻结页面**：初次捕获只调用 `serializeCSSOMToDOM()`，不调用 `freezePageState()`。这样定时器、WebSocket 等保持运行，后续 DOM 监听器才能观察到变化。

2. **克隆 DOM 写入**：`inlineLayoutStyles()` 克隆整个 DOM 树，从原始 DOM 读取 computed style，写入克隆节点。原始 DOM 不受影响。

3. **虚拟滚动收集**：`DOMCollector` 在页面加载时立即启动，早于捕获流程，确保不遗漏任何被虚拟滚动移除的节点。详见 [virtual-scroll-capture.md](virtual-scroll-capture.md)。

### 发送到服务端

```
sendCapture()
  ├─ POST /api/archive
  ├─ 收到 { page_id, action }
  │    action = "created"   → 新页面，启动 DOM 监听
  │    action = "unchanged" → 内容未变（哈希相同），不启动监听
  └─ 记录 initialHTMLSize（用于后续更新的质量保护）
```

## 二、持续更新机制

初次快照成功且 `action === "created"` 后，启动 DOM 变化监听器，定时检查并上传更新。

### 监听器架构

```
startDOMChangeMonitor()
  ├─ 断开 collectorObserver
  ├─ 创建新 MutationObserver（同时负责 mutation 计数 + 喂养 DOMCollector）
  ├─ 启动 setInterval（每 5 秒检查一次）
  └─ 启动超时定时器（5 分钟后自动停止）
```

新的 observer 同时承担两个职责：
- 统计 mutation 数量（用于判断是否需要上传）
- 将 mutation 传递给 `DOMCollector`（继续收集虚拟滚动节点）

### 定时检查逻辑

每 `UPDATE_CHECK_INTERVAL`（5 秒）执行一次：

```
interval 触发
  ├─ isUpdating? → 跳过（上一次上传还在进行）
  ├─ pageId 变了? → 停止监听（SPA 导航）
  ├─ collector 达到上限? → 强制触发最后一次上传
  ├─ mutations < 10? → 跳过，等下一轮
  ├─ tab hidden? → 跳过（DOM 可能被剥离）
  ├─ scrollY < maxScrollY? → 跳过（用户在回看已捕获的内容）
  └─ 执行上传
       ├─ serializeCSSOMToDOM()
       ├─ inlineLayoutStyles()
       ├─ mergeInto()（合并虚拟滚动节点）
       ├─ HTML 缩水检查（< 70% 初始大小则跳过）
       ├─ PUT /api/archive/:id
       └─ 如果是 final update → stopDOMChangeMonitor()
```

### 停止条件

监听器在以下情况停止：

| 条件 | 说明 |
|------|------|
| 超时（5 分钟） | `UPDATE_MONITOR_TIMEOUT` 到期 |
| Collector 达到 10MB | 做最后一次上传后停止 |
| SPA 导航 | `resetState()` 清理一切 |
| 页面卸载 | `beforeunload` / `pagehide` |

### 质量保护

为避免上传低质量快照，有以下保护措施：

1. **Hidden tab 保护**：`document.visibilityState === 'hidden'` 时跳过上传。X.com 等网站在 tab 失焦时会剥离 DOM 节点。

2. **HTML 缩水保护**：新 HTML 长度 < 初始快照的 70% 时跳过。防止虚拟化剥离内容后上传不完整的快照。

3. **并发保护**：`isUpdating` 标志防止多次上传同时进行。

4. **跨页面保护**：快照 `monitorPageId`，如果 SPA 导航导致 `currentPageId` 变化，停止监听。

5. **回滚保护**：记录 `maxScrollY`（用户到达过的最远滚动位置），当前 `scrollY < maxScrollY` 时跳过上传。回看已捕获的内容不会触发更新，减少不必要的上传和重复风险。

## 三、SPA 导航处理

SPA 导航时 URL 变化但页面不刷新，需要特殊处理。

### 检测方式

优先使用 Navigation API（`navigate` 事件，处理 push/replace/traverse），fallback 到 `history.pushState`/`replaceState` hook + `popstate` 事件。

注意：`traverse`（后退/前进）导航也必须处理，否则旧页面的 DOM monitor 会继续运行，把新页面内容更新到旧页面 ID。只有 `reload` 被跳过（页面完全重载，脚本会重新初始化）。

### 导航流程

```
URL 变化（push/replace/traverse）
  ├─ sendCapture()          发送当前页面的快照
  ├─ resetState()           清理状态
  │    ├─ stopDOMChangeMonitor()
  │    ├─ 清空 DOMCollector
  │    └─ 取消挂起的 SPA 定时器（防止快速连续跳转时脏数据）
  ├─ 等待 SPA_TRANSITION_DELAY (500ms)
  │    └─ 重建 collectorObserver（旧页面 DOM 拆除已完成）
  ├─ 等待 DOM_STABILITY_DELAY (2s)
  └─ prepareCapture() + sendCapture()   捕获新页面
```

**为什么延迟重建 collectorObserver？**

SPA 导航时，框架会拆除旧页面 DOM 并渲染新页面。如果立即重建 collectorObserver，
旧页面被移除的 DOM 节点会被当作「虚拟滚动移除」收集，最终合并进新页面快照，
导致数据泄漏（如主页时间线内容混入推文详情页）。

## 四、服务端处理

### 初次归档（POST /api/archive）

`Deduplicator.ProcessCapture()`:

1. 计算 HTML 的 SHA256 哈希
2. 查询数据库：相同 URL + 相同哈希 → 只更新 `last_visited`，返回 `unchanged`
3. 从 HTML 中提取资源 URL（img/link/script 等）
4. 8 个 worker 并发下载/处理资源
5. 每个资源按哈希去重（相同内容只存一份）
6. 对 CSS：提取并下载其子资源，但不原地改写共享 CSS 文件
7. 用 `URLRewriter` 将 HTML 中的外部 URL 替换为本地路径
8. 保存 HTML 到 `data/html/YYYY/MM/DD/pageID_timestamp.html`
9. 创建 `pages` 和 `page_resources` 数据库记录

### CSS 资源处理约束

CSS 文件按内容哈希去重后，会被多个页面共享复用。

因此服务端必须遵守以下约束：

1. `ProcessCapture()` / `UpdateCapture()` 只负责下载 CSS 中引用的子资源，并建立 `page_resources` 关联。
2. 捕获阶段不能原地改写共享 CSS 文件，否则会把共享文件污染成带 `/archive/resources/...` 的页面专属版本。
3. CSS 的 `url(...)` 重写必须在查看阶段完成：`GET /archive/:page_id/:timestamp/...css` 返回 CSS 时，再根据当前 `page_id` 动态改写为 `/archive/resources/...`。
4. 这样共享 CSS 永远保持原始内容，后续页面复用时不会把归档路径误当成原站 URL 再次下载。

### 更新归档（PUT /api/archive/:id）

`Deduplicator.UpdateCapture()`:

1. 删除旧的 `page_resources` 关联（不删除 `resources` 记录，可能被其他页面引用）
2. 旧 HTML 文件保留在文件系统（用于 debug）
3. 重新执行资源处理流程（提取 → 下载 → 去重 → 重写）
4. 保存新 HTML 文件，更新数据库中的 `html_path` 和 `content_hash`

注意：更新是**全量覆盖**，不是增量合并。每次上传的 HTML 就是最终版本。

### 归档页面查看（GET /view/:id）

服务端对 HTML 进行清理后返回：
- 移除 `<script>`、`<noscript>`、`<base>` 标签
- 移除内联事件处理器和 `javascript:` 链接
- 移除 `loading="lazy"`（归档页面无 JS，懒加载无法触发）
- 移除 `<video autoplay>` 并补上原生 `controls`，避免自动播放，同时保留手动播放能力
- 隐藏无源的 `<video>` 元素
- 修复未重写的 `srcset`、嵌套 `<button>`
- 移除 SPA loading 覆盖层
- `iframe` 子文档即使没有 `.html` 后缀，也按 HTML 响应并做相同清理，避免浏览器把旧快照中的子页面当作下载文件
- 对 CSS 响应按当前 `page_id` 动态重写 `url(...)`，但不修改共享存储中的原始 CSS 文件

## 五、Observer 生命周期

```
页面加载 → collectorObserver 启动（收集虚拟滚动移除的节点）
    ↓
waitForDOMStable → serializeCSSOM → inlineLayoutStyles → mergeInto
    ↓
POST /api/archive（网络请求期间 collectorObserver 持续收集）
    ↓
startDOMChangeMonitor → collectorObserver 断开，新 observer 接管
    ↓
每 5 秒检查 → 有变化则 inlineLayoutStyles → mergeInto → PUT /api/archive/:id（循环）
    ↓
5 分钟超时 或 collector 达到 10MB → 最后一次上传 → 停止监听
    ↓
SPA 导航 → resetState → 重建 collectorObserver → 重复上述流程
```

## 六、配置参数

| 参数 | 值 | 说明 |
|------|-----|------|
| `DOM_STABILITY_DELAY` | 2000ms | 页面加载后延迟多久开始捕获 |
| `DOM_STABLE_TIME` | 1000ms | DOM 无变化多久视为稳定 |
| `MUTATION_OBSERVER_TIMEOUT` | 10000ms | DOM 稳定等待超时 |
| `UPDATE_CHECK_INTERVAL` | 5000ms | 更新检查间隔 |
| `UPDATE_MIN_MUTATIONS` | 10 | 触发更新的最小 mutation 数 |
| `UPDATE_MONITOR_TIMEOUT` | 300000ms | 监听器自动停止时间（5 分钟） |
| `MIN_NODE_SIZE` | 2KB | DOMCollector 收集的最小节点大小 |
| `MAX_COLLECTED_SIZE` | 10MB | DOMCollector 收集的总大小上限 |

## 相关文档

- [virtual-scroll-capture.md](virtual-scroll-capture.md) — DOMCollector 虚拟滚动捕获的详细实现
- [style-inliner-flex-fix.md](style-inliner-flex-fix.md) — 样式内联的 flex 布局修复
