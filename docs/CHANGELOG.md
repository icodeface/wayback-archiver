# Changelog - 2026-03-14

## 安全修复

### 修复的漏洞

1. **CORS 允许所有来源** - 限制为可配置的白名单
2. **资源去重竞态条件** - 使用数据库 ON CONFLICT 防止
3. **UpdateCapture 非原子操作** - 已记录为已知限制
4. **下载资源无大小限制** - 添加 200MB 限制
5. **JSON.parse 未 try/catch** - 添加异常处理

详见 [SECURITY_FIXES_2026-03-14.md](SECURITY_FIXES_2026-03-14.md)

## 新增功能

### 远程部署支持

- 新增 `ALLOWED_ORIGINS` 环境变量，支持配置 CORS 白名单
- 默认值：`http://localhost:8080,http://127.0.0.1:8080,null`
- 远程部署时可添加自定义域名：`ALLOWED_ORIGINS=https://your-domain.com,null`

### 资源大小限制调整

- 最大下载资源限制从 50MB 提升到 **200MB**
- 支持大型 CSS/JS 文件和高清图片

## 配置变更

### 新增环境变量

```bash
# CORS 配置（逗号分隔的允许来源列表）
ALLOWED_ORIGINS=http://localhost:8080,http://127.0.0.1:8080,null
```

### 配置文件

新增 `server/.env.example` 示例配置文件

## 测试改进

### 新增测试文件

1. `server/internal/storage/filesystem_security_test.go` - 文件系统安全测试
2. `server/internal/storage/deduplicator_security_test.go` - 去重器安全测试
3. `server/internal/api/cors_security_test.go` - CORS 安全测试

### 测试覆盖

- SSRF 防护测试
- CORS 白名单测试
- 资源大小限制测试
- 根域名判断测试

所有测试通过 ✓

## 文档更新

### 新增文档

1. [REMOTE_DEPLOYMENT.md](REMOTE_DEPLOYMENT.md) - 远程部署完整指南
   - 配置说明
   - 安全建议
   - HTTPS 配置
   - 故障排查
   - 监控和备份

2. [SECURITY_FIXES_2026-03-14.md](SECURITY_FIXES_2026-03-14.md) - 安全修复详细说明

### 更新文档

- [README.md](README.md) - 添加远程部署说明和新配置项

## 破坏性变更

### API 签名变更

`SetupRoutes` 函数新增 `serverCfg` 参数：

```go
// 旧版本
func SetupRoutes(r *gin.Engine, handler *Handler, authCfg *config.AuthConfig)

// 新版本
func SetupRoutes(r *gin.Engine, handler *Handler, authCfg *config.AuthConfig, serverCfg *config.ServerConfig)
```

**影响**：如果你有自定义的测试代码调用 `SetupRoutes`，需要更新调用方式。

## 升级指南

### 从旧版本升级

1. **更新代码**
   ```bash
   git pull
   cd browser && npm run build
   cd ../server && go build -o wayback-server ./cmd/server
   ```

2. **更新配置**（可选）
   ```bash
   # 如果需要远程部署，添加 ALLOWED_ORIGINS
   export ALLOWED_ORIGINS=https://your-domain.com,null
   ```

3. **重启服务**
   ```bash
   ./wayback-server
   ```

### 本地开发

无需修改配置，默认值已适配本地开发环境。

### 远程部署

参考 [REMOTE_DEPLOYMENT.md](REMOTE_DEPLOYMENT.md) 进行配置。

## 安全建议

### 必须执行

1. ✅ 远程部署时设置 `AUTH_PASSWORD`
2. ✅ 使用 HTTPS（通过 Nginx/Caddy 反向代理）
3. ✅ 限制 `ALLOWED_ORIGINS` 仅包含信任的域名

### 推荐执行

1. 配置防火墙，仅开放必要端口
2. 定期备份数据库和文件
3. 监控日志文件
4. 使用 logrotate 管理日志

## 已知问题

1. **UpdateCapture 非原子操作** - 多个步骤未在事务中执行，中途失败可能留下不一致状态。计划在未来版本中添加完整事务支持。

## 性能优化

- 资源下载并发数：8（可在代码中调整）
- 数据库连接池：最大 25 个连接
- 资源缓存 TTL：2 小时

## 贡献者

- Claude Opus 4.6 - 安全修复和远程部署支持

---

**版本**：v1.1.0  
**发布日期**：2026-03-14
