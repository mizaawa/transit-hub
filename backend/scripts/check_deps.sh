#!/bin/bash
# 启动前依赖检查脚本

set -e

echo "=== Transit Hub Dependency Check ==="
echo ""

# 检查环境变量
echo "[1/4] Checking environment variables..."
required_vars=(
    "DATABASE_URL"
    "REDIS_ADDR"
    "JWT_SECRET"
    "ADMIN_EMAIL"
    "ADMIN_PASSWORD"
)

missing_vars=()
for var in "${required_vars[@]}"; do
    if [ -z "${!var}" ]; then
        missing_vars+=("$var")
        echo "  ✗ $var is not set"
    else
        echo "  ✓ $var is set"
    fi
done

if [ ${#missing_vars[@]} -ne 0 ]; then
    echo ""
    echo "Error: Missing required environment variables"
    exit 1
fi

echo ""

# 检查 PostgreSQL 连接
echo "[2/4] Checking PostgreSQL connection..."
if command -v psql &> /dev/null; then
    if psql "$DATABASE_URL" -c "SELECT 1" &> /dev/null; then
        echo "  ✓ PostgreSQL is reachable"
    else
        echo "  ✗ Cannot connect to PostgreSQL"
        echo "  Database URL: $DATABASE_URL"
        exit 1
    fi
else
    echo "  ⚠ psql not found, skipping connection test"
fi

echo ""

# 检查 Redis 连接
echo "[3/4] Checking Redis connection..."
if command -v redis-cli &> /dev/null; then
    REDIS_HOST=$(echo "$REDIS_ADDR" | cut -d: -f1)
    REDIS_PORT=$(echo "$REDIS_ADDR" | cut -d: -f2)
    
    if [ -n "$REDIS_PASSWORD" ]; then
        if redis-cli -h "$REDIS_HOST" -p "$REDIS_PORT" -a "$REDIS_PASSWORD" ping &> /dev/null; then
            echo "  ✓ Redis is reachable"
        else
            echo "  ✗ Cannot connect to Redis"
            echo "  Redis address: $REDIS_ADDR"
            exit 1
        fi
    else
        if redis-cli -h "$REDIS_HOST" -p "$REDIS_PORT" ping &> /dev/null; then
            echo "  ✓ Redis is reachable"
        else
            echo "  ✗ Cannot connect to Redis"
            echo "  Redis address: $REDIS_ADDR"
            exit 1
        fi
    fi
else
    echo "  ⚠ redis-cli not found, skipping connection test"
fi

echo ""

# 检查端口可用性
echo "[4/4] Checking port availability..."
PORT="${PORT:-8080}"

if command -v lsof &> /dev/null; then
    if lsof -Pi :$PORT -sTCP:LISTEN -t >/dev/null 2>&1; then
        echo "  ✗ Port $PORT is already in use"
        echo "  Process using port:"
        lsof -Pi :$PORT -sTCP:LISTEN
        exit 1
    else
        echo "  ✓ Port $PORT is available"
    fi
elif command -v netstat &> /dev/null; then
    if netstat -tuln | grep -q ":$PORT "; then
        echo "  ✗ Port $PORT is already in use"
        exit 1
    else
        echo "  ✓ Port $PORT is available"
    fi
else
    echo "  ⚠ Cannot check port availability (lsof/netstat not found)"
fi

echo ""
echo "=== All checks passed! ==="
echo ""
