# LabNexus API 契约 v0.1

> 前后端分离的地基:**改接口先改本文档**。前端(阶段 1 纯 HTML → 正式 React)按此契约开发,不因前端框架切换而返工。
> 依据:PRD v0.4 功能清单 F1~F9。

## 通用约定

- Base URL:`/api`;JSON 请求/响应;UTF-8
- 认证:JWT,`Authorization: Bearer <access_token>`;refresh 走 httpOnly cookie `ln_refresh`
- 权限标注:🔓 无需登录 / 🔐 需登录 / 👑 管理员 / ✍️ 作者本人 / 🧑💼 项目负责人
- 分页:统一 `?page=1&page_size=20`,响应 `{..., "pagination": {"page":1,"page_size":20,"total":N}}`
- 错误格式:统一 `{"error": {"code": "AUTH_REQUIRED", "message": "..."}}`
- 常用错误码:`AUTH_REQUIRED` / `FORBIDDEN` / `NOT_FOUND` / `VALIDATION` / `CONFLICT` / `INTERNAL`

## 阶段 1:F1~F6(社区 MVP)

### F1 账号系统

| 方法 | 路径 | 权限 | 请求体 | 响应 |
|---|---|---|---|---|
| POST | `/auth/register` | 🔓 | `{invite_code, username, display_name, password}` | `201 {access_token, user}`(注册即登录,refresh 写 cookie) |
| POST | `/auth/login` | 🔓 | `{username, password}` | `{access_token, user}`(refresh 写 cookie) |
| POST | `/auth/refresh` | 🔓 | —(读 cookie) | `{access_token}`(刷新并轮换 refresh) |
| POST | `/auth/logout` | 🔐 | — | `204`(撤销 refresh,Redis 删除) |
| GET | `/me` | 🔐 | — | `{user}` |
| PATCH | `/me` | 🔐 | `{display_name?, password?}` | `{user}` |

`user` 结构:`{id, username, display_name, role, avatar_url, created_at}`

### F2 个人空间与目录

| 方法 | 路径 | 权限 | 请求体 | 响应 |
|---|---|---|---|---|
| GET | `/me/space` | 🔐 | — | `{space, folders: []}`(树形) |
| POST | `/me/folders` | 🔐 | `{name, parent_id?}` | `201 {folder}` |
| PATCH | `/me/folders/:id` | 🔐 | `{name?, sort_order?}` | `{folder}` |
| DELETE | `/me/folders/:id` | 🔐 | — | `204`(空目录才可删) |

`folder` 结构:`{id, name, parent_id, sort_order, children: []}`

### F3 文档(笔记 = 帖子)

| 方法 | 路径 | 权限 | 请求体 | 响应 |
|---|---|---|---|---|
| GET | `/me/documents` | 🔐 | query:`folder_id?, visibility?` | `{documents: []}` |
| POST | `/me/documents` | 🔐 | `{folder_id?, title, content, visibility, tag_ids?}` | `201 {document}` |
| GET | `/documents/:id` | 🔐(作者或公开) | — | `{document}` |
| PATCH | `/documents/:id` | ✍️ | `{title?, content?, visibility?, folder_id?, pinned?, tag_ids?}` | `{document}` |
| DELETE | `/documents/:id` | ✍️ | — | `204`(软删除) |

`document` 结构:`{id, title, content, visibility, pinned, folder_id, author:{id,display_name}, tags:[], reactions_count, comments_count, created_at, updated_at}`
可见性切换:`private → public` = 发帖;`public → private` = 撤回(评论/点赞保留)。

### F4 社区信息流

| 方法 | 路径 | 权限 | 请求体 | 响应 |
|---|---|---|---|---|
| GET | `/feed` | 🔐 | query:`sort=latest\|hot, page, page_size` | `{documents: [], pagination}` |
| POST | `/documents/:id/reactions` | 🔐 | `{emoji?}`(默认👍) | `204`(toggle:已有则取消) |
| GET | `/documents/:id/comments` | 🔐 | — | `{comments: []}` |
| POST | `/documents/:id/comments` | 🔐 | `{content, reply_to_id?}` | `201 {comment}` |
| DELETE | `/comments/:id` | ✍️ | — | `204` |

`comment` 结构:`{id, content, reply_to_id, author:{id,display_name}, created_at}`

### F5 标签

| 方法 | 路径 | 权限 | 请求体 | 响应 |
|---|---|---|---|---|
| GET | `/tags` | 🔐 | — | `{tags: []}` |
| POST | `/tags` | 🔐 | `{name, color?}` | `201 {tag}` |
| GET | `/tags/:id/contents` | 🔐 | — | `{documents: [], resources: []}` |

`tag` 结构:`{id, name, color}`

### F6 搜索

| 方法 | 路径 | 权限 | 请求体 | 响应 |
|---|---|---|---|---|
| GET | `/search` | 🔐 | query:`q, type?(document\|resource\|task)` | `{documents: [], resources: [], tasks: []}` |

