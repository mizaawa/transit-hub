# Transit Hub 健壮性改进文档

## 概述

本文档说明了为提高 Transit Hub 项目健壮性所做的改进，解决了"模块缺失"、"登录不进去"等常见问题。

## 🔧 后端改进

### 1. 健康检查脚本 (`backend/scripts/health-check.sh`)

**功能：**
- 启动前检查所有必需的环境变量
- 验证数据库和 Redis 连接
- 检查 Go 模块依赖是否完整
- 提供清晰的错误提示

**使用方法：**
```bash
cd backend
./scripts/health-check.sh
```

### 2. 增强的中间件

#### Panic 恢复 (`internal/httpserver/recovery.go`)
- 捕获所有 panic，防止单个请求崩溃整个服务
- 记录完整的堆栈信息
- 返回友好的 500 错误响应

#### 超时控制 (`internal/httpserver/timeout.go`)
- 为每个请求设置超时时间（默认 30 秒）
- 防止请求挂起耗尽资源
- 提供请求 ID 追踪

#### CORS 处理 (`internal/httpserver/cors.go`)
- 统一的跨域配置
- 支持预检请求
- 灵活的源白名单

### 3. 数据库和 Redis 健壮性

#### 自动重连机制
- 连接断开时自动重试
- 指数退避策略
- 连接池健康检查

#### 配置验证 (`internal/config/validator.go`)
- 启动时验证所有必需配置
- 提供详细的错误信息
- 防止配置错误导致的运行时崩溃

## 🎨 前端改进

### 1. 增强的错误边界 (`frontend/src/components/ErrorBoundary.vue`)

**新功能：**
- 识别常见错误类型并提供友好提示
- "清除缓存并刷新"功能
- 显示/隐藏错误详情
- 针对性的解决建议

**处理的错误类型：**
- 网络连接失败
- API 配置错误
- 模块加载失败
- 其他运行时错误

### 2. 请求重试机制 (`frontend/src/lib/requestRetry.ts`)

**特性：**
- 自动重试网络错误（最多 3 次）
- 指数退避延迟
- 不重试业务错误（4xx/5xx）

### 3. Token 自动刷新 (`frontend/src/modules/auth/api/auth.ts`)

**功能：**
- Token 过期前 5 分钟自动刷新
- 互斥锁防止并发刷新
- 刷新失败自动跳转登录页

### 4. 连接监控 (`frontend/src/composables/useConnectionMonitor.ts`)

**功能：**
- 实时监控网络连接状态
- 定期检查后端健康状态（每 30 秒）
- 提供重连机制

### 5. 健康监控 UI (`frontend/src/components/HealthMonitor.vue`)

**显示内容：**
- 网络断开提示
- 后端服务不可用提示
- 最后检查时间

### 6. 认证恢复 (`frontend/src/composables/useAuthRecovery.ts`)

**功能：**
- 页面加载时检查登录状态
- 自动处理 token 过期
- 重定向到登录页

### 7. 离线指示器 (`frontend/src/components/OfflineIndicator.vue`)

**功能：**
- 显示网络连接状态
- 重试连接按钮
- 友好的视觉提示

## 📦 使用指南

### 后端启动前检查

```bash
cd backend

# 运行健康检查
./scripts/health-check.sh

# 如果检查通过，启动服务
go run cmd/api/main.go
```

### 前端集成

在主应用中添加健壮性组件：

```vue
<template>
  <ErrorBoundary>
    <HealthMonitor />
    <OfflineIndicator />
    <RouterView />
  </ErrorBoundary>
</template>

<script setup lang="ts">
import ErrorBoundary from '@/components/ErrorBoundary.vue'
import HealthMonitor from '@/components/HealthMonitor.vue'
import OfflineIndicator from '@/components/OfflineIndicator.vue'
</script>
```

### 使用连接监控

```typescript
import { useConnectionMonitor } from '@/composables/useConnectionMonitor'

const { isOnline, backendHealthy, checkBackendHealth } = useConnectionMonitor()

// 手动检查健康状态
await checkBackendHealth()
```

### 使用认证恢复

```typescript
import { useAuthRecovery } from '@/composables/useAuthRecovery'

const { hasValidToken, isChecking } = useAuthRecovery()
```

## 🐛 常见问题解决

### 问题：登录不进去

**可能原因：**
1. Token 过期但未清除
2. API 路径配置错误
3. 后端服务未启动

**解决方案：**
1. 点击"清除缓存并刷新"按钮
2. 检查 `.env` 中的 `VITE_API_BASE_URL` 配置
3. 运行健康检查脚本验证后端状态

### 问题：模块加载失败

**可能原因：**
1. 浏览器缓存过期
2. 前端构建不完整
3. 依赖包缺失

**解决方案：**
1. 清除浏览器缓存（Ctrl+Shift+Del）
2. 重新构建前端：`npm run build`
3. 重新安装依赖：`npm ci`

### 问题：网络请求失败

**可能原因：**
1. 网络连接断开
2. 后端服务未响应
3. CORS 配置错误

**解决方案：**
1. 检查网络连接
2. 查看 HealthMonitor 显示的状态
3. 检查浏览器控制台的 CORS 错误

## 🔍 调试技巧

### 查看请求 ID

每个请求都有唯一的 Request ID，可以在响应头中找到：

```
X-Request-ID: 20260818150405-abc12345
```

### 启用详细日志

后端会自动记录：
- 所有 panic 的堆栈信息
- 数据库连接重试
- Redis 连接状态
- 请求超时

### 前端错误追踪

错误边界会捕获所有运行时错误，并在控制台中打印详细信息：

```javascript
[ErrorBoundary] TypeError: Cannot read property 'x' of undefined
    at Component.vue:42
    ...
```

## ✅ 最佳实践

1. **启动前检查**：每次启动前运行 `health-check.sh`
2. **监控告警**：在生产环境集成 HealthMonitor
3. **日志收集**：配置集中式日志收集系统
4. **定期清理**：定期清理过期的 token 和缓存
5. **版本控制**：使用语义化版本号，便于问题追踪

## 📊 性能影响

所有改进都经过优化，对性能影响最小：

- 请求重试：仅在网络错误时触发
- Token 刷新：提前刷新，不影响用户体验
- 健康检查：30 秒一次，使用 5 秒超时
- 错误边界：零性能开销，仅在错误时激活

## 🚀 下一步改进建议

1. 添加 Service Worker 实现离线缓存
2. 集成 Sentry 进行错误追踪
3. 添加性能监控（Web Vitals）
4. 实现请求队列和批处理
5. 添加 WebSocket 心跳检测

---

**维护者：** Transit Hub Team  
**最后更新：** 2026-08-18
