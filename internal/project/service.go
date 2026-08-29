// Package project 业务层:F9 项目/成员/里程碑/任务。
// 依据规格:docs/specs/f9-project.md;契约:api-contract.md §F9。
package project

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"labnexus/internal/database"
	"labnexus/internal/user"
)

// 哨兵错误(handler 层统一映射 HTTP)
var (
	ErrProjectNotFound    = errors.New("project not found")
	ErrNotMember          = errors.New("not a project member")
	ErrNotOwner           = errors.New("project owner permission required")
	ErrProjectNameEmpty   = errors.New("project name is empty")
	ErrTaskNotFound       = errors.New("task not found")
	ErrTaskTitleEmpty     = errors.New("task title is empty")
	ErrMilestoneNotFound  = errors.New("milestone not found")
	ErrMilestoneNameEmpty = errors.New("milestone name is empty")
	ErrInvalidPriority    = errors.New("invalid priority")
	ErrInvalidTaskStatus  = errors.New("invalid task status")
	ErrInvalidTransition  = errors.New("invalid task status transition")
	ErrInvalidDate        = errors.New("invalid date, expected YYYY-MM-DD")
	ErrAssigneeNotMember  = errors.New("assignee is not a project member")
	ErrMemberExists       = errors.New("member already exists")
	ErrCannotRemoveOwner  = errors.New("cannot remove project owner")
	ErrInvalidLinkType    = errors.New("invalid link target type")
)

// 成员角色
const (
	MemberRoleOwner  = "owner"
	MemberRoleMember = "member"
)

// allowedTransitions 任务状态机(todo → in_progress → blocked|done;done 为终态)
var allowedTransitions = map[string][]string{
	TaskStatusTodo:       {TaskStatusInProgress},
	TaskStatusInProgress: {TaskStatusBlocked, TaskStatusDone},
	TaskStatusBlocked:    {TaskStatusTodo, TaskStatusInProgress},
	TaskStatusDone:       {},
}

// validStatuses / validPriorities
var validStatuses = map[string]bool{
	TaskStatusTodo: true, TaskStatusInProgress: true, TaskStatusBlocked: true, TaskStatusDone: true,
}
var validPriorities = map[string]bool{
	PriorityHigh: true, PriorityMedium: true, PriorityLow: true,
}

// Service 项目业务逻辑
type Service struct {
	repo     Repository
	users    user.Repository
	txRunner database.TxRunner
}

// NewService 构造函数(依赖注入)
func NewService(repo Repository, users user.Repository) *Service {
	return &Service{repo: repo, users: users, txRunner: database.NoopTxRunner()}
}

// WithTxRunner 注入事务运行器。
func (s *Service) WithTxRunner(runner database.TxRunner) *Service {
	s.txRunner = runner
	return s
}

// ---- 视图 ----

// MemberView 成员视图
type MemberView struct {
	User *user.User `json:"user"`
	Role string     `json:"role"`
}

// TaskView 任务视图
type TaskView struct {
	*Task
	Assignee *user.User  `json:"assignee,omitempty"`
	Links    []*TaskLink `json:"links,omitempty"`
}

// ProjectView 项目视图
type ProjectView struct {
	*Project
	Owner      *user.User    `json:"owner"`
	Members    []*MemberView `json:"members"`
	Milestones []*Milestone  `json:"milestones"`
	Tasks      []*TaskView   `json:"tasks"`
}

// ---- 请求 ----

type CreateProjectRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type UpdateProjectRequest struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
	Status      *string `json:"status"`
}

type AddMemberRequest struct {
	UserID string `json:"user_id"`
	Role   string `json:"role"`
}

type CreateMilestoneRequest struct {
	Name    string  `json:"name"`
	DueDate *string `json:"due_date"`
}

type UpdateMilestoneRequest struct {
	Name        *string    `json:"name"`
	DueDate     *string    `json:"due_date"`
	CompletedAt *time.Time `json:"completed_at"`
}

