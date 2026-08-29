#!/usr/bin/env bash
# 种子:创建固定的测试管理员账号 test_admin / Test@123456(幂等,可重复执行)
# 用途:集成测试/清库会清空 users 表,运行本脚本即可恢复固定账号。
# 用法:bash scripts/seed-test-admin.sh  或  make seed-admin
set -euo pipefail

cd "$(dirname "$0")/.."
BASE="${BASE:-http://localhost:8080/api}"

USERNAME="test_admin"
PASSWORD="Test@123456"
INVITE="TEST-ADMIN"
DISPLAY="测试管理员"

# 后端未启动则自动拉起(后台)
if ! curl -s -o /dev/null --max-time 2 "$BASE/health"; then
  echo "==> 后端未运行,启动中…"
  nohup go run ./cmd/server > /tmp/labnexus-server.log 2>&1 &
  for i in $(seq 1 30); do
    curl -s -o /dev/null --max-time 1 "$BASE/health" && break
    sleep 1
  done
fi

echo "==> 1. 清理旧账号(幂等)"
docker exec labnexus-postgres psql -U labnexus -d labnexus -c "
  DELETE FROM users WHERE username='${USERNAME}';
  DELETE FROM invite_codes WHERE code='${INVITE}';" >/dev/null

echo "==> 2. 生成邀请码并注册"
docker exec labnexus-postgres psql -U labnexus -d labnexus -c \
  "INSERT INTO invite_codes (id, code, created_by) VALUES (gen_random_uuid(), '${INVITE}', '00000000-0000-0000-0000-000000000000');" >/dev/null
RESP=$(curl -s -X POST "$BASE/auth/register" -H 'Content-Type: application/json' \
  -d "{\"invite_code\":\"${INVITE}\",\"username\":\"${USERNAME}\",\"display_name\":\"${DISPLAY}\",\"password\":\"${PASSWORD}\"}")
echo "$RESP" | python3 -c "import sys,json; d=json.load(sys.stdin); print('注册:', d.get('user',{}).get('username'), d.get('user',{}).get('role'))" 2>/dev/null \
  || { echo "注册失败: $RESP"; exit 1; }

echo "==> 3. 提升为 admin"
docker exec labnexus-postgres psql -U labnexus -d labnexus -c \
  "UPDATE users SET role='admin' WHERE username='${USERNAME}';" >/dev/null

echo "==> 4. 验证登录"
LOGIN=$(curl -s -X POST "$BASE/auth/login" -H 'Content-Type: application/json' \
  -d "{\"username\":\"${USERNAME}\",\"password\":\"${PASSWORD}\"}")
echo "$LOGIN" | python3 -c "
import sys,json
d=json.load(sys.stdin)
assert d.get('access_token'), '登录失败'
print('登录 OK | 角色:', d.get('user',{}).get('role'))" || { echo "登录失败: $LOGIN"; exit 1; }

echo ""
echo "✅ 测试管理员已就绪: ${USERNAME} / ${PASSWORD}"
