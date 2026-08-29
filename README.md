# LabNexus

**课题组内部社区平台:科研朋友圈 · 知识库 · 进度监督**

一个为 10 人以内高校课题组打造的私有平台,把三个真实痛点装进一个系统:

1. **资源管理**——链接/文件统一入库、打标签、描述、下载与浏览器内预览(告别"微信文件 7 天过期后重新找导师要");
2. **笔记与社区**——个人笔记空间(自定义目录)+ 可公开帖子(点赞/评论),像"科研朋友圈";
3. **项目监督**——任务看板 + 状态机 + 里程碑,进度共享与自我监督(告别拖延)。

> 产品决策依据:[`docs/product-context.md`](docs/product-context.md)(自包含摘要;完整 PRD 在 `../research-group-app/PRD.md`)。
> 接手开发前先读 [`AGENTS.md`](AGENTS.md)(AI 工具会自动读取)。

---

## 功能全景(已实现,全部有测试)

| 模块 | 功能 | 说明 |
|---|---|---|
| **F1 账号** | 邀请码注册(注册即登录)、JWT + refresh 轮换、登出撤销、个人资料 | 防外人员混入 |
| **F2 空间目录** | 多级目录树(建/改/删,非空拦截)、文档挂载 | 会议记录/近期工作/日常… |
| **F3 文档** | **笔记 = 帖子**(私有/公开一键切换)、Markdown、标签、软删除 | 统一内容模型,核心差异化 |
| **F4 信息流** | 公开帖时间线(latest/hot + 分页)、点赞 toggle、评论(一级回复) | 高频社区功能 |
| **F5 标签** | 全局标签库、标签内容页(可见性过滤) | 跨内容检索 |
| **F6 搜索** | **跨类型聚合**:文档 + 资源 + 任务同词搜索,type 定向 | 标题优先排序 |
| **F7 资源库** | link/file 两种资源、文件上传(扩展名+MIME 双校验,普通 50MB/视频 100MB)、下载/预览(PDF/图片/文本/视频)、标签/描述/类型/关键词筛选 | 本地磁盘存储;论文以 PDF 上传 |
| **F9 项目任务** | 项目 + 成员管理 + 里程碑 + 任务看板 + **状态机**(todo→in_progress→blocked|done) | 四级权限隔离 |
| **F10 经费管理** | 周转批次、明细(**手动录入 / Excel 导入**)、上交/补交(自动进资金池)、批次汇总/完成、单账户资金池、参与同学历史账单 | 仅 admin+导师可见;收入+支出=总金额 |
| **前端外壳** | 纯 HTML/JS 单页应用,对接全部 API,浏览器直接使用 | 零构建 |

**灵感库**(已记录,暂不开发,按触发条件评估):通知推送、微信机器人、全文搜索升级、导师视图、积分激励等,见 `docs/product-context.md` §2。

---

## 技术栈

| 层 | 选型 |
|---|---|
| 后端 | **Go 1.25 + Gin + GORM + Postgres 16 + Redis 7**(JWT access + opaque refresh) |
| 前端 | 阶段 1 验证:**纯 HTML + 原生 JS**(无构建);正式版规划 React + Vite + Tailwind |
| 架构 | **模块化单体 + MVC 轻量分层**(handler → service → repository),不做微服务 |
| 部署 | 本地 Docker Compose → 0 成本演示(局域网/Cloudflare Tunnel)→ 国内服务器正式 |

---

## 快速开始

### 前置要求

- Docker Desktop(已启动)
- Go 1.25+(可选,仅开发时需要;若只运行可跳过)

### 启动(3 步)

```bash
git clone ssh://git@ssh.github.com:443/alan22333/labnexus.git   # 或 HTTPS: https://github.com/alan22333/labnexus.git
cd labnexus

make up          # 1. 启动 Postgres(宿主机 5433)+ Redis(6380),避开本机已有服务端口
make run         # 2. 启动后端(默认 :8080)

curl http://localhost:8080/api/health   # 3. 应返回 {"status":"ok","db":true}
```

浏览器打开 **http://localhost:8080** 即可使用。

### 生成邀请码(管理端未实现,用 SQL 插入)

```bash
docker exec labnexus-postgres psql -U labnexus -d labnexus -c \
  "INSERT INTO invite_codes (id, code, created_by) VALUES
   (gen_random_uuid(),'YOUR-CODE','00000000-0000-0000-0000-000000000000');"
```

### 环境变量(均有本地开发默认值,生产必须覆盖)