type CreateTaskRequest struct {
	Title          string  `json:"title"`
	Description    string  `json:"description"`
	AssigneeID     *string `json:"assignee_id"`
	Priority       string  `json:"priority"`
	DueDate        *string `json:"due_date"`
	MilestoneID    *string `json:"milestone_id"`
	LinkDocumentID *string `json:"link_document_id"`
	LinkResourceID *string `json:"link_resource_id"`
}

type UpdateTaskRequest struct {
	Title       *string `json:"title"`
	Description *string `json:"description"`
	Priority    *string `json:"priority"`
	DueDate     *string `json:"due_date"`
	AssigneeID  *string `json:"assignee_id"`
	MilestoneID *string `json:"milestone_id"`
}

// TransitionRequest 状态流转请求
type TransitionRequest struct {
	Status string `json:"status"`
}

// ---- 项目 ----

// CreateProject 创建项目(创建者即 owner,自动成为成员;事务)。
func (s *Service) CreateProject(ctx context.Context, userID string, req CreateProjectRequest) (*ProjectView, error) {
	if strings.TrimSpace(req.Name) == "" {
		return nil, ErrProjectNameEmpty
	}
	p := NewProject(req.Name, req.Description, userID)
	err := s.txRunner(ctx, func(tctx context.Context) error {
		if err := s.repo.CreateProject(tctx, p); err != nil {
			return err
		}
		return s.repo.AddMember(tctx, &ProjectMember{
			ProjectID: p.ID, UserID: userID, Role: MemberRoleOwner,
		})
	})
	if err != nil {
		return nil, err
	}
	return s.buildProjectView(ctx, p, false)
}

// ListProjects 本人参与的项目列表。
func (s *Service) ListProjects(ctx context.Context, userID string) ([]*ProjectView, error) {
	list, err := s.repo.ListProjectsByMember(ctx, userID)
	if err != nil {
		return nil, err
	}
	views := make([]*ProjectView, 0, len(list))
	for _, p := range list {
		view, err := s.buildProjectView(ctx, p, false)
		if err != nil {
			return nil, err
		}
		views = append(views, view)
	}
	return views, nil
}

// GetProject 项目详情(成员可见,含全部子资源)。
func (s *Service) GetProject(ctx context.Context, userID, projectID string) (*ProjectView, error) {
	p, err := s.repo.GetProject(ctx, projectID)
	if err != nil {
		return nil, ErrProjectNotFound
	}
	if _, err := s.repo.GetMember(ctx, p.ID, userID); err != nil {
		return nil, ErrNotMember
	}
	return s.buildProjectView(ctx, p, true)
}

// UpdateProject 修改项目(仅 owner)。
func (s *Service) UpdateProject(ctx context.Context, userID, projectID string, req UpdateProjectRequest) (*ProjectView, error) {
	p, err := s.requireOwner(ctx, userID, projectID)
	if err != nil {
		return nil, err
	}
	if req.Name != nil {
		if strings.TrimSpace(*req.Name) == "" {
			return nil, ErrProjectNameEmpty
		}
		p.Name = *req.Name
	}
	if req.Description != nil {
		p.Description = *req.Description
	}
	if req.Status != nil {
		p.Status = *req.Status
	}
	p.UpdatedAt = time.Now()
	if err := s.repo.UpdateProject(ctx, p); err != nil {
		return nil, err
	}
	return s.buildProjectView(ctx, p, true)
}

// ---- 成员 ----

// AddMember 添加成员(仅 owner;目标用户必须存在;重复添加 → 400)。
func (s *Service) AddMember(ctx context.Context, userID, projectID string, req AddMemberRequest) (*MemberView, error) {
	if _, err := s.requireOwner(ctx, userID, projectID); err != nil {
		return nil, err
	}
	target, err := s.users.GetByID(ctx, req.UserID)
	if err != nil {
		return nil, user.ErrNotFound
	}
	if _, err := s.repo.GetMember(ctx, projectID, req.UserID); err == nil {
		return nil, ErrMemberExists
	}
	role := req.Role
	if role == "" {
		role = MemberRoleMember
	}
	if err := s.repo.AddMember(ctx, &ProjectMember{ProjectID: projectID, UserID: req.UserID, Role: role}); err != nil {
		return nil, err
	}
	return &MemberView{User: target, Role: role}, nil
}

