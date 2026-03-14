# Security Fixes - 2026-03-14

本文档记录了针对 Wayback Archiver 项目的安全漏洞修复。

## 修复的漏洞

### 1. 硬编码 CSP Nonce (High)

**位置**: `server/internal/api/view_handler.go`

**问题**: CSP nonce 是固定字符串 `"wayback-fix-positioning"`，归档的恶意页面可以在 HTML 中嵌入同样 nonce 的 `<script>` 标签绕过 CSP。

**修复**:
- 实现 `generateNonce()` 函数，使用 `crypto/rand` 生成 128 位随机 nonce
- 每次请求生成新的 nonce，防止预测和重放攻击
- 添加测试验证 nonce 的唯一性和随机性

**测试**: `server/internal/api/security_test.go::TestGenerateNonce`

---

### 2. SSRF 漏洞 - 资源下载 (High)

**位置**: `server/internal/storage/filesystem.go`

**问题**: `DownloadResource` 对用户提供的 URL 没有限制内网地址。169.254.169.254（云元数据）、10.x、192.168.x 等私有地址都可以被请求到。

**修复**:
- 实现 `validateResourceURL()` 函数，验证 URL 协议和主机名
- 实现 `isPrivateIP()` 函数，检测私有 IP 地址段（RFC1918、Loopback、Link-local）
- 实现 `isCloudMetadataIP()` 函数，阻止访问云服务元数据 IP（169.254.169.254、fd00:ec2::254）
- 只允许 http 和 https 协议
- 正确处理 IPv4-mapped IPv6 地址（如 ::ffff:8.8.8.8）

**阻止的地址**:
- 私有 IPv4: 10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16, 127.0.0.0/8, 169.254.0.0/16
- 私有 IPv6: ::1/128, fc00::/7, fe80::/10
- 云元数据: 169.254.169.254/32, fd00:ec2::254/128

**测试**: `server/internal/storage/security_test.go::TestValidateResourceURL`, `TestIsPrivateIP`, `TestIsCloudMetadataIP`

---

### 3. Cookie 同根域判断不准确 (Medium)

**位置**: `server/internal/storage/filesystem.go`

**问题**: `getRootDomain` 取最后两段，对 co.uk、com.au 等多段 TLD 会误判。evil.co.uk 和 bank.co.uk 都会被认为是同根域，导致 Cookie 泄露给不相关的域名。

**修复**:
- 更新 `getRootDomain()` 函数，支持常见的多段 TLD
- 添加公共后缀列表（co.uk, co.jp, com.au, com.br, com.cn, com.hk, com.tw 等）
- 正确提取根域名（如 example.co.uk 而不是 co.uk）

**测试**: `server/internal/storage/security_test.go::TestGetRootDomain`, `TestIsSameRootDomain`

**示例**:
- `evil.co.uk` 和 `bank.co.uk` → 不同根域，不共享 Cookie ✓
- `www.bank.co.uk` 和 `api.bank.co.uk` → 同根域，可共享 Cookie ✓

---

### 4. SPA 导航竞态条件 (Medium)

**位置**: `browser/src/main.ts`

**问题**: SPA 导航时 `sendCapture()` 是异步的但没有 await，紧接着就 `resetState()`，可能导致上一个页面的捕获数据被清空后才发送，造成数据丢失。

**修复**:
- 在 Navigation API 监听器中，等待 `sendCapture()` 完成后再调用 `resetState()`
- 在 `onURLChange()` 函数中应用相同的修复
- 使用 Promise chain 确保操作顺序

**影响**: 防止 SPA 页面切换时丢失归档数据

---

## 测试覆盖

所有修复都包含完整的单元测试：

```bash
# 运行所有安全测试
cd server
go test ./internal/api -v -run Security
go test ./internal/storage -v -run Security

# 运行所有测试
go test ./... -v
```

测试结果: ✅ 所有测试通过

---

## 影响评估

| 漏洞 | 严重性 | 影响范围 | 修复状态 |
|------|--------|----------|----------|
| 硬编码 CSP Nonce | High | 归档页面可执行恶意脚本 | ✅ 已修复 |
| SSRF - 资源下载 | High | 可访问内网和云元数据 | ✅ 已修复 |
| Cookie 同根域误判 | Medium | Cookie 泄露给不相关域名 | ✅ 已修复 |
| SPA 导航竞态 | Medium | 数据丢失 | ✅ 已修复 |

---

## 部署建议

1. 更新代码后重新编译服务端：
   ```bash
   cd server
   go build -o wayback-server ./cmd/server
   ```

2. 重新构建浏览器脚本：
   ```bash
   cd browser
   npm run build
   ```

3. 运行测试验证：
   ```bash
   cd server
   go test ./... -v
   ```

4. 重启服务

---

## 参考

- [OWASP SSRF Prevention Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Server_Side_Request_Forgery_Prevention_Cheat_Sheet.html)
- [CSP Nonce Best Practices](https://content-security-policy.com/nonce/)
- [Public Suffix List](https://publicsuffix.org/)
