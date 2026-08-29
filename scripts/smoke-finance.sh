#!/usr/bin/env bash
# F10 经费管理 冒烟验收脚本(幂等)
# 前置:docker compose up -d 且后端已在 :8080 运行
set -euo pipefail

cd "$(dirname "$0")/.."
BASE="${BASE:-http://localhost:8080/api}"

echo "==> 0. 清理冒烟数据(幂等)"
docker exec labnexus-postgres psql -U labnexus -d labnexus -c "
  DELETE FROM turnover_submissions; DELETE FROM turnover_items; DELETE FROM participants;
  DELETE FROM turnover_batches; DELETE FROM transactions; DELETE FROM accounts;
  DELETE FROM users WHERE username IN ('smoke_fin', 'smoke_fin_stu');
  DELETE FROM invite_codes WHERE code IN ('SMOKE-FIN', 'SMOKE-FINS');" >/dev/null
docker exec labnexus-postgres psql -U labnexus -d labnexus -c "
  INSERT INTO invite_codes (id, code, created_by) VALUES (gen_random_uuid(), 'SMOKE-FIN', '00000000-0000-0000-0000-000000000000');
  INSERT INTO invite_codes (id, code, created_by) VALUES (gen_random_uuid(), 'SMOKE-FINS', '00000000-0000-0000-0000-000000000000');" >/dev/null

echo "==> 1. 注册经费负责人(admin)+ 普通学生"
TOK=$(curl -s -X POST "$BASE/auth/register" -H 'Content-Type: application/json' \
  -d '{"invite_code":"SMOKE-FIN","username":"smoke_fin","display_name":"经费冒烟","password":"password123"}' \
  | python3 -c "import sys,json;print(json.load(sys.stdin)['access_token'])")
AUTH="Authorization: Bearer $TOK"
curl -s -X POST "$BASE/auth/register" -H 'Content-Type: application/json' \
  -d '{"invite_code":"SMOKE-FINS","username":"smoke_fin_stu","display_name":"普通学生","password":"password123"}' >/dev/null
STU=$(curl -s -X POST "$BASE/auth/login" -H 'Content-Type: application/json' \
  -d '{"username":"smoke_fin_stu","password":"password123"}' \
  | python3 -c "import sys,json;print(json.load(sys.stdin)['access_token'])")
STUAUTH="Authorization: Bearer $STU"
docker exec labnexus-postgres psql -U labnexus -d labnexus -c \
  "UPDATE users SET role='admin' WHERE username='smoke_fin';" >/dev/null

echo "==> 2. 学生访问应 403"
curl -s -o /dev/null -w "student /finance/batches: %{http_code}\n" "$BASE/finance/batches" -H "$STUAUTH"

echo "==> 3. 建批次 + 手动明细"
BID=$(curl -s -X POST "$BASE/finance/batches" -H "$AUTH" -H 'Content-Type: application/json' \
  -d '{"name":"2026-08","note":"冒烟批次"}' \
  | python3 -c "import sys,json;print(json.load(sys.stdin)['batch']['id'])")
IID=$(curl -s -X POST "$BASE/finance/batches/$BID/items" -H "$AUTH" -H 'Content-Type: application/json' \
  -d '{"name":"张同学","student_no":"2023001","date":"2026-08-20","payroll_amount":250000,"tip_amount":10000}' \
  | python3 -c "import sys,json;print(json.load(sys.stdin)['item']['id'])")
echo "batch=$BID item=$IID"

echo "==> 4. 上交 2100 元 → 资金池 +2100"
curl -s -X POST "$BASE/finance/items/$IID/submit" -H "$AUTH" -H 'Content-Type: application/json' \
  -d '{"amount":210000,"date":"2026-08-21","note":"微信"}' -o /dev/null -w "submit1: %{http_code}\n"
curl -s "$BASE/finance/ledger" -H "$AUTH" | python3 -c "
import sys,json; d=json.load(sys.stdin)
print('balance:', d['balance'])
assert d['balance']==210000, '资金池应为 210000'
print('资金池 OK')"

echo "==> 5. 补交 300 → 交清"
curl -s -X POST "$BASE/finance/items/$IID/submit" -H "$AUTH" -H 'Content-Type: application/json' \
  -d '{"amount":30000,"date":"2026-08-28","note":"补交"}' -o /dev/null -w "submit2: %{http_code}\n"

echo "==> 6. 批次完成"
curl -s -X POST "$BASE/finance/batches/$BID/complete" -H "$AUTH" -o /dev/null -w "complete: %{http_code}\n"

echo "==> 7. 参与同学库 + 历史账单"
curl -s "$BASE/finance/participants" -H "$AUTH" | python3 -c "
import sys,json; d=json.load(sys.stdin)
print('participants:', [(p['name'], p['student_no']) for p in d['participants']])
assert len(d['participants'])==1, '应去重为 1 人'
print('参与同学 OK')"

echo "==> 8. 导师补充 + 支出"
curl -s -X POST "$BASE/finance/ledger/income" -H "$AUTH" -H 'Content-Type: application/json' \
  -d '{"amount":500000,"date":"2026-08-25","note":"导师补充"}' -o /dev/null -w "income: %{http_code}\n"
curl -s -X POST "$BASE/finance/ledger/expense" -H "$AUTH" -H 'Content-Type: application/json' \
  -d '{"amount":150000,"date":"2026-08-26","note":"发劳务"}' -o /dev/null -w "expense: %{http_code}\n"
curl -s "$BASE/finance/ledger" -H "$AUTH" | python3 -c "
import sys,json; d=json.load(sys.stdin)
# 2100+300+5000-1500 = 5900 元
print('balance:', d['balance'])
assert d['balance']==590000, f'余额应为 590000, got {d[\"balance\"]}'
print('资金池最终余额 OK')"

echo ""
echo "SMOKE F10 OK ✅"