MVP 为数据库 LIKE;全文搜索升级见灵感库 F14。

## 阶段 2:F7~F9(资源库 + 项目任务)

### F7 资源库

资源类型:仅 `link` / `file`。论文以 PDF 形式作为 `file` 上传(`paper` 类型与 DOI/arXiv 抓取已废弃,见规格 f8-paper-meta.md,重做后再引入)。

| 方法 | 路径 | 权限 | 请求体 | 响应 |
|---|---|---|---|---|
| GET | `/resources` | 🔐 | query:`type?(link\|file), tag_id?, keyword?, page, page_size` | `{resources: [], pagination}` |
| POST | `/resources` | 🔐 | `{type: link, title, url, description?, tag_ids?}` | `201 {resource}` |
| POST | `/resources/upload` | 🔐 | multipart:`file, title?, description?, tag_ids?` | `201 {resource}` |
| GET | `/resources/:id` | 🔐 | — | `{resource}` |
| GET | `/resources/:id/download` | 🔐 | — | 文件流(`attachment`,仅 file) |
| GET | `/resources/:id/preview` | 🔐 | — | 文件流(`inline`,仅 file 且支持预览) |
| PATCH | `/resources/:id` | ✍️或👑 | `{title?, description?, tag_ids?}` | `{resource}` |
| DELETE | `/resources/:id` | ✍️或👑 | — | `204` |

`resource` 结构:

```json
{
  "id": "...", "type": "link|file", "title": "...", "description": "...",
  "url": "...",             // 仅 link
  "original_name": "...",   // 仅 file
  "mime_type": "...",       // 仅 file,如 application/pdf
  "file_size": 12345,       // 仅 file,单位 byte
  "preview": {"supported": true, "type": "pdf", "url": "/api/resources/:id/preview"},
  "download_url": "/api/resources/:id/download",
  "uploader": {"id": "...", "display_name": "..."}, "tags": [], 
  "created_at": "...", "updated_at": "..."
}
```

- `url` 校验:仅允许 `http://` / `https://` 且可解析,否则 400 `VALIDATION`;
- 文件上传:扩展名白名单 + 内容 MIME 双校验;大小上限:普通文件 50MB、视频(mp4/webm)100MB,超限 400 `VALIDATION`;
- 预览支持:`pdf` / 图片(`png/jpg/jpeg/webp/gif`) / 文本(`txt/md`) / 视频(`mp4/webm`);Word/PPT/Excel/压缩包仅下载,预览返回 400 `PREVIEW_UNSUPPORTED`;
- 下载/预览仅 `file` 类型;`link` 调用返回 400 `VALIDATION`;任意登录用户可用(资源共享)。

### F9 项目与任务

| 方法 | 路径 | 权限 | 请求体 | 响应 |
|---|---|---|---|---|
| GET | `/projects` | 🔐 | — | `{projects: []}`(含本人成员项目) |
| POST | `/projects` | 🔐 | `{name, description?}` | `201 {project}`(创建者即 owner) |
| GET | `/projects/:id` | 🔐(成员) | — | `{project, members, milestones, tasks}` |
| PATCH | `/projects/:id` | 🧑💼 | `{name?, description?, status?}` | `{project}` |
| POST | `/projects/:id/members` | 🧑💼 | `{user_id, role?}` | `201 {member}` |
| DELETE | `/projects/:id/members/:user_id` | 🧑💼 | — | `204` |
| POST | `/projects/:id/milestones` | 🧑💼 | `{name, due_date?}` | `201 {milestone}` |
| PATCH | `/milestones/:id` | 🧑💼 | `{name?, due_date?, completed_at?}` | `{milestone}` |
| GET | `/projects/:id/tasks` | 🔐(成员) | query:`status?, assignee_id?, milestone_id?` | `{tasks: []}` |
| POST | `/projects/:id/tasks` | 🔐(成员) | `{title, description?, assignee_id?, priority?, due_date?, milestone_id?, link_document_id?, link_resource_id?}` | `201 {task}` |
| PATCH | `/tasks/:id` | 🔐(负责人/指派者) | `{title?, description?, priority?, due_date?, assignee_id?, milestone_id?}` | `{task}` |
| POST | `/tasks/:id/transition` | 🔐(负责人/指派者) | `{status}` | `{task}` |
| DELETE | `/tasks/:id` | 🔐(负责人) | — | `204`(软删除) |

- `project` 结构:`{id, name, description, status, owner:{id,display_name}, created_at}`
- `task` 结构:`{id, title, description, status, priority, due_date, milestone_id, assignee:{id,display_name}, created_at, updated_at}`
- **状态机**(service 层校验合法迁移):`todo → in_progress → blocked → todo`(受阻后可回进行中)、`in_progress → done`;done 为终态;其余迁移返回 `VALIDATION` 错误。

