# TransitHub

[![Go](https://img.shields.io/badge/Go-1.25+-00ADD8.svg)](https://golang.org/)
[![Vue](https://img.shields.io/badge/Vue-3.5+-4FC08D.svg)](https://vuejs.org/)
[![PostgreSQL](https://img.shields.io/badge/PostgreSQL-16+-336791.svg)](https://www.postgresql.org/)
[![Redis](https://img.shields.io/badge/Redis-7+-DC382D.svg)](https://redis.io/)
[![Docker](https://img.shields.io/badge/Docker-Ready-2496ED.svg)](https://www.docker.com/)

面向 sub2api / new-api 自托管 API 服务的多上游运营管理中心。

> 本仓库是 [deviseo/transit-hub](https://github.com/deviseo/transit-hub) 的自维护分支，
> 由 [mizaawa/transit-hub](https://github.com/mizaawa/transit-hub) 独立维护：修复了若干上游遗留缺陷、
> 移除了 README 中的推广内容，默认服务端口改为 `5478`，并且不发布公共镜像（请自行构建）。

## 重要说明

使用本项目之前，请先阅读以下内容：

- **上游平台规则风险**：TransitHub 用于帮助管理员连接和操作上游后台平台。请确认你的使用方式符合所有上游平台的服务条款。
- **合规使用**：请仅在你所在国家或地区法律法规允许的范围内使用本项目。禁止用于绕过授权、滥用上游服务，或操作你无权管理的账号。
- **自部署责任**：你需要自行保护管理员凭据、数据库备份、网络访问权限和部署密钥。
- **免责声明**：本项目仅用于技术学习。你需要自行确保使用方式符合适用法律法规和上游平台规则。因使用本项目导致的服务中断、账号限制、数据丢失或其他直接/间接损失，作者不承担责任。

## 项目概览

TransitHub 是一个自部署的后台运营中心，用于管理多个上游站点和管理员工作区。它关注真实运营工作流：连接上游平台、追踪余额和分组倍率、查看仪表盘指标、配置通知，并运行可定时恢复原倍率的分组活动调价。

项目由 Go 后端和 Vue 3 管理前端组成，使用 PostgreSQL 和 Redis。

## 功能特性

- **管理员工作区管理** - 在多个管理员账号/工作区之间切换，并隔离工作区数据。
- **上游站点管理** - 添加、同步、查看和管理上游站点，并缓存关键指标。
- **仪表盘指标** - 查看实时和历史运营数据，包括余额、成本、趋势、分组用量和上游下钻明细。
- **分组倍率追踪** - 记录分组倍率快照、变动、历史、平台标签和自定义分组类型。
- **活动调价** - 创建立即或定时的调价活动，更新选中的 admin 分组，并在活动结束后恢复原倍率。
- **自动调价支持** - 基于上游倍率变化，为映射分组配置自动调价规则。
- **通知渠道** - 配置钉钉、企业微信、飞书、QQ 和 Telegram 机器人，用于余额预警、倍率变化、自动调价和活动通知。
- **工单与嵌入页** - 工单管理、排行榜和抽奖嵌入页，支持独立 embed token 鉴权。
- **系统设置** - 管理刷新间隔、通知策略、邮件模板和运行时展示配置。

## 技术栈

| 组件 | 技术 |
|------|------|
| 后端 | Go 1.25, net/http, pgx |
| 前端 | Vue 3.5, Vite, TypeScript, TailwindCSS, vue-i18n |
| 数据库 | PostgreSQL 16+ |
| 缓存 / 会话 | Redis 7+ |
| 部署 | Docker, Docker Compose |

## 快速开始

本仓库**不发布公共镜像**，部署前必须先自行构建镜像。完整流程如下：

```bash
# 1. 克隆仓库
git clone https://github.com/mizaawa/transit-hub.git transit-hub
cd transit-hub

# 2. 构建镜像（Dockerfile 在 deploy/，但构建上下文必须是仓库根目录）
docker build -f deploy/Dockerfile -t transithub:local .

# 3. 修改部署配置：替换所有 change-this-* 占位值
#    - DATABASE_URL 与 POSTGRES_PASSWORD 中的数据库密码必须一致
#    - ADMIN_EMAIL / ADMIN_PASSWORD 为首次启动创建的管理员账号
#    编辑 deploy/docker-compose.prod.yml

# 4. 启动
docker compose -f deploy/docker-compose.prod.yml up -d
```

访问地址（默认端口 `5478`）：

```text
http://YOUR_SERVER_IP:5478
```

## 自行构建

### 构建 Docker 镜像

Dockerfile 位于 `deploy/`，但它需要读取 `frontend/` 和 `backend/` 两个目录，
因此**构建上下文必须是仓库根目录**，用 `-f` 指定 Dockerfile 路径：

```bash
# 在仓库根目录执行。注意结尾的 "." 就是构建上下文
docker build -f deploy/Dockerfile -t transithub:local .
```

镜像分三个阶段构建，无需本地安装 Go 和 Node：

1. `node:22-alpine` 构建前端静态文件（输出到 `/app/public`）
2. `golang:1.25-alpine` 编译后端二进制（静态链接，`CGO_ENABLED=0`）
3. `alpine:3.20` 作为运行时，仅包含二进制和前端产物

可选构建参数：

| 参数 | 默认值 | 说明 |
|------|--------|------|
| `VITE_API_BASE_URL` | `/api` | 前端请求的 API 前缀。前后端同源部署时保持默认即可 |

```bash
# 仅在把前端单独部署到其他域名时才需要覆盖
docker build -f deploy/Dockerfile \
  --build-arg VITE_API_BASE_URL=https://api.example.com/api \
  -t transithub:local .
```

> **注意**：不要把 `VITE_API_BASE_URL` 设为空值。留空会让前端请求丢掉 `/api` 前缀，
> 命中前端 history 回退拿到 HTML，从而报 `Unexpected token '<' ... is not valid JSON`。
> 当前代码已对空值做了兜底（回退到 `/api`），但仍建议显式保持默认。

### 打标签并推送到自己的镜像仓库

```bash
# 以 GitHub Container Registry 为例
docker tag transithub:local ghcr.io/mizaawa/transithub:v0.1.15
docker push ghcr.io/mizaawa/transithub:v0.1.15
```

推送后把 `deploy/docker-compose.prod.yml` 里的 `image:` 改成带**固定版本号**的地址。
不要使用 `:latest`，否则无法确定线上实际运行的是哪个版本。

### 不使用 Docker 直接构建二进制

需要本地安装 Go 1.25+ 和 Node 22+：

```bash
# 前端：产物在 frontend/dist
cd frontend
npm ci
npm run build

# 后端：产物是单个静态二进制
cd ../backend
CGO_ENABLED=0 go build -o transithub-api ./cmd/api

# 运行时把前端产物目录指给后端
PUBLIC_DIR=../frontend/dist \
DATABASE_URL='postgres://postgres:postgres@localhost:5432/transithub?sslmode=disable' \
./transithub-api
```

## 部署说明

### 服务组成

生产 compose 包含三个服务：

- `app`：TransitHub 应用容器（Go 后端 + 已构建的前端静态文件），监听 `5478`
- `postgres`：PostgreSQL 数据库
- `redis`：管理员会话、缓存和定时任务（已开启 AOF 持久化）

### 端口

默认服务端口为 `5478`。修改方式：

```yaml
# deploy/docker-compose.prod.yml
ports:
  # 左侧是宿主机端口，可任意修改；右侧必须与 PORT 环境变量一致
  - "8080:5478"
environment:
  PORT: "5478"
```

### 持久化数据

默认存放在仓库根目录的 `data/`：

```text
data/postgres         # 数据库
data/redis            # Redis（含 admin 会话）
data/ticket-uploads   # 工单图片附件
```

`data/ticket-uploads` 挂载到容器的 `TICKET_UPLOAD_DIR`（默认 `/app/data/ticket-uploads`），
不会作为公开静态目录对外暴露。重建 `app` 容器前请确认该 volume 已存在，
否则图片文件会丢失（数据库里的附件 metadata 不受影响）。

### 环境变量

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `PORT` | `5478` | 后端监听端口 |
| `DATABASE_URL` | 无（必填） | PostgreSQL 连接串 |
| `REDIS_URL` | `redis://127.0.0.1:6379/0` | Redis 连接串 |
| `PUBLIC_DIR` | `/app/public` | 前端静态文件目录 |
| `ADMIN_EMAIL` | 无 | 首次空库启动时创建的管理员邮箱 |
| `ADMIN_PASSWORD` | 无 | 首次空库启动时创建的管理员密码 |
| `ALLOW_PUBLIC_REGISTER` | `true` | 生产环境建议设为 `false` |
| `CORS_ORIGINS` | 空 | 逗号分隔的允许来源；留空表示不限制 |
| `TICKET_UPLOAD_DIR` | `data/ticket-uploads` | 工单附件存储目录 |
| `SMTP_ENCRYPTION_KEY` | 空 | 可选，见下文 |
| `LOTTERY_ALLOW_PRIVATE_SUB2API_TARGETS` | `false` | 仅本地联调用，线上必须保持 `false` |

`SMTP_ENCRYPTION_KEY` 仅当需要在「系统设置 - 邮件设置」中保存 SMTP 密码或发送测试邮件时才需要配置，
缺失不影响应用启动，也不影响任何非 SMTP 功能。生成方式：

```bash
openssl rand -base64 32
```

该值必须是 base64 编码的 32 字节随机值，且一经设置需要长期稳定保存；
更换 key 后旧的 SMTP 密码密文将无法解密，需要重新填写并保存密码。

### 升级

```bash
git pull
docker build -f deploy/Dockerfile -t transithub:local .
docker compose -f deploy/docker-compose.prod.yml up -d
```

数据库迁移在应用启动时自动执行，并通过 PostgreSQL advisory lock 串行化，
多副本同时启动也不会重复执行同一个迁移。

## 本地开发

### 启动依赖服务

```bash
docker compose -f deploy/docker-compose.yml up -d
```

这会在本地开放 PostgreSQL `5432` 和 Redis `6379`。

### 后端

```bash
cd backend
go run ./cmd/api
```

常用环境变量（可写入 `backend/.env`）：

```env
PORT=5478
DATABASE_URL=postgres://postgres:postgres@localhost:5432/transithub?sslmode=disable
REDIS_URL=redis://127.0.0.1:6379/0
ADMIN_EMAIL=admin@example.com
ADMIN_PASSWORD=transithub
ALLOW_PUBLIC_REGISTER=true
# 仅限本地抽奖联调；线上必须保持 false 或不设置
LOTTERY_ALLOW_PRIVATE_SUB2API_TARGETS=false
```

### 前端

```bash
cd frontend
npm install
npm run dev
```

开发服务器监听 `5444`，并把 `/api` 代理到 `http://127.0.0.1:5478`
（与后端默认端口一致）。如果你改了后端 `PORT`，需要同步修改 `frontend/vite.config.ts` 中的代理目标，
否则代理会静默失败并返回 HTML，前端报 `Unexpected token '<'`。

## 验证命令

提交变更前建议运行：

```bash
cd backend
go build ./...
go vet ./...
go test ./...

cd ../frontend
npm run build

cd ..
docker compose -f deploy/docker-compose.yml config
docker compose -f deploy/docker-compose.prod.yml config
```

## 项目结构

```text
transit-hub/
├── backend/                  # Go 后端服务
│   ├── cmd/api/              # API 入口
│   ├── internal/config/      # 运行配置
│   ├── internal/database/    # PostgreSQL、Redis、迁移
│   ├── internal/httpserver/  # HTTP 服务组装和中间件
│   ├── internal/shared/      # 跨模块公共代码（鉴权上下文、JSON helper）
│   └── internal/modules/     # 领域模块
│       ├── admin_accounts/   # 工作区/管理员账号
│       ├── auth/             # 登录注册
│       ├── connection_health/# 分组健康探活
│       ├── dashboard/        # 仪表盘与指标
│       ├── group_rate_campaigns/ # 活动调价
│       ├── group_rates/      # 分组倍率
│       ├── leaderboard/      # 排行榜
│       ├── lottery/          # 抽奖
│       ├── mass_email/       # 批量邮件
│       ├── my_sites/         # 分组映射与自动调价
│       ├── settings/         # 系统设置与通知
│       ├── tickets/          # 工单
│       └── upstream/         # 上游平台客户端
├── frontend/                 # Vue 3 管理前端
│   └── src/
│       ├── lib/apiClient.ts  # 统一请求层
│       ├── locales/          # i18n（默认中文）
│       └── modules/          # 前端业务模块
├── deploy/                   # Dockerfile 和 Compose 文件
└── data/                     # 持久化运行数据
```

## 核心工作流

- 工作区隔离的多上游运营：每个 admin workspace 独立管理，可连接多个 sub2api/new-api 上游并执行同步与账号管理。
- 分组倍率工作流：跟踪最新上游倍率，支持搜索和筛选、分组关联，以及按活动组织调价。
- 已对接分组自动调价：支持手动或同步后执行，可配置策略、查看执行状态并发送通知。
- 日常运维面板：覆盖仪表盘指标、连接健康、工单和邮件/模板管理。

## License

本项目采用 GNU Lesser General Public License v3.0（LGPL-3.0-only）协议，详见 [LICENSE](LICENSE)。
