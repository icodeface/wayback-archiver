# Wayback Archiver

> *我的互联网记忆 - 记录我浏览过的一切*

[English](README.md) | 中文

一个自托管的个人网页归档系统，自动捕获并保存你在 Chrome 中浏览过的网页 — HTML、CSS、JavaScript、图片等一应俱全。当原始网页无法访问时，你仍然可以通过归档副本还原当时的页面样式和内容。

![index](./screenshot/index.webp)  
![x](./screenshot/x.webp)   
![v2ex](./screenshot/v2ex.webp)  

## 工作原理

```
Chrome + Tampermonkey ──HTTP POST──▶ Go 服务器 ──▶ PostgreSQL (元数据)
  (页面加载完成后                       │              + 文件系统 (静态资源)
   自动捕获)                            │
                                        ▼
                                     Web UI ──▶ 浏览 / 搜索 / 还原
```

1. Tampermonkey 用户脚本在浏览器中运行，页面加载完成后自动捕获完整的 DOM 和资源。若后续 DOM 发生显著变化，会自动提交一次更新。
2. Go 服务器接收快照，下载浏览器因 CORS 限制无法获取的跨域资源，基于内容哈希去重后存储到本地。
3. 内置 Web UI 可以浏览、搜索和还原任意归档页面 — 完全离线，不依赖外部服务。

## 功能特性

- **高保真还原** — CSSOM 序列化、计算样式内联、防刷新保护，尽可能还原页面原始效果
- **完整页面捕获** — HTML、CSS、JS、图片、字体；资源 URL 自动重写为本地路径
- **跨域资源恢复** — 服务器端自动提取并下载被 CORS 拦截的资源
- **内容哈希去重** — 相同资源跨页面共享，仅存储一份（SHA-256）
- **版本历史** — 同一 URL 可多次归档，按时间戳区分
- **时间线视图** — 在可视化时间轴上浏览同一 URL 的所有快照（类似 web.archive.org），支持快照间前后导航
- **智能去重** — 会话级 + 服务器级双重去重，内容无变化时仅更新访问时间
- **动态内容支持** — 捕获实时 DOM 状态；MutationObserver 监听变化，超过阈值自动提交一次更新
- **SPA 感知** — 检测单页应用导航，按路由重置捕获状态
- **防刷新保护** — 归档页面被冻结：定时器、WebSocket 和导航 API 均被拦截
- **Web UI** — 响应式界面，支持全文搜索（页面内容、URL、标题）、按时间范围筛选和还原归档页面
- **RESTful API** — 提供完整的归档和查询接口

## 环境要求

