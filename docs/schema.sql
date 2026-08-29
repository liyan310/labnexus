-- ============================================================================
-- LabNexus 数据库 Schema 定稿
-- 依据:PRD v0.4 §5.2(数据模型)
-- 阶段标注:阶段 1 = 社区 MVP(F1~F6);阶段 2 = 资源库+任务(F7~F9);暂缓 = 灵感库
-- 说明:开发期由 GORM AutoMigrate 建表,本文件是权威定义(供 review 与部署前 goose 基线迁移)
-- ============================================================================

-- v2 迁移说明(2026-08-25,F7 重写:去 paper 类型):
--   AutoMigrate 只加列不删列,已有开发库需手工执行:
--   ALTER TABLE resources DROP COLUMN IF EXISTS doi;
--   ALTER TABLE resources DROP COLUMN IF EXISTS arxiv_id;
--   ALTER TABLE resources DROP COLUMN IF EXISTS metadata;
--   (description/original_name/mime_type/file_size 由 AutoMigrate 自动新增)


CREATE EXTENSION IF NOT EXISTS pgcrypto; -- gen_random_uuid()

-- ============================ 阶段 1:社区 MVP ============================

-- 用户(role 三枚举,为导师差异化权限预留)
CREATE TABLE users (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    username      VARCHAR(50)  NOT NULL UNIQUE,
    display_name  VARCHAR(50)  NOT NULL,
    password_hash VARCHAR(255) NOT NULL,
    role          VARCHAR(20)  NOT NULL DEFAULT 'student', -- admin / supervisor / student
    avatar_url    VARCHAR(255),
    created_at    TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ  NOT NULL DEFAULT now()
);

