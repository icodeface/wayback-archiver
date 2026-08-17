# Add Favorites Feature

## 概述
添加收藏功能，允许用户标记和管理常用的归档页面。

## 功能特性

### 数据库层
- ✅ 新增 `favorites` 表，支持 PostgreSQL 和 SQLite
- ✅ 自动数据库迁移
- ✅ 外键约束确保数据完整性
- ✅ 索引优化查询性能

### 后端 API
- `POST /api/favorites/:id` - 添加收藏
- `DELETE /api/favorites/:id` - 取消收藏
- `GET /api/favorites/:id/status` - 检查收藏状态
- `GET /api/favorites` - 列出收藏（支持分页）
- `GET /api/favorites/search` - 搜索收藏（支持分页）

### 前端界面
- **主页面增强**：
  - 每个页面项添加收藏按钮（★）
  - 实时更新收藏状态（已收藏时高亮显示）
  - 顶部导航添加 FAVORITES 链接

- **独立收藏页面**：
  - 专门的收藏夹页面 (`/favorites`)
  - 全文搜索收藏的页面
  - 分页浏览支持
  - 支持取消收藏和删除操作
  - 保持与主页面一致的赛博朋克风格

## 技术实现

### 数据库设计
```sql
CREATE TABLE favorites (
    page_id BIGINT NOT NULL REFERENCES pages(id) ON DELETE CASCADE,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    PRIMARY KEY (page_id)
);
CREATE INDEX idx_favorites_created_at ON favorites(created_at DESC);
```

### API 响应示例
```json
// GET /api/favorites
{
  "pages": [...],
  "total": 3,
  "limit": 20,
  "offset": 0
}

// GET /api/favorites/:id/status
{
  "is_favorite": true
}
```

## 测试
- ✅ 添加/删除收藏功能测试通过
- ✅ 收藏状态检查测试通过
- ✅ 列表和搜索功能测试通过
- ✅ 数据库迁移自动执行成功
- ✅ 外键约束验证通过

## 兼容性
- ✅ 支持 PostgreSQL 和 SQLite
- ✅ 自动数据库迁移，不影响现有数据
- ✅ 向后兼容，不破坏现有功能

## 文件变更
- 新增：`server/internal/api/favorites_handler.go` - 收藏功能处理器
- 新增：`server/web/favorites.html` - 收藏页面
- 修改：`server/internal/database/interface.go` - 数据库接口
- 修改：`server/internal/database/postgres.go` - PostgreSQL 实现
- 修改：`server/internal/database/sqlite.go` - SQLite 实现
- 修改：`server/internal/api/routes.go` - 路由注册
- 修改：`server/web/index.html` - 主页面增强

## 截图
### 主页面收藏按钮
- 每个页面项右侧显示 ★ 按钮
- 已收藏的页面按钮高亮显示

### 收藏页面
- 独立的收藏夹界面
- 支持搜索和分页
- 与主页面风格一致

## 使用方式

1. **添加收藏**：在主页面点击页面项右侧的 ★ 按钮
2. **查看收藏**：点击顶部导航的 FAVORITES 链接
3. **搜索收藏**：在收藏页面使用搜索框
4. **取消收藏**：在收藏页面点击"取消收藏"按钮，或在主页面再次点击 ★ 按钮

## 未来改进
- [ ] 添加收藏夹分类/标签功能
- [ ] 支持批量操作
- [ ] 导出收藏列表
- [ ] 收藏统计和分析
