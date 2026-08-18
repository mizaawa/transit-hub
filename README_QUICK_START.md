# 🚀 快速启动指南

## 启动前检查

**Windows 用户**（使用 Git Bash 或 WSL）：
```bash
cd backend
bash ./scripts/check_deps.sh
```

**Linux/Mac 用户**：
```bash
cd backend
./scripts/check_deps.sh
```

检查脚本会验证：
- ✅ 环境变量配置
- ✅ PostgreSQL 数据库连接
- ✅ Redis 连接
- ✅ 端口占用情况

## 后端启动

```bash
cd backend
go run cmd/api/main.go
```

启动时会自动：
1. 验证所有配置项
2. 连接数据库并运行迁移
3. 验证数据库和 Redis 连接
4. 启动后台健康监控
5. 启动 HTTP 服务器

## 前端启动

```bash
cd frontend
npm install
npm run dev
```

## 健壮性功能

### 后端

- **配置验证**：启动时检查所有必需配置，错误立即失败
- **健康监控**：每 30 秒探测数据库和 Redis 连接
- **限流保护**：Token Bucket 算法，默认 500 req/s
- **Panic 恢复**：捕获所有 panic，记录堆栈，返回标准错误
- **依赖检查**：启动前检查脚本验证所有依赖

### 前端

- **错误边界**：捕获组件渲染错误，防止白屏
- **离线检测**：实时监测网络状态，显示提示横幅
- **后端监控**：定期检查后端健康状态
- **请求重试**：网络错误或 5xx 自动重试（最多 3 次）

## 健康检查接口

```bash
curl http://localhost:8080/api/health
```

响应：
```json
{
  "status": "ok",
  "timestamp": "2026-08-18T10:00:00Z",
  "database": "healthy",
  "redis": "healthy"
}
```

## 常见问题

### 启动失败

1. 运行依赖检查：`cd backend && ./scripts/check_deps.sh`
2. 检查 `.env` 文件配置
3. 确认数据库和 Redis 服务运行

### 登录失败

1. 确认 `JWT_SECRET` 长度 ≥ 32 字节
2. 检查 `ADMIN_EMAIL` 和 `ADMIN_PASSWORD` 环境变量
3. 查看后端日志确认管理员账号创建

### 调整限流阈值

编辑 `backend/internal/httpserver/server.go`：
```go
rateLimiter := RateLimit(1000) // 调整这个值
```

## 文档

- `ROBUSTNESS_SUMMARY.md` - 健壮性改进摘要
- `ROBUSTNESS_IMPROVEMENTS.md` - 详细技术文档
- `backend/README_ROBUSTNESS.md` - 后端部署建议