| 变量 | 默认值 | 说明 |
|---|---|---|
| `PORT` | 8080 | HTTP 端口 |
| `DB_HOST/DB_PORT/DB_USER/DB_PASSWORD/DB_NAME` | localhost / 5433 / labnexus | 容器映射 5433 |
| `REDIS_ADDR` | localhost:6380 | 容器映射 6380 |
| `JWT_SECRET` | dev-secret-change-me | **生产必须覆盖** |
| `ACCESS_TOKEN_TTL` / `REFRESH_TOKEN_TTL` | 15m / 720h | token 有效期 |

---

## 使用指南(界面速览)

| 入口 | 能做什么 |
|---|---|
| **信息流** | 看全组公开帖、点赞、评论;「发帖/写笔记」新建(私有=笔记,公开=发帖) |
| **我的空间** | 目录树管理;按目录查看文档;编辑/删除/发布撤回 |
| **资源库** | 建链接(仅 http/https)、上传文件(支持预览/下载);按类型/关键词筛选 |
| **项目** | 建项目 → 加成员 → 建里程碑 → 建任务;看板按状态分列,点按钮流转 |
| **经费**(仅 admin/导师) | 建批次 → 手动/导入明细 → 收款(上交/补交)→ 资金池余额与流水 |
| **标签** | 创建标签、查看标签内容页 |
| **顶栏搜索** | 跨文档/资源/任务三类搜索 |

完整人工验收步骤见 [`docs/manual-acceptance.md`](docs/manual-acceptance.md)(11 个区块,可直接打印打勾)。

---

## 测试体系(三层防线 + 手动验收)

| 层 | 命令 | 说明 |
|---|---|---|
| 单元 + handler | `make check` | service 业务逻辑(内存替身)+ HTTP 层,make check 含 vet/fmt/test/lint/build |
| 集成(端到端) | `make test-integration` | **真实 Postgres+Redis 容器**,含认证/业务流/越权/边界/契约/资源/项目/经费/搜索/前端外壳 |
| 前端 E2E | `node scripts/e2e-frontend.mjs` | mock DOM + 真实 API 全流程(注册→发帖→点赞→评论→空间→资源→项目→标签→搜索) |
| 前端诊断 | `node scripts/diag-frontend.mjs` | node mock 环境定位前端运行时错误 |
| 冒烟脚本 | `./scripts/smoke-*.sh` ×6 | auth/space/doc/resource/project/finance 每功能手动冒烟 |
| 测试管理员 | `make seed-admin` | **固定账号 test_admin / Test@123456**(admin)。集成测试/清库会清空 users,跑一次即恢复 |
| **人工验收** | [`docs/manual-acceptance.md`](docs/manual-acceptance.md) | 给导师/组员用的逐步操作清单 |

---

## 开发规范(SDD + TDD,必读)

> **完整规范:[`docs/standards.md`](docs/standards.md);AI 接手入口:[`AGENTS.md`](AGENTS.md)**

- **SDD(规格驱动)**:每个功能先写规格(`docs/specs/<feature>.md`)→ 评审 → **契约先行**(改接口先改 `docs/api-contract.md`)→ 编码;
- **TDD(测试驱动)**:先写测试看到失败(红)→ 最小实现(绿)→ 重构;铁律:**没有规格不写代码,没有测试不提交**;
- **AI Coding**:规范 §7 定义了完整循环(规格 → AI 生成测试 → AI 生成实现 → 人工 review),以及 AI 产物红线(不得擅改契约/schema/规范、不得跳过测试);
- **质量门禁**:提交前 `make check` 必须全绿。

---

## 协作开发(与朋友一起)

### 分支流程

```bash
git checkout -b feature/xxx        # 每个功能一个分支(命名见下)
# ...开发(SDD+TDD)...
make check                        # 门禁
git push origin feature/xxx        # 推送分支
# 在 GitHub 上发起 Pull Request → 合入 main
```

### 提交信息规范(Conventional Commits)

```
feat(auth): implement F1 account system
fix(web): resolve this-context bug
test(integration): deep API test suite
docs(README): rewrite for collaboration
```

### 协作约定

- **禁止直接推 main**(除一次性初始化);功能走 `feature/<slug>` 分支 + PR;
- **PR 前必过**:`make check` + `make test-integration`(需 `make up`);
- **契约先行**:改动任何接口,先改 `docs/api-contract.md` 再改代码;
- **一人一个领域**:模块按 `internal/<domain>` 隔离,跨模块只走 service 层接口,冲突面小;
- 提交历史保持**一个逻辑变更一个提交**,合入前 rebase main 保持线性。

