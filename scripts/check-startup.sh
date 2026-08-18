#!/bin/bash

# Transit Hub 启动依赖检查脚本
# 在启动应用前验证所有必需的依赖和配置

set -e

echo "🔍 Transit Hub 启动依赖检查"
echo "=============================="

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

ERRORS=0
WARNINGS=0

# 检查函数
check_pass() {
    echo -e "${GREEN}✓${NC} $1"
}

check_fail() {
    echo -e "${RED}✗${NC} $1"
    ERRORS=$((ERRORS + 1))
}

check_warn() {
    echo -e "${YELLOW}⚠${NC} $1"
    WARNINGS=$((WARNINGS + 1))
}

echo ""
echo "📦 检查 Node.js 环境..."
if command -v node &> /dev/null; then
    NODE_VERSION=$(node --version)
    check_pass "Node.js 已安装: $NODE_VERSION"
    
    # 检查版本是否满足要求 (v18+)
    NODE_MAJOR=$(echo $NODE_VERSION | cut -d'.' -f1 | sed 's/v//')
    if [ "$NODE_MAJOR" -ge 18 ]; then
        check_pass "Node.js 版本满足要求 (>=18)"
    else
        check_fail "Node.js 版本过低，需要 v18 或更高版本"
    fi
else
    check_fail "Node.js 未安装"
fi

echo ""
echo "📦 检查 Go 环境..."
if command -v go &> /dev/null; then
    GO_VERSION=$(go version)
    check_pass "Go 已安装: $GO_VERSION"
else
    check_fail "Go 未安装"
fi

echo ""
echo "📦 检查前端依赖..."
if [ -f "frontend/package.json" ]; then
    check_pass "package.json 存在"
    
    if [ -d "frontend/node_modules" ]; then
        check_pass "node_modules 目录存在"
    else
        check_fail "node_modules 缺失，请运行: cd frontend && npm install"
    fi
else
    check_fail "frontend/package.json 不存在"
fi

echo ""
echo "📦 检查后端依赖..."
if [ -f "backend/go.mod" ]; then
    check_pass "go.mod 存在"
    
    if [ -f "backend/go.sum" ]; then
        check_pass "go.sum 存在"
    else
        check_warn "go.sum 缺失，将在首次构建时生成"
    fi
else
    check_fail "backend/go.mod 不存在"
fi

echo ""
echo "🔧 检查环境配置..."
if [ -f ".env" ]; then
    check_pass ".env 文件存在"
    
    # 检查必需的环境变量
    required_vars=("DATABASE_URL" "REDIS_URL" "PORT" "ADMIN_EMAIL" "ADMIN_PASSWORD")
    for var in "${required_vars[@]}"; do
        if grep -q "^${var}=" .env; then
            value=$(grep "^${var}=" .env | cut -d'=' -f2-)
            if [ -z "$value" ]; then
                check_fail "$var 已定义但为空"
            else
                check_pass "$var 已配置"
            fi
        else
            check_fail "$var 未在 .env 中定义"
        fi
    done
else
    check_fail ".env 文件不存在，请复制 .env.example 并配置"
fi

echo ""
echo "📁 检查关键目录..."
required_dirs=("backend/cmd/api" "backend/internal" "frontend/src")
for dir in "${required_dirs[@]}"; do
    if [ -d "$dir" ]; then
        check_pass "$dir 存在"
    else
        check_fail "$dir 不存在"
    fi
done

echo ""
echo "=============================="
if [ $ERRORS -eq 0 ]; then
    echo -e "${GREEN}✓ 所有检查通过！${NC}"
    if [ $WARNINGS -gt 0 ]; then
        echo -e "${YELLOW}⚠ 有 $WARNINGS 个警告，但不影响启动${NC}"
    fi
    echo ""
    echo "可以安全启动应用:"
    echo "  前端: cd frontend && npm run dev"
    echo "  后端: cd backend && go run cmd/api/main.go"
    exit 0
else
    echo -e "${RED}✗ 发现 $ERRORS 个错误${NC}"
    if [ $WARNINGS -gt 0 ]; then
        echo -e "${YELLOW}⚠ 以及 $WARNINGS 个警告${NC}"
    fi
    echo ""
    echo "请修复上述错误后再启动应用"
    exit 1
fi
