#!/bin/bash

# 健壮性检查脚本：在启动前验证所有依赖项

set -e

echo "=========================================="
echo "Transit Hub 健康检查"
echo "=========================================="
echo ""

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# 检查计数
PASSED=0
FAILED=0
WARNINGS=0

check_pass() {
    echo -e "${GREEN}✓${NC} $1"
    PASSED=$((PASSED + 1))
}

check_fail() {
    echo -e "${RED}✗${NC} $1"
    FAILED=$((FAILED + 1))
}

check_warn() {
    echo -e "${YELLOW}⚠${NC} $1"
    WARNINGS=$((WARNINGS + 1))
}

echo "1. 检查环境变量配置"
echo "-------------------------------------------"

if [ -f ".env" ]; then
    check_pass ".env 文件存在"
    
    # 检查必需的环境变量
    required_vars=("DATABASE_URL" "REDIS_URL" "JWT_SECRET" "PORT")
    for var in "${required_vars[@]}"; do
        if grep -q "^${var}=" .env; then
            value=$(grep "^${var}=" .env | cut -d '=' -f2-)
            if [ -z "$value" ] || [ "$value" = '""' ] || [ "$value" = "''" ]; then
                check_fail "$var 已配置但值为空"
            else
                check_pass "$var 已配置"
            fi
        else
            check_fail "$var 未在 .env 中配置"
        fi
    done
else
    check_fail ".env 文件不存在"
    echo "  请从 .env.example 复制并配置环境变量"
fi

echo ""
echo "2. 检查数据库连接"
echo "-------------------------------------------"

if [ -f ".env" ]; then
    source .env
    
    if [ -n "$DATABASE_URL" ]; then
        # 尝试解析 DATABASE_URL
        if [[ $DATABASE_URL =~ postgres://([^:]+):([^@]+)@([^:]+):([^/]+)/(.+) ]]; then
            DB_HOST="${BASH_REMATCH[3]}"
            DB_PORT="${BASH_REMATCH[4]}"
            
            # 检查端口是否可达
            if command -v nc &> /dev/null; then
                if timeout 3 nc -z "$DB_HOST" "$DB_PORT" 2>/dev/null; then
                    check_pass "数据库端口 $DB_HOST:$DB_PORT 可达"
                else
                    check_fail "数据库端口 $DB_HOST:$DB_PORT 不可达"
                fi
            else
                check_warn "nc 命令不可用，跳过端口检查"
            fi
        else
            check_warn "DATABASE_URL 格式无法解析，跳过端口检查"
        fi
    fi
fi

echo ""
echo "3. 检查 Redis 连接"
echo "-------------------------------------------"

if [ -f ".env" ]; then
    source .env
    
    if [ -n "$REDIS_URL" ]; then
        # 尝试解析 REDIS_URL
        if [[ $REDIS_URL =~ redis://([^:]+):([^/]+) ]]; then
            REDIS_HOST="${BASH_REMATCH[1]}"
            REDIS_PORT="${BASH_REMATCH[2]}"
            
            if command -v nc &> /dev/null; then
                if timeout 3 nc -z "$REDIS_HOST" "$REDIS_PORT" 2>/dev/null; then
                    check_pass "Redis 端口 $REDIS_HOST:$REDIS_PORT 可达"
                else
                    check_fail "Redis 端口 $REDIS_HOST:$REDIS_PORT 不可达"
                fi
            else
                check_warn "nc 命令不可用，跳过端口检查"
            fi
        else
            check_warn "REDIS_URL 格式无法解析，跳过端口检查"
        fi
    fi
fi

echo ""
echo "4. 检查 Go 模块依赖"
echo "-------------------------------------------"

if [ -f "go.mod" ]; then
    check_pass "go.mod 存在"
    
    if [ -d "vendor" ]; then
        check_pass "vendor 目录存在"
    else
        check_warn "vendor 目录不存在，将使用模块缓存"
    fi
    
    # 检查 go.sum 是否存在
    if [ -f "go.sum" ]; then
        check_pass "go.sum 存在"
    else
        check_warn "go.sum 不存在，建议运行 go mod tidy"
    fi
else
    check_fail "go.mod 不存在"
fi

echo ""
echo "5. 检查必需的 Go 包"
echo "-------------------------------------------"

required_packages=(
    "github.com/jackc/pgx/v5"
    "github.com/redis/go-redis/v9"
    "github.com/golang-jwt/jwt/v5"
)

for pkg in "${required_packages[@]}"; do
    if go list -m "$pkg" &> /dev/null; then
        check_pass "$pkg 已安装"
    else
        check_fail "$pkg 未安装"
    fi
done

echo ""
echo "=========================================="
echo "检查总结"
echo "=========================================="
echo -e "${GREEN}通过: $PASSED${NC}"
echo -e "${YELLOW}警告: $WARNINGS${NC}"
echo -e "${RED}失败: $FAILED${NC}"
echo ""

if [ $FAILED -gt 0 ]; then
    echo -e "${RED}发现 $FAILED 个严重问题，请修复后再启动服务${NC}"
    exit 1
elif [ $WARNINGS -gt 0 ]; then
    echo -e "${YELLOW}发现 $WARNINGS 个警告，建议检查后再启动服务${NC}"
    exit 0
else
    echo -e "${GREEN}所有检查通过，可以安全启动服务${NC}"
    exit 0
fi
