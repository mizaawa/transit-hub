# Transit Hub 快速修复指南

当遇到"登录不进去"或"模块缺失"等问题时，按照以下步骤操作：

## 🚨 紧急修复步骤

### 1️⃣ 清除浏览器缓存和本地存储

在浏览器中按 `F12` 打开开发者工具，然后：

**Chrome/Edge:**
```
1. 按 Ctrl+Shift+P (Mac: Cmd+Shift+P)
2. 输入 "Clear site data"
3. 选择 "Clear site data" 并确认
4. 刷新页面 (F5)
```

**或者使用应用内功能:**
- 当出现错误页面时，点击"清除缓存并刷新"按钮

### 2️⃣ 检查后端服务状态

```bash
cd backend
./scripts/health-check.sh
```

**如果检查失败，根据提示修复：**

- ❌ `.env 文件不存在` → 从 `.env.example` 复制并配置
- ❌ `数据库端口不可达` → 启动数据库服务
- ❌ `Redis 端口不可达` → 启动 Redis 服务
- ❌ `Go 包未安装` → 运行 `go mod download`

### 3️⃣ 验证环境配置

**后端 `.env` 必需项：**
```env
DATABASE_URL=postgres://user:password@localhost:5432/transithub
REDIS_URL=redis://localhost:6379
JWT_SECRET=your-secret-key-change-in-production
PORT=8080
```

**前端 `.env` 必需项：**
```env
VITE_API_BASE_URL=/api
```

### 4️⃣ 重启服务

**后端：**
```bash
cd backend
go run cmd/api/main.go
```

**前端：**
```bash
cd frontend
npm run dev
```

## 🔍 常见错误及解决方案

### 错误 1: "Failed to fetch" / "network error"

**原因：** 后端服务未启动或端口不匹配

**解决：**
1. 检查后端是否在运行
2. 确认端口配置（默认 8080）
3. 检查防火墙设置

### 错误 2: "Unexpected token '<'" / JSON 解析错误

**原因：** API 路径配置错误，请求被路由到前端

**解决：**
1. 检查 `VITE_API_BASE_URL` 是否为 `/api`
2. 确认反向代理配置正确
3. 清除浏览器缓存

### 错误 3: 登录后立即退出

**原因：** Token 存储或刷新机制异常

**解决：**
1. 清除 localStorage: 
   ```javascript
   localStorage.clear()
   ```
2. 检查后端 `/auth/refresh` 接口是否正常
3. 确认 JWT_SECRET 配置一致

### 错误 4: "module not found" / 模块加载失败

**原因：** 依赖未安装或版本不匹配

**解决：**

**后端：**
```bash
cd backend
go mod tidy
go mod download
```

**前端：**
```bash
cd frontend
rm -rf node_modules package-lock.json
npm install
```

### 错误 5: CORS 错误

**原因：** 跨域配置不正确

**解决：**
1. 确认前后端端口配置
2. 检查后端 CORS 中间件是否正确配置
3. 如果使用反向代理，检查代理配置

## 🛠️ 完全重置（最后手段）

如果上述方法都无效，执行完全重置：

```bash
# 1. 停止所有服务
# Ctrl+C 或 kill 进程

# 2. 清理后端
cd backend
rm -rf tmp/
go clean -cache -modcache -testcache
go mod download

# 3. 清理前端
cd ../frontend
rm -rf node_modules dist .vite
npm ci

# 4. 清理数据库（可选，会丢失数据）
# psql -U postgres -c "DROP DATABASE transithub;"
# psql -U postgres -c "CREATE DATABASE transithub;"

# 5. 运行健康检查
cd ../backend
./scripts/health-check.sh

# 6. 重新启动服务
go run cmd/api/main.go

# 在新终端
cd ../frontend
npm run dev
```

## 📞 获取帮助

如果问题仍未解决：

1. **查看日志：** 检查后端控制台和浏览器开发者工具
2. **记录错误信息：** 包括完整的错误堆栈
3. **检查版本：** 确认 Go、Node.js 版本符合要求
4. **查看文档：** 阅读 README-ROBUSTNESS.md

## ✅ 预防措施

定期执行以下操作可以减少问题发生：

1. **每周更新依赖：**
   ```bash
   go get -u ./...  # 后端
   npm update       # 前端
   ```

2. **定期清理缓存：**
   - 浏览器缓存（每月）
   - Go 构建缓存（每月）
   - Node modules（重大更新时）

3. **备份配置文件：**
   - 保留 `.env` 的备份
   - 使用版本控制管理 `.env.example`

4. **监控服务状态：**
   - 使用 HealthMonitor 组件
   - 定期查看后端日志

---

**记住：** 90% 的问题都可以通过"清除缓存 + 重启服务"解决！