// RemoveMember 移除成员(仅 owner;不可移除 owner 自己)。
func (s *Service) RemoveMember(ctx context.Context, userID, projectID, targetUserID string) error {
	if _, err := s.requireOwner(ctx, userID, projectID); err != nil {
		return err
	}
	if targetUserID == userID {
		return ErrCannotRemoveOwner
	}
	if _, err := s.repo.GetMember(ctx, projectID, targetUserID); err != nil {
		return ErrNotMember
	}
	return s.repo.RemoveMember(ctx, projectID, targetUserID)
}

// ---- 里程碑 ----

// CreateMilestone 创建里程碑(仅 owner)。
func (s *Service) CreateMilestone(ctx context.Context, userID, projectID string, req CreateMilestoneRequest) (*Milestone, error) {
	if _, err := s.requireOwner(ctx, userID, projectID); err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.Name) == "" {
		return nil, ErrMilestoneNameEmpty
	}
	if req.DueDate != nil && !validDate(*req.DueDate) {
		return nil, ErrInvalidDate
	}
	m := &Milestone{
		ID: newID(), ProjectID: projectID, Name: req.Name, DueDate: req.DueDate, CreatedAt: time.Now(),
	}
	if err := s.repo.CreateMilestone(ctx, m); err != nil {
		return nil, err
	}
	return m, nil
}

// UpdateMilestone 修改里程碑(仅 owner)。
func (s *Service) UpdateMilestone(ctx context.Context, userID, milestoneID string, req UpdateMilestoneRequest) (*Milestone, error) {
	m, err := s.repo.GetMilestone(ctx, milestoneID)
	if err != nil {
		return nil, ErrMilestoneNotFound
	}
	if _, err := s.requireOwner(ctx, userID, m.ProjectID); err != nil {
		return nil, err
	}
	if req.Name != nil {
		if strings.TrimSpace(*req.Name) == "" {
			return nil, ErrMilestoneNameEmpty
		}
		m.Name = *req.Name
	}
	if req.DueDate != nil {
		if !validDate(*req.DueDate) {
			return nil, ErrInvalidDate
		}
		m.DueDate = req.DueDate
	}
	if req.CompletedAt != nil {
		m.CompletedAt = req.CompletedAt
	}
	if err := s.repo.UpdateMilestone(ctx, m); err != nil {
		return nil, err
	}
	return m, nil
}

// ---- 任务 ----

// ListTasks 任务列表(成员;status/assignee/milestone 筛选)。
func (s *Service) ListTasks(ctx context.Context, userID, projectID string, f ListFilter) ([]*TaskView, error) {
	if _, err := s.requireMember(ctx, userID, projectID); err != nil {
		return nil, err
	}
	list, err := s.repo.ListTasks(ctx, projectID, f)
	if err != nil {
		return nil, err
	}
	return s.buildTaskViews(ctx, list)
}