## 阶段 3:经费管理(F10,仅 admin/supervisor)

> 权限标注:💰 = 仅 admin(经费负责人)/ supervisor(导师)可访问;student 一律 403。
> 核心模型:**收入 + 支出 → 维护总金额**。金额一律以"分"传输/存储,展示转元。
> 依据:PRD-经费管理.md v2.0、specs/funds-management.md。

### 批次

| 方法 | 路径 | 权限 | 请求体 | 响应 |
|---|---|---|---|---|
| POST | `/finance/batches` | 💰 | `{name, note?}` | `201 {batch}` |
| GET | `/finance/batches` | 💰 | — | `{batches: []}`(含汇总) |
| GET | `/finance/batches/:id` | 💰 | — | `{batch, summary, items: []}` |
| DELETE | `/finance/batches/:id` | 💰 | — | `204`(仅 active) |
| POST | `/finance/batches/:id/complete` | 💰 | — | `{batch}`(全部交清才可完成) |

`batch`:`{id, name, status(active/done), note, created_at}`
`summary`:`{item_count, total_payroll, total_should_return, total_returned, total_unreturned}`(分)

### 明细

| 方法 | 路径 | 权限 | 请求体 | 响应 |
|---|---|---|---|---|
| POST | `/finance/batches/:id/items` | 💰 | `{name, student_no, date, payroll_amount, tax_amount?, tip_amount, should_return?, note?}` | `201 {item}` |
| POST | `/finance/batches/:id/items/import-preview` | 💰 | multipart:`file`(.xlsx) | `200 {preview_id, valid_rows: [], error_rows: []}` |
| POST | `/finance/imports/:preview_id/confirm` | 💰 | — | `200 {imported_count, skipped_count}` |

- `item`:`{id, batch_id, participant:{id,name,student_no}, date, payroll_amount, tax_amount, tip_amount, should_return, returned, unreturned, note, status(pending/partial/done)}`(分)
- **应交公式**:`should_return = payroll_amount − tax_amount − tip_amount`(自动计算,可手动覆盖);
- 导入列(表头识别,顺序无关):`姓名 / 学号 / 日期 / 应发 / 扣税 / 辛苦费 / 备注`;
- `unreturned = should_return − returned`;`status` 由 returned 推导:0=未交,`0<returned<should_return`=部分交,`>=`=已交清。

### 上交

| 方法 | 路径 | 权限 | 请求体 | 响应 |
|---|---|---|---|---|
| POST | `/finance/items/:id/submit` | 💰 | `{amount, date, note?}` | `201 {submission}` |

- `submission`:`{id, item_id, amount, date, note, operator:{id,display_name}, created_at}`(分);
- 每次上交:item.returned 累加 + **资金池自动 +amount**(同事务);上交金额 > 未交 → 400。

### 资金池

| 方法 | 路径 | 权限 | 请求体 | 响应 |
|---|---|---|---|---|
| GET | `/finance/ledger` | 💰 | — | `{balance, transactions: []}` |
| POST | `/finance/ledger/income` | 💰 | `{amount, date, note?}` | `201 {transaction}`(导师补充) |
| POST | `/finance/ledger/expense` | 💰 | `{amount, date, note?}` | `201 {transaction}`(资金支出) |

- `transaction`:`{id, type(income/expense), amount, category, note, occurred_at, operator:{id,display_name}, created_at}`(分);
- `balance`(分)= Σincome − Σexpense,实时计算。

### 参与同学库

| 方法 | 路径 | 权限 | 请求体 | 响应 |
|---|---|---|---|---|
| GET | `/finance/participants` | 💰 | — | `{participants: []}`(去重+累计) |
| GET | `/finance/participants/:id/bills` | 💰 | — | `{participant, bills: []}`(跨批次历史账单) |

`participant`:`{id, name, student_no, total_items, total_should_return, total_returned}`
`bill`:`{batch_name, date, payroll_amount, should_return, returned, note}`

## 管理端(阶段 1 起)

| 方法 | 路径 | 权限 | 请求体 | 响应 |
|---|---|---|---|---|
| GET | `/admin/users` | 👑 | — | `{users: []}` |
| PATCH | `/admin/users/:id/role` | 👑 | `{role}` | `{user}` |
| POST | `/admin/invites` | 👑 | `{expires_at?}` | `201 {code, url}` |
| GET | `/admin/invites` | 👑 | — | `{invites: []}` |
| DELETE | `/admin/invites/:id` | 👑 | — | `204` |

## 待定/备注

- 排序"热门"= 点赞数(阶段 1 按点赞数倒序,无需额外字段);
- 任务关联文档/资源阶段 2 通过 `link_document_id` / `link_resource_id` 一次性写入 `task_links`;
- 权限语义的最终解释权在 `internal/*/service.go` 与 `can()` 接口(PRD §3.3)。
