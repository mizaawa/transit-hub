# 健壮性改进说明

本次改进针对项目中"模块缺失、登录不进去"等问题，全面提升了系统的健壮性和可靠性。

## 改进内容

### 1. 启动时依赖检查

**文件**: `backend/scripts/check_deps.sh`

在服务启动前自动检查：
- 必需的环境变量是否配置
- PostgreSQL 数据库连接是否可用
- Redis 连接是否正常
- 服务端口是否被占用

使用方法：
```bash
cd backend
./scripts/check_deps.sh && go run cmd/api/main.go
```

### 2. 配置验证增强

**文件**: `backend/internal/config/validator.go`

启动时自动验证所有配置项：
- 数据库 URL 格式
- Redis 地址格式
- JWT 密钥长度（至少 32 字节）
- 管理员账号配置
- 端口号合法性

配置错误时会立即失败并给出明确提示，避免运行时才发现问题。

### 3. 数据库健康监控

**文件**: `backend/internal/database/health.go`

后台定期检查数据库和 Redis 连接状态：
- 每 30 秒自动探测连接
- 连接失败时记录详细日志
- 提供实时健康状态接口 `/api/health`

健康检查接口返回格式：
```json
{
  "status": "ok",
  "timestamp": "2026-08-18T10:00:00Z",
  "database": "healthy",
  "redis": "healthy"
}
```

### 4. 全局限流保护

**文件**: `backend/internal/httpserver/middleware.go`

防止过多并发请求压垮服务：
- Token Bucket 算法实现
- 默认每秒 500 请求
- 超出限制返回 429 状态码
- 自动补充令牌，支持突发流量

### 5. Panic 恢复机制

**文件**: `backend/internal/httpserver/recovery.go`

捕获所有 panic 防止服务崩溃：
- 记录完整堆栈信息
- 返回标准 JSON 错误响应
- 不影响其他请求处理

### 6. 前端错误边界

**文件**: `frontend/src/components/ErrorBoundary.vue`

捕获 Vue 组件渲染错误：
- 防止整个应用白屏
- 显示友好错误提示
- 提供刷新按钮恢复

### 7. 离线状态提示

**文件**: `frontend/src/components/OfflineIndicator.vue`

实时监测网络状态：
- 检测离线/在线切换
- 顶部横幅提示用户
- 网络恢复自动消失

### 8. 后端服务监控

**文件**: `frontend/src/components/BackendStatusIndicator.vue`

定期检查后端健康状态：
- 每 10 秒调用 `/api/health`
- 连续 2 次失败才报警（避免误报）
- 服务恢复自动消失
- 显示连接异常横幅

### 9. API 请求重试机制

**文件**: `frontend/src/lib/apiRetry.ts`

自动重试失败的请求：
- 指数退避 + 随机抖动
- 最多重试 3 次
- 仅对网络错误和 5xx 错误重试
- 不重试业务错误（4xx）

使用示例：
```typescript
import { fetchWithRetry } from '@/lib/apiRetry'

const data = await fetchWithRetry('/api/users', {
  method: 'GET',
  headers: { 'Authorization': `Bearer ${token}` }
})
```

## 部署建议

1. **生产环境启动前检查**
   ```bash
   ./scripts/check_deps.sh && ./transit-hub-backend
   ```

2. **监控健康检查接口**
   - 配置负载均衡器定期调用 `/api/health`
   - 连续失败时自动摘除节点

3. **调整限流参数**
   根据实际负载调整 `server.go` 中的限流阈值：
   ```go
   rateLimiter := RateLimit(1000) // 根据需要调整
   ```

4. **日志监控**
   关注以下日志关键词：
   - `[health]` - 健康检查状态
   - `[panic]` - 捕获的 panic
   - `[api-retry]` - 请求重试
   - `[backend-status]` - 前端监控

## 问题排查

### 启动失败
1. 运行 `./scripts/check_deps.sh` 查看具体问题
2. 检查 `.env` 文件配置是否完整
3. 确认数据库和 Redis 服务是否运行

### 登录失败
1. 确认 `ADMIN_EMAIL` 和 `ADMIN_PASSWORD` 环境变量已设置
2. 检查 JWT_SECRET 长度至少 32 字节
3. 查看后端日志确认管理员账号是否创建成功

### 频繁 429 错误
- 调高限流阈值或优化前端请求频率

### 健康检查失败
- 检查数据库连接池是否耗尽
- 确认 Redis 连接数是否达到上限