// CreateTask 创建任务(成员;assignee 须为成员;可关联文档/资源)。
func (s *Service) CreateTask(ctx context.Context, userID, projectID string, req CreateTaskRequest) (*TaskView, error) {
	if _, err := s.requireMember(ctx, userID, projectID); err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.Title) == "" {
		return nil, ErrTaskTitleEmpty
	}
	priority := req.Priority
	if priority == "" {
		priority = PriorityMedium
	}
	if !validPriorities[priority] {
		return nil, ErrInvalidPriority
	}
	if req.DueDate != nil && !validDate(*req.DueDate) {
		return nil, ErrInvalidDate
	}
	if req.AssigneeID != nil {
		if _, err := s.repo.GetMember(ctx, projectID, *req.AssigneeID); err != nil {
			return nil, ErrAssigneeNotMember
		}
	}
	if req.MilestoneID != nil {
		if _, err := s.repo.GetMilestone(ctx, *req.MilestoneID); err != nil {
			return nil, ErrMilestoneNotFound
		}
	}

	task := NewTask(projectID, req.Title, req.Description, req.AssigneeID, priority, req.DueDate, req.MilestoneID)
	err := s.txRunner(ctx, func(tctx context.Context) error {
		if err := s.repo.CreateTask(tctx, task); err != nil {
			return err
		}
		if req.LinkDocumentID != nil {
			if err := s.repo.LinkTask(tctx, task.ID, "document", *req.LinkDocumentID); err != nil {
				return err
			}
		}
		if req.LinkResourceID != nil {
			if err := s.repo.LinkTask(tctx, task.ID, "resource", *req.LinkResourceID); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.buildTaskView(ctx, task)
}

// UpdateTask 修改任务(owner 或 assignee)。
func (s *Service) UpdateTask(ctx context.Context, userID, taskID string, req UpdateTaskRequest) (*TaskView, error) {
	task, err := s.repo.GetTask(ctx, taskID)
	if err != nil {
		return nil, ErrTaskNotFound
	}
	ok, err := s.canManageTask(ctx, userID, task)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrNotOwner
	}

	if req.Title != nil {
		if strings.TrimSpace(*req.Title) == "" {
			return nil, ErrTaskTitleEmpty
		}
		task.Title = *req.Title
	}
	if req.Description != nil {
		task.Description = *req.Description
	}
	if req.Priority != nil {
		if !validPriorities[*req.Priority] {
			return nil, ErrInvalidPriority
		}
		task.Priority = *req.Priority
	}
	if req.DueDate != nil {
		if !validDate(*req.DueDate) {
			return nil, ErrInvalidDate
		}
		task.DueDate = req.DueDate
	}
	if req.AssigneeID != nil {
		if _, err := s.repo.GetMember(ctx, task.ProjectID, *req.AssigneeID); err != nil {
			return nil, ErrAssigneeNotMember
		}
		task.AssigneeID = req.AssigneeID
	}
	if req.MilestoneID != nil {
		if _, err := s.repo.GetMilestone(ctx, *req.MilestoneID); err != nil {
			return nil, ErrMilestoneNotFound
		}
		task.MilestoneID = req.MilestoneID
	}
	task.UpdatedAt = time.Now()
	if err := s.repo.UpdateTask(ctx, task); err != nil {
		return nil, err
	}
	return s.buildTaskView(ctx, task)
}

// TransitionTask 状态流转(owner 或 assignee;状态机校验)。
func (s *Service) TransitionTask(ctx context.Context, userID, taskID, newStatus string) (*TaskView, error) {
	task, err := s.repo.GetTask(ctx, taskID)
	if err != nil {
		return nil, ErrTaskNotFound
	}
	if !validStatuses[newStatus] {
		return nil, ErrInvalidTaskStatus
	}
	ok, err := s.canManageTask(ctx, userID, task)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrNotOwner
	}
	if !contains(allowedTransitions[task.Status], newStatus) {
		return nil, ErrInvalidTransition
	}
	task.Status = newStatus
	task.UpdatedAt = time.Now()
	if err := s.repo.UpdateTask(ctx, task); err != nil {
		return nil, err
	}
	return s.buildTaskView(ctx, task)
}

// DeleteTask 删除任务(仅 owner;软删除)。
func (s *Service) DeleteTask(ctx context.Context, userID, taskID string) error {
	task, err := s.repo.GetTask(ctx, taskID)
	if err != nil {
		return ErrTaskNotFound
	}
	if _, err := s.requireOwner(ctx, userID, task.ProjectID); err != nil {
		return err
	}
	return s.repo.SoftDeleteTask(ctx, task.ID)
}

// ---- 内部 helper ----

// requireOwner 校验用户是项目 owner,返回项目。
func (s *Service) requireOwner(ctx context.Context, userID, projectID string) (*Project, error) {
	p, err := s.repo.GetProject(ctx, projectID)
	if err != nil {
		return nil, ErrProjectNotFound
	}
	m, err := s.repo.GetMember(ctx, projectID, userID)
	if err != nil {
		return nil, ErrNotOwner
	}
	if m.Role != MemberRoleOwner {
		return nil, ErrNotOwner
	}
	return p, nil
}