### 环境对齐

- 端口已避开本机冲突:Postgres **5433** / Redis **6380**(本机 5432/6379 不受影响);
- 若本机 Docker 拉镜像报 Keychain 错误:删除 `~/.docker/config.json` 中的 `"credsStore"`(备份后操作);
- 国内网络访问 GitHub:SSH 走 `ssh.github.com:443`(本仓库 remote 已配置)。

---

## 项目结构

```
labnexus/
├── cmd/server/            # 入口(极薄,只调 app.Build)
├── internal/
│   ├── app/               # 装配:连库/迁移/依赖注入/路由(测试与生产共用)
│   ├── auth/ user/        # F1 账号
│   ├── space/             # F2 空间/目录
│   ├── document/ tag/     # F3/F4/F5/F6 文档/信息流/标签/搜索
│   ├── resource/          # F7 资源库(link/file,下载/预览)
│   ├── project/           # F9 项目/任务
│   ├── finance/           # F10 经费管理(批次/明细/上交/资金池)
│   ├── cache/ token/ middleware/ database/ config/   # 基础设施
├── web/                   # 前端外壳(纯 HTML/JS,后端托管)
├── test/integration/      # 集成测试(build tag: integration)
├── docs/
│   ├── standards.md       # 开发规范(SDD+TDD)
│   ├── api-contract.md    # API 契约(全部端点)
│   ├── schema.sql         # 数据模型权威定义
│   ├── specs/             # 功能规格(含经费管理/前端)
│   ├── manual-acceptance.md  # 人工验收指南
│   └── product-context.md # 产品上下文(自包含)
├── scripts/               # check.sh + 冒烟 ×6 + 前端 diag/e2e
├── Makefile               # up/down/run/build/test/test-integration/lint/check
├── AGENTS.md              # AI 接手指南(自动读取)
└── docker-compose.yml     # Postgres(5433)+ Redis(6380)
```

---

## 里程碑

| 阶段 | 内容 | 状态 |
|---|---|---|
| 阶段 0 | PRD 定稿 + 数据模型 + API 契约 + 骨架 + 规范 | ✅ |
| 阶段 1 | MVP 社区:F1~F6 + 深度接口测试 + 前端外壳 | ✅ |
| 阶段 2 | 资源库/项目任务:F7~F9 + 深度接口测试(2026-08-25:F7 重写为 link/file,去掉 paper/DOI/arXiv) | ✅ |
| 阶段 3 | 经费管理:F10(批次/Excel 导入/上交/资金池,2026-08-29) | ✅ |
| 阶段 4 | 迭代:灵感库功能按触发条件捞回(通知推送/导师视图等)+ 导师参与 | ⏳ 待启动 |

---

## 文档索引

| 文档 | 用途 |
|---|---|
| [`AGENTS.md`](AGENTS.md) | AI/新成员接手指南(第一优先读) |
| [`docs/standards.md`](docs/standards.md) | 开发规范:SDD+TDD、分层、Git、AI 工作流 |
| [`docs/api-contract.md`](docs/api-contract.md) | API 契约(改接口先改它) |
| [`docs/schema.sql`](docs/schema.sql) | 数据库权威定义 |
| [`docs/manual-acceptance.md`](docs/manual-acceptance.md) | 人工验收指南(11 区块) |
| [`docs/product-context.md`](docs/product-context.md) | 产品定位/功能清单/决策记录 |
| `docs/specs/` | 12 份功能规格(每功能一份) |

---

## 常见问题

**Q: `make up` 报端口冲突?**
宿主机 5432/6379 已被本机 Postgres/Redis 占用时,项目容器已改用 5433/6380,一般不会冲突;若仍冲突,改 `docker-compose.yml` 的映射并同步 `config.go` 默认值。

**Q: 拉镜像报 Keychain Error?**
删除 `~/.docker/config.json` 中的 `"credsStore": "desktop"`(先备份),公共镜像无需凭据。

**Q: 访问页面没反应?**
先 `curl http://localhost:8080/api/health` 确认服务;前端改动后请**硬刷新**(Cmd/Ctrl+Shift+R)清缓存。

**Q: 如何重置数据库?**
`docker compose down && docker compose up -d` 会清空卷重建;或 `docker exec labnexus-postgres psql -U labnexus -d labnexus -c "TRUNCATE ..."`。

**Q: 集成测试跳过了?**
`make test-integration` 需要 `make up` 后容器健康;未就绪时用例自动 skip(不阻塞 `make check`)。
