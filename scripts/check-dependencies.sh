#!/bin/bash

# 依赖检查脚本：在启动前验证所有必需的环境和配置
# 用法：./scripts/check-dependencies.sh

set -e

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo "======================================"
echo "Transit Hub - 依赖检查"
echo "======================================"
echo ""

ERRORS=0
WARNINGS=0

# 检查必需的环境变量
check_env() {
    local var_name=$1
    local required=$2
    
    if [ -z "${!var_name}" ]; then
        if [ "$required" = "true" ]; then
            echo -e "${RED}✗${NC} $var_name 未设置（必需）"
            ERRORS=$((ERRORS + 1))
        else
            echo -e "${YELLOW}⚠${NC} $var_name 未设置（可选）"
            WARNINGS=$((WARNINGS + 1))
        fi
    else
        echo -e "${GREEN}✓${NC} $var_name 已设置"
    fi
}

# 检查端口是否被占用
check_port() {
    local port=$1
    if command -v lsof &> /dev/null; then
        if lsof -Pi :$port -sTCP:LISTEN -t >/dev/null 2>&1; then
            echo -e "${YELLOW}⚠${NC} 端口 $port 已被占用"
            WARNINGS=$((WARNINGS + 1))
        else
            echo -e "${GREEN}✓${NC} 端口 $port 可用"
        fi
    else
        echo -e "${YELLOW}⚠${NC} 无法检查端口 $port (lsof 未安装)"
    fi
}

# 检查数据库连接
check_database() {
    if [ -z "$DATABASE_URL" ]; then
        echo -e "${RED}✗${NC} 无法检查数据库连接：DATABASE_URL 未设置"
        return
    fi
    
    if command -v psql &> /dev/null; then
        if psql "$DATABASE_URL" -c "SELECT 1" >/dev/null 2>&1; then
            echo -e "${GREEN}✓${NC} 数据库连接正常"
        else
            echo -e "${RED}✗${NC} 数据库连接失败"
            ERRORS=$((ERRORS + 1))
        fi
    else
        echo -e "${YELLOW}⚠${NC} 跳过数据库连接检查 (psql 未安装)"
        WARNINGS=$((WARNINGS + 1))
    fi
}

# 检查 Redis 连接
check_redis() {
    local redis_url="${REDIS_URL:-redis://127.0.0.1:6379/0}"
    
    # 从 URL 中提取主机和端口
    local redis_host=$(echo "$redis_url" | sed -n 's|redis://\([^:]*\).*|\1|p')
    local redis_port=$(echo "$redis_url" | sed -n 's|redis://[^:]*:\([0-9]*\).*|\1|p')
    
    redis_host=${redis_host:-127.0.0.1}
    redis_port=${redis_port:-6379}
    
    if command -v redis-cli &> /dev/null; then
        if redis-cli -h "$redis_host" -p "$redis_port" ping >/dev/null 2>&1; then
            echo -e "${GREEN}✓${NC} Redis 连接正常"
        else
            echo -e "${RED}✗${NC} Redis 连接失败"
            ERRORS=$((ERRORS + 1))
        fi
    else
        echo -e "${YELLOW}⚠${NC} 跳过 Redis 连接检查 (redis-cli 未安装)"
        WARNINGS=$((WARNINGS + 1))
    fi
}

# 检查必需的文件和目录
check_files() {
    local files=("backend/cmd/api/main.go" "frontend/package.json")
    
    for file in "${files[@]}"; do
        if [ -f "$file" ]; then
            echo -e "${GREEN}✓${NC} $file 存在"
        else
            echo -e "${RED}✗${NC} $file 不存在"
            ERRORS=$((ERRORS + 1))
        fi
    done
}

# 检查 Go 环境
check_go() {
    if command -v go &> /dev/null; then
        local go_version=$(go version | awk '{print $3}')
        echo -e "${GREEN}✓${NC} Go 已安装 ($go_version)"
    else
        echo -e "${RED}✗${NC} Go 未安装"
        ERRORS=$((ERRORS + 1))
    fi
}

# 检查 Node.js 环境
check_node() {
    if command -v node &> /dev/null; then
        local node_version=$(node --version)
        echo -e "${GREEN}✓${NC} Node.js 已安装 ($node_version)"
    else
        echo -e "${RED}✗${NC} Node.js 未安装"
        ERRORS=$((ERRORS + 1))
    fi
}

# 加载 .env 文件
if [ -f ".env" ]; then
    echo "加载 .env 文件..."
    export $(grep -v '^#' .env | xargs)
fi

if [ -f "backend/.env" ]; then
    echo "加载 backend/.env 文件..."
    export $(grep -v '^#' backend/.env | xargs)
fi

echo ""
echo "1. 检查环境变量"
echo "------------------------"
check_env "DATABASE_URL" "true"
check_env "REDIS_URL" "false"
check_env "ADMIN_EMAIL" "false"
check_env "ADMIN_PASSWORD" "false"
check_env "PORT" "false"

echo ""
echo "2. 检查运行环境"
echo "------------------------"
check_go
check_node

echo ""
echo "3. 检查项目文件"
echo "------------------------"
check_files

echo ""
echo "4. 检查端口可用性"
echo "------------------------"
PORT=${PORT:-5478}
check_port "$PORT"

echo ""
echo "5. 检查数据库连接"
echo "------------------------"
check_database

echo ""
echo "6. 检查 Redis 连接"
echo "------------------------"
check_redis

echo ""
echo "======================================"
echo "检查完成"
echo "======================================"
echo ""

if [ $ERRORS -gt 0 ]; then
    echo -e "${RED}发现 $ERRORS 个错误${NC}"
    echo "请修复上述错误后再启动服务"
    exit 1
elif [ $WARNINGS -gt 0 ]; then
    echo -e "${YELLOW}发现 $WARNINGS 个警告${NC}"
    echo "服务可能可以启动，但建议检查警告项"
    exit 0
else
    echo -e "${GREEN}所有检查通过！${NC}"
    echo "可以安全启动服务"
    exit 0
fi