// requireMember 校验用户是项目成员。
func (s *Service) requireMember(ctx context.Context, userID, projectID string) (*Project, error) {
	p, err := s.repo.GetProject(ctx, projectID)
	if err != nil {
		return nil, ErrProjectNotFound
	}
	if _, err := s.repo.GetMember(ctx, projectID, userID); err != nil {
		return nil, ErrNotMember
	}
	return p, nil
}

// canManageTask owner 或任务指派者。
func (s *Service) canManageTask(ctx context.Context, userID string, task *Task) (bool, error) {
	if task.AssigneeID != nil && *task.AssigneeID == userID {
		return true, nil
	}
	m, err := s.repo.GetMember(ctx, task.ProjectID, userID)
	if err != nil {
		return false, ErrNotMember
	}
	return m.Role == MemberRoleOwner, nil
}

// buildProjectView 组装项目视图(详情含全部子资源,列表仅 owner)。
func (s *Service) buildProjectView(ctx context.Context, p *Project, full bool) (*ProjectView, error) {
	ownerUser, err := s.users.GetByID(ctx, p.OwnerID)
	if err != nil {
		return nil, err
	}
	view := &ProjectView{
		Project:    p,
		Owner:      ownerUser,
		Members:    []*MemberView{},
		Milestones: []*Milestone{},
		Tasks:      []*TaskView{},
	}
	if !full {
		return view, nil
	}
	members, err := s.repo.ListMembers(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	memberViews, err := s.buildMemberViews(ctx, members)
	if err != nil {
		return nil, err
	}
	milestones, err := s.repo.ListMilestones(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	tasks, err := s.repo.ListTasks(ctx, p.ID, ListFilter{})
	if err != nil {
		return nil, err
	}
	taskViews, err := s.buildTaskViews(ctx, tasks)
	if err != nil {
		return nil, err
	}
	view.Members = memberViews
	view.Milestones = milestones
	view.Tasks = taskViews
	return view, nil
}

func (s *Service) buildMemberViews(ctx context.Context, members []*ProjectMember) ([]*MemberView, error) {
	if len(members) == 0 {
		return []*MemberView{}, nil
	}
	ids := make([]string, 0, len(members))
	for _, m := range members {
		ids = append(ids, m.UserID)
	}
	users, err := s.users.GetByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	userByID := make(map[string]*user.User, len(users))
	for _, u := range users {
		userByID[u.ID] = u
	}
	views := make([]*MemberView, 0, len(members))
	for _, m := range members {
		views = append(views, &MemberView{User: userByID[m.UserID], Role: m.Role})
	}
	return views, nil
}

func (s *Service) buildTaskView(ctx context.Context, task *Task) (*TaskView, error) {
	views, err := s.buildTaskViews(ctx, []*Task{task})
	if err != nil {
		return nil, err
	}
	return views[0], nil
}

// buildTaskViews 批量组装任务视图(assignee/links 一次查询,防 N+1)。
func (s *Service) buildTaskViews(ctx context.Context, tasks []*Task) ([]*TaskView, error) {
	if len(tasks) == 0 {
		return []*TaskView{}, nil
	}
	assigneeIDs := make([]string, 0, len(tasks))
	for _, t := range tasks {
		if t.AssigneeID != nil {
			assigneeIDs = append(assigneeIDs, *t.AssigneeID)
		}
	}
	users, err := s.users.GetByIDs(ctx, assigneeIDs)
	if err != nil {
		return nil, err
	}
	userByID := make(map[string]*user.User, len(users))
	for _, u := range users {
		userByID[u.ID] = u
	}
	views := make([]*TaskView, 0, len(tasks))
	for _, t := range tasks {
		links, err := s.repo.ListLinks(ctx, t.ID)
		if err != nil {
			return nil, err
		}
		var assignee *user.User
		if t.AssigneeID != nil {
			assignee = userByID[*t.AssigneeID]
		}
		views = append(views, &TaskView{Task: t, Assignee: assignee, Links: links})
	}
	return views, nil
}

func validDate(s string) bool {
	_, err := time.Parse("2006-01-02", s)
	return err == nil
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func newID() string {
	return uuid.NewString()
}