-- 个人空间(与用户 1:1)
CREATE TABLE spaces (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID NOT NULL UNIQUE REFERENCES users(id) ON DELETE CASCADE,
    name       VARCHAR(50) NOT NULL DEFAULT '我的空间',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- 目录(树形,个人空间下)
CREATE TABLE folders (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    space_id   UUID NOT NULL REFERENCES spaces(id) ON DELETE CASCADE,
    parent_id  UUID REFERENCES folders(id) ON DELETE CASCADE,
    name       VARCHAR(100) NOT NULL,
    sort_order INT  NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_folders_space ON folders(space_id);

-- 文档(笔记 = 帖子,统一内容模型,可见性切换)
CREATE TABLE documents (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    author_id  UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    space_id   UUID NOT NULL REFERENCES spaces(id) ON DELETE CASCADE,
    folder_id  UUID REFERENCES folders(id) ON DELETE SET NULL,
    title      VARCHAR(200) NOT NULL,
    content    TEXT NOT NULL DEFAULT '',
    visibility VARCHAR(20) NOT NULL DEFAULT 'private', -- private / public
    pinned     BOOLEAN NOT NULL DEFAULT FALSE,
    deleted_at TIMESTAMPTZ,                            -- 软删除
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_documents_author ON documents(author_id);
-- 信息流查询:公开 + 未删除,按时间倒序
CREATE INDEX idx_documents_feed ON documents(visibility, created_at DESC) WHERE deleted_at IS NULL;

-- 评论(一级回复)
CREATE TABLE comments (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    document_id UUID NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
    author_id   UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    content     TEXT NOT NULL,
    reply_to_id UUID REFERENCES comments(id) ON DELETE CASCADE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_comments_document ON comments(document_id);

-- 点赞(emoji 反应,默认 👍;一人一文档一表情一次)
CREATE TABLE reactions (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    document_id UUID NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    emoji       VARCHAR(16) NOT NULL DEFAULT '👍',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (document_id, user_id, emoji)
);

-- 标签(全局共享)
CREATE TABLE tags (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name       VARCHAR(50) NOT NULL UNIQUE,
    color      VARCHAR(20) NOT NULL DEFAULT '#3b82f6',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- 文档-标签(多对多)
CREATE TABLE document_tags (
    document_id UUID NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
    tag_id      UUID NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
    PRIMARY KEY (document_id, tag_id)
);

-- 邀请码(管理员生成,凭码注册)
CREATE TABLE invite_codes (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    code       VARCHAR(32) NOT NULL UNIQUE,
    created_by UUID NOT NULL REFERENCES users(id),
    expires_at TIMESTAMPTZ,
    used_by    UUID REFERENCES users(id),
    used_at    TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ===================== 阶段 2:资源库 + 项目任务 =====================

-- 资源(链接 / 文件)
CREATE TABLE resources (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    type          VARCHAR(20) NOT NULL,  -- link / file
    title         VARCHAR(300) NOT NULL,
    description   TEXT NOT NULL DEFAULT '', -- 资源说明/用途/备注
    url           VARCHAR(500),          -- link 类型:外部链接(仅 http/https)
    file_path     VARCHAR(500),          -- file 类型:本地存储路径
    original_name VARCHAR(255),          -- file 类型:用户上传的原始文件名
    mime_type     VARCHAR(100),          -- file 类型:检测到的 MIME
    file_size     BIGINT,                -- file 类型:字节数
    uploader_id   UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_resources_type ON resources(type);
CREATE INDEX idx_resources_uploader ON resources(uploader_id);

-- 资源-标签(多对多)
CREATE TABLE resource_tags (
    resource_id UUID NOT NULL REFERENCES resources(id) ON DELETE CASCADE,
    tag_id      UUID NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
    PRIMARY KEY (resource_id, tag_id)
);

-- 项目
CREATE TABLE projects (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name        VARCHAR(100) NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    owner_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    status      VARCHAR(20) NOT NULL DEFAULT 'active', -- active / done
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- 项目成员
CREATE TABLE project_members (
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role       VARCHAR(20) NOT NULL DEFAULT 'member', -- owner / member
    PRIMARY KEY (project_id, user_id)
);

-- 里程碑
CREATE TABLE milestones (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id   UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name         VARCHAR(100) NOT NULL,
    due_date     DATE,
    completed_at TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_milestones_project ON milestones(project_id);

-- 任务(状态机:todo → in_progress → blocked / done,流转校验在 service 层)
CREATE TABLE tasks (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id   UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    title        VARCHAR(200) NOT NULL,
    description  TEXT NOT NULL DEFAULT '',
    assignee_id  UUID REFERENCES users(id) ON DELETE SET NULL,
    status       VARCHAR(20) NOT NULL DEFAULT 'todo', -- todo / in_progress / blocked / done
    priority     VARCHAR(10) NOT NULL DEFAULT 'medium', -- high / medium / low
    due_date     DATE,
    milestone_id UUID REFERENCES milestones(id) ON DELETE SET NULL,
    deleted_at   TIMESTAMPTZ, -- 软删除
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_tasks_project ON tasks(project_id);
CREATE INDEX idx_tasks_assignee ON tasks(assignee_id);

-- 任务关联(多态:可关联文档或资源;target_id 无外键,由应用层保证完整性)
CREATE TABLE task_links (
    task_id     UUID NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    target_type VARCHAR(20) NOT NULL, -- document / resource
    target_id   UUID NOT NULL,
    PRIMARY KEY (task_id, target_type, target_id)
);

-- ============================ 阶段 3:经费管理(F10) ============================
-- 依据:PRD-经费管理.md v2.0、specs/funds-management.md
-- 核心模型:收入 + 支出 → 维护总金额;金额一律 BIGINT 存"分"

-- 周转批次(一次"发放→回收"流程,名称即标识,如 "2026-08")
CREATE TABLE turnover_batches (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name       VARCHAR(100) NOT NULL,
    status     VARCHAR(20) NOT NULL DEFAULT 'active', -- active / done
    note       TEXT NOT NULL DEFAULT '',
    created_by UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_turnover_batches_created ON turnover_batches(created_at DESC);

-- 参与同学(姓名+学号去重,跨批次历史账单)
CREATE TABLE participants (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name       VARCHAR(50) NOT NULL,
    student_no VARCHAR(50) NOT NULL,
    note       TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (name, student_no)
);

-- 发放明细(每同学一条)
CREATE TABLE turnover_items (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    batch_id       UUID NOT NULL REFERENCES turnover_batches(id) ON DELETE CASCADE,
    participant_id UUID NOT NULL REFERENCES participants(id) ON DELETE RESTRICT,
    date           DATE NOT NULL,               -- 发放日期
    payroll_amount BIGINT NOT NULL DEFAULT 0,   -- 应发(分)
    tax_amount     BIGINT NOT NULL DEFAULT 0,   -- 扣税(分,人工算好只登记)
    tip_amount     BIGINT NOT NULL DEFAULT 0,   -- 辛苦费(分)
    should_return  BIGINT NOT NULL DEFAULT 0,   -- 应交(分,默认=应发−扣税−辛苦费,可覆盖)
    returned       BIGINT NOT NULL DEFAULT 0,   -- 已交(分,由上交记录累加)
    note           TEXT NOT NULL DEFAULT '',
    created_by     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_turnover_items_batch ON turnover_items(batch_id);
CREATE INDEX idx_turnover_items_participant ON turnover_items(participant_id);

-- 上交记录(每次上交一条;补交不限次数)
CREATE TABLE turnover_submissions (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    item_id     UUID NOT NULL REFERENCES turnover_items(id) ON DELETE CASCADE,
    amount      BIGINT NOT NULL,                -- 本次上交(分)
    date        DATE NOT NULL,                  -- 上交日期
    note        TEXT NOT NULL DEFAULT '',
    operator_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_turnover_submissions_item ON turnover_submissions(item_id);

-- 资金账户(v1 单账户,预置一行)
CREATE TABLE accounts (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name       VARCHAR(100) NOT NULL,
    account_no VARCHAR(50) NOT NULL DEFAULT '',
    note       TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- 资金流水(余额 = Σincome − Σexpense,实时计算)
CREATE TABLE transactions (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id   UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    type         VARCHAR(10) NOT NULL,          -- income / expense
    amount       BIGINT NOT NULL,               -- 金额(分,正数)
    category     VARCHAR(30) NOT NULL DEFAULT 'other', -- 收入:turnover/supplement;支出:labor/other
    related_type VARCHAR(20) NOT NULL DEFAULT 'none',  -- turnover_item / none
    related_id   UUID,
    note         TEXT NOT NULL DEFAULT '',
    occurred_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    operator_id  UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_transactions_account ON transactions(account_id, occurred_at DESC);

-- ============================ 暂缓(灵感库) ============================
-- resource_refs(文档引用资源,灵感库 F10):document_id + resource_id
-- weekly_reports(周报,灵感库 F11)
