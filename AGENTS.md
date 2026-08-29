# AGENTS.md — LabNexus AI 接手指南

> 你是新接手的 AI 开发助手。读完本文件即可了解项目全貌、约定与当前状态,安全开始开发。
> **强制开发规范:`docs/standards.md`(SDD + TDD 为核心,开发前必读)。**

## 项目是什么

**LabNexus** —— 课题组内部社区平台:科研朋友圈 + 知识库 + 进度监督。
10 人以内高校课题组私有部署;前后端分离;产品上下文见 `docs/product-context.md`(自包含;完整 PRD 在仓库外 `../research-group-app/PRD.md`,以 PRD 的产品决策为准)。

## 技术栈(已定稿,勿改)

| 层 | 选型 |
|---|---|
| 后端 | Go 1.25 + **Gin** + **GORM** + Postgres + Redis(JWT access 15min + refresh 存 Redis) |
| 前端 | 阶段 1 验证期**纯 HTML/JS**(仓库 `labnexus/web/`);正式 **React + Vite + Tailwind + shadcn/ui**(独立仓库 `labnexus-frontend`,见其 README) |
| 架构 | **模块化单体** + **MVC 轻量分层**(`internal/<domain>/{handler,service,repository}`),依赖方向 handler→service→repository |
| 部署 | 本地 docker compose → 0 成本演示(局域网/Cloudflare Tunnel)→ 国内服务器正式 |

## 文档地图(新接手按此顺序读)

| 顺序 | 文档 | 作用 |
|---|---|---|
| 1 | `AGENTS.md`(本文件) | 总入口:全貌、状态、铁律 |
| 2 | `docs/product-context.md` | 产品定位、功能清单、核心设计、**已定决策(勿推翻)** |
| 3 | `docs/standards.md` | 开发规范:SDD+TDD、分层、Git、**AI 工作流与红线** |
| 4 | `docs/api-contract.md` | API 契约(改接口**先改它**) |
| 5 | `docs/schema.sql` | 数据模型权威定义(改表必须同步) |
| 6 | `docs/specs/` | 功能规格(当前开发中功能的唯一需求来源) |

## 当前状态与下一步

- ✅ **阶段 0 完成**:骨架、开发规范、API 契约与 schema 已就绪;
- ✅ **阶段 1 完成**:F1~F6 社区 MVP 已实现并通过单元、handler、集成和前端检查;
- ✅ **阶段 2 完成**:F7~F9 资源库(link/file + 下载/预览)、项目/任务已实现并通过集成测试(F8 文献元数据已废弃,重做后再引入);
- ✅ **阶段 3 完成**:F10 经费管理(批次/明细/Excel 导入/上交/资金池,仅 admin+导师)已实现并通过集成测试;
- ✅ **前端验证壳完成**:纯 HTML/JS shell、诊断、端到端和 smoke 流程已完成;
- ▶️ **当前重点**:稳定化、部署准备和课题组试用增强，详细路线见 `docs/backend-roadmap.md`;
- ⏸️ **F11~F21 暂缓**:除非触发条件满足且经人工确认，否则不进入开发。

## 铁律(违反即"跑偏",禁止)

1. **SDD + TDD**:没有规格不写代码;没有测试不提交(standards §0);
2. **契约先行**:任何接口变更先改 `docs/api-contract.md`;
3. **不推翻已定决策**:Go/Gin/GORM/模块化单体/MVC/统一内容模型等(清单见 `docs/product-context.md` §4);
4. **不做灵感库功能**:F11~F21 暂不实现(清单见 product-context §2 灵感库),除非触发条件满足且经人工确认;
5. **AI 产物红线**:不改 `schema.sql`/`api-contract.md`/`standards.md` 而不经明确 review;不引入新依赖不说明理由;不跳过测试;不绕过分层(standards §7.3);
6. **权限判断只走 `can(user, action, target)`**,禁止散落硬编码(PRD §3.3)。

## 开发流程(每个功能固定循环)

```
Spec(规格,评审过) → Contract(契约先行) → RED(先写测试,看到失败)
→ GREEN(最小实现) → REFACTOR(保持全绿) → make check → 提交
```

AI 生成代码的完整工作流见 `docs/standards.md` §7(给 AI 的输入必须含规格路径、契约、相关模块,并汇报测试结果)。

## 常用命令

```bash
make up        # 启动 Postgres+Redis 容器
make down      # 停止容器
make run       # 启动后端(:8080)
make build     # 编译
make test      # go test ./... -cover
make lint      # golangci-lint
make check     # 提交前全量门禁(vet+fmt+test+lint+build),必须全绿
```

## 环境注意

- **Docker daemon 可能未启动**:`make up` 前先启动 Docker Desktop;
- **受限沙箱**:`scripts/check.sh` 已自动把 Go/lint 缓存指到工作区(`.gocache` 等),无需手工处理;若换环境(无 `.gocache` 目录),此适配自动不生效;
- **数据库**:开发期仍使用 GORM AutoMigrate;正式部署前切换 goose,基线 = `docs/schema.sql`;
- **git 状态**:尚未 init(若已 init 则跳过),首次提交前 `git init`。

## 提交规范

Conventional Commits:`feat|fix|docs|test|refactor|chore(<scope>): 描述`(standards §6)。