- **Go** 1.21+
- **Node.js** 16+（用于构建用户脚本）
- **PostgreSQL** 14+
- **Chrome** + [Tampermonkey](https://www.tampermonkey.net/) 扩展

## 快速开始

### 1. 数据库配置

```bash
createdb -U postgres wayback
psql -U postgres wayback < server/init_db.sql
```

### 2. 启动服务器

```bash
cd server
cp .env.example .env   # 按需修改配置
go build -o wayback-server ./cmd/server
./wayback-server
```

服务器默认在 `http://localhost:8080` 启动。

如需通过代理下载外部资源：

```bash
export http_proxy=http://127.0.0.1:7897
export https_proxy=http://127.0.0.1:7897
./wayback-server
```

### 3. 安装用户脚本

```bash
cd browser
npm install
npm run build
```

然后：

1. 在 Chrome 中打开 Tampermonkey 管理面板
2. 创建新脚本
3. 粘贴 `browser/dist/wayback.user.js` 的内容
4. 保存并启用

### 4. 开始浏览

就这样。页面加载完成后会自动归档。打开 `http://localhost:8080` 查看你的归档。

## 配置项

环境变量（或 `server/.env` 文件）：

| 变量 | 默认值 | 说明 |
|---|---|---|
| `DB_HOST` | `localhost` | PostgreSQL 主机 |
| `DB_PORT` | `5432` | PostgreSQL 端口 |
| `DB_USER` | `postgres` | 数据库用户 |
| `DB_PASSWORD` | *（空）* | 数据库密码 |
| `DB_NAME` | `wayback` | 数据库名称 |
| `DB_SSLMODE` | `disable` | SSL 模式 |
| `SERVER_PORT` | `8080` | HTTP 服务端口 |
| `DATA_DIR` | `./data` | HTML 和资源的存储目录 |
| `LOG_DIR` | `./data/logs` | 日志文件目录 |
| `AUTH_PASSWORD` | *（空）* | HTTP Basic Auth 密码（为空时关闭认证，用户名固定为 `wayback`） |

## API

| 方法 | 路径 | 说明 |
|---|---|---|
| `POST` | `/api/archive` | 创建页面归档 |
| `PUT` | `/api/archive/:id` | 更新已有归档快照 |
| `GET` | `/api/pages` | 列出所有归档页面 |
| `GET` | `/api/pages/:id` | 获取页面详情 |
| `GET` | `/api/pages/:id/content` | 获取页面正文的 Markdown 格式（方便 AI/LLM 读取） |
| `GET` | `/api/search?q=keyword` | 按 URL 或标题搜索 |
| `GET` | `/api/pages/timeline?url=URL` | 获取同一 URL 的所有快照（时间线视图） |
| `GET` | `/api/logs` | 列出可用日志文件 |
| `GET` | `/api/logs/:filename` | 获取日志文件内容（支持 `?tail=N`） |
| `GET` | `/view/:id` | 还原归档页面 |
| `GET` | `/timeline?url=URL` | URL 时间线可视化页面 |
| `GET` | `/logs` | 服务器日志查看器 |

### POST /api/archive

返回 `{ status, page_id, action }`，其中 `action` 为 `created`（新建）或 `unchanged`（内容未变，仅更新 `last_visited`）。

### PUT /api/archive/:id

请求体与 POST 相同。替换快照内容 — 旧 HTML 和资源关联被移除，资源重新处理。返回 `{ status, page_id, action }`，`action` 为 `updated` 或 `unchanged`。

## 项目结构

```
wayback-archiver/
├── browser/                  # Tampermonkey 用户脚本 (TypeScript)
│   ├── src/
│   │   ├── main.ts           # 入口 & 流程编排
│   │   ├── config.ts         # 常量配置
│   │   ├── types.ts          # TypeScript 类型定义
│   │   ├── page-filter.ts    # URL 过滤逻辑
│   │   ├── page-freezer.ts   # 冻结页面运行时状态
│   │   ├── dom-collector.ts  # DOM 序列化
│   │   └── archiver.ts       # 服务器通信
│   ├── dist/                 # 构建产物
│   └── build.js              # 打包脚本
│
├── server/                   # Go 后端
│   ├── cmd/server/main.go    # 入口
│   ├── internal/
│   │   ├── api/              # HTTP 处理器（模块化）
│   │   ├── config/           # 环境变量配置
│   │   ├── database/         # PostgreSQL 操作
│   │   ├── logging/          # 文件日志 & 自动轮转
│   │   ├── models/           # 数据模型
│   │   └── storage/          # 文件存储 & 去重
│   ├── web/                  # Web UI 静态文件
│   └── .env.example
│
└── tests/                    # 测试
    ├── browser/              # 浏览器端测试
    └── server/               # 服务器端 & 端到端测试
```

## 存储结构

```
data/
├── html/                     # HTML 快照，按日期组织
│   └── 2026/03/09/
│       └── <timestamp>_<hash>.html
├── logs/                     # 服务器日志，按大小（10MB）和日期轮转（保留 7 天）
│   ├── wayback-2026-03-12.001.log
│   └── wayback-2026-03-12.002.log
└── resources/                # 去重后的静态资源
    └── ab/cd/
        └── <sha256>.css
```

## 测试

```bash
# Go 单元测试
cd server && go test ./... -v

# 端到端测试（需要 Chrome）
cd tests/server && node test_update_feature.js
```

## 已知限制

- 部分跨域资源可能因服务器端 403/404 响应而无法保存
- 通过 JS 动态注入的脚本可能无法被捕获
- 带动态参数的统计/追踪 URL 不会被保存（不影响页面渲染）
- 大型媒体文件（视频、高清图片）会占用较多存储空间

## 许可证

MIT
