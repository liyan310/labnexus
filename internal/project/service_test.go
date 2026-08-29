package project_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"labnexus/internal/project"
	"labnexus/internal/user"
)

// ---- 内存替身 ----

type memUserRepo struct {
	byID map[string]*user.User
}

func newMemUsers() *memUserRepo { return &memUserRepo{byID: map[string]*user.User{}} }

func (r *memUserRepo) seed(id string) *user.User {
	u := &user.User{ID: id, Username: id, DisplayName: id, Role: "student"}
	r.byID[id] = u
	return u
}

func (r *memUserRepo) GetByID(_ context.Context, id string) (*user.User, error) {
	u, ok := r.byID[id]
	if !ok {
		return nil, user.ErrNotFound
	}
	return u, nil
}

func (r *memUserRepo) GetByIDs(_ context.Context, ids []string) ([]*user.User, error) {
	var out []*user.User
	for _, id := range ids {
		if u, ok := r.byID[id]; ok {
			out = append(out, u)
		}
	}
	return out, nil
}

func (r *memUserRepo) Create(_ context.Context, u *user.User) error { r.byID[u.ID] = u; return nil }
func (r *memUserRepo) GetByUsername(_ context.Context, _ string) (*user.User, error) {
	return nil, user.ErrNotFound
}
func (r *memUserRepo) Update(_ context.Context, u *user.User) error { r.byID[u.ID] = u; return nil }

type memProjectRepo struct {
	projects   map[string]*project.Project
	members    map[string]*project.ProjectMember // key: projectID|userID
	milestones map[string]*project.Milestone
	tasks      map[string]*project.Task
	links      map[string][]*project.TaskLink // taskID -> links
}

func newMemRepo() *memProjectRepo {
	return &memProjectRepo{
		projects:   map[string]*project.Project{},
		members:    map[string]*project.ProjectMember{},
		milestones: map[string]*project.Milestone{},
		tasks:      map[string]*project.Task{},
		links:      map[string][]*project.TaskLink{},
	}
}

func memberKey(projectID, userID string) string { return projectID + "|" + userID }

func (r *memProjectRepo) CreateProject(_ context.Context, p *project.Project) error {
	r.projects[p.ID] = p
	return nil
}

func (r *memProjectRepo) GetProject(_ context.Context, id string) (*project.Project, error) {
	p, ok := r.projects[id]
	if !ok {
		return nil, project.ErrNotFound
	}
	return p, nil
}

func (r *memProjectRepo) UpdateProject(_ context.Context, p *project.Project) error {
	r.projects[p.ID] = p
	return nil
}

func (r *memProjectRepo) ListProjectsByMember(_ context.Context, userID string) ([]*project.Project, error) {
	var out []*project.Project
	for _, m := range r.members {
		if m.UserID == userID {
			if p, ok := r.projects[m.ProjectID]; ok {
				out = append(out, p)
			}
		}
	}
	return out, nil
}

func (r *memProjectRepo) AddMember(_ context.Context, m *project.ProjectMember) error {
	r.members[memberKey(m.ProjectID, m.UserID)] = m
	return nil
}

func (r *memProjectRepo) RemoveMember(_ context.Context, projectID, userID string) error {
	delete(r.members, memberKey(projectID, userID))
	return nil
}

func (r *memProjectRepo) GetMember(_ context.Context, projectID, userID string) (*project.ProjectMember, error) {
	m, ok := r.members[memberKey(projectID, userID)]
	if !ok {
		return nil, project.ErrNotFound
	}
	return m, nil
}

func (r *memProjectRepo) ListMembers(_ context.Context, projectID string) ([]*project.ProjectMember, error) {
	var out []*project.ProjectMember
	for _, m := range r.members {
		if m.ProjectID == projectID {
			out = append(out, m)
		}
	}
	return out, nil
}

func (r *memProjectRepo) CreateMilestone(_ context.Context, m *project.Milestone) error {
	r.milestones[m.ID] = m
	return nil
}

func (r *memProjectRepo) GetMilestone(_ context.Context, id string) (*project.Milestone, error) {
	m, ok := r.milestones[id]
	if !ok {
		return nil, project.ErrNotFound
	}
	return m, nil
}

func (r *memProjectRepo) UpdateMilestone(_ context.Context, m *project.Milestone) error {
	r.milestones[m.ID] = m
	return nil
}

func (r *memProjectRepo) ListMilestones(_ context.Context, projectID string) ([]*project.Milestone, error) {
	var out []*project.Milestone
	for _, m := range r.milestones {
		if m.ProjectID == projectID {
			out = append(out, m)
		}
	}
	return out, nil
}

func (r *memProjectRepo) CreateTask(_ context.Context, t *project.Task) error {
	r.tasks[t.ID] = t
	return nil
}

func (r *memProjectRepo) GetTask(_ context.Context, id string) (*project.Task, error) {
	t, ok := r.tasks[id]
	if !ok {
		return nil, project.ErrNotFound
	}
	return t, nil
}

func (r *memProjectRepo) UpdateTask(_ context.Context, t *project.Task) error {
	r.tasks[t.ID] = t
	return nil
}

func (r *memProjectRepo) SoftDeleteTask(_ context.Context, id string) error {
	delete(r.tasks, id)
	return nil
}

func (r *memProjectRepo) ListTasks(_ context.Context, projectID string, f project.ListFilter) ([]*project.Task, error) {
	var out []*project.Task
	for _, t := range r.tasks {
		if t.ProjectID != projectID {
			continue
		}
		if f.Status != "" && t.Status != f.Status {
			continue
		}
		if f.AssigneeID != "" && (t.AssigneeID == nil || *t.AssigneeID != f.AssigneeID) {
			continue
		}
		if f.MilestoneID != "" && (t.MilestoneID == nil || *t.MilestoneID != f.MilestoneID) {
			continue
		}
		out = append(out, t)
	}
	return out, nil
}

func (r *memProjectRepo) SearchTasks(_ context.Context, keyword string, limit int) ([]*project.Task, error) {
	var out []*project.Task
	for _, t := range r.tasks {
		if strings.Contains(strings.ToLower(t.Title), strings.ToLower(keyword)) {
			out = append(out, t)
		}
	}
	return out, nil
}

func (r *memProjectRepo) LinkTask(_ context.Context, taskID, targetType, targetID string) error {
	r.links[taskID] = append(r.links[taskID], &project.TaskLink{TaskID: taskID, TargetType: targetType, TargetID: targetID})
	return nil
}

func (r *memProjectRepo) ListLinks(_ context.Context, taskID string) ([]*project.TaskLink, error) {
	return r.links[taskID], nil
}

// ---- 夹具 ----

type fixture struct {
	svc   *project.Service
	repo  *memProjectRepo
	users *memUserRepo
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{repo: newMemRepo(), users: newMemUsers()}
	f.svc = project.NewService(f.repo, f.users)
	return f
}

const (
	owner  = "owner-1"
	member = "member-1"
	other  = "other-1"
)

// seedProject 创建项目并加入成员
func (f *fixture) seedProject() *project.Project {
	f.users.seed(owner)
	f.users.seed(member)
	f.users.seed(other)
	p, err := f.svc.CreateProject(context.Background(), owner, project.CreateProjectRequest{Name: "论文项目"})
	if err != nil {
		panic(err)
	}
	_, err = f.svc.AddMember(context.Background(), owner, p.ID, project.AddMemberRequest{UserID: member})
	if err != nil {
		panic(err)
	}
	return p.Project
}

func (f *fixture) seedTask(p *project.Project, assignee string) *project.Task {
	req := project.CreateTaskRequest{Title: "任务A", AssigneeID: &assignee, Priority: project.PriorityMedium}
	view, err := f.svc.CreateTask(context.Background(), owner, p.ID, req)
	if err != nil {
		panic(err)
	}
	return view.Task
}

// ---- 项目 ----

func TestCreateProject_OwnerBecomesMember(t *testing.T) {
	f := newFixture(t)
	f.users.seed(owner)
	p, err := f.svc.CreateProject(context.Background(), owner, project.CreateProjectRequest{Name: "论文"})
	require.NoError(t, err)
	assert.Equal(t, owner, p.OwnerID)
	assert.Equal(t, project.ProjectStatusActive, p.Status)

	// owner 自动成为成员(role=owner)
	m, err := f.repo.GetMember(context.Background(), p.ID, owner)
	require.NoError(t, err)
	assert.Equal(t, "owner", m.Role)
}

func TestCreateProject_EmptyName(t *testing.T) {
	f := newFixture(t)
	f.users.seed(owner)
	_, err := f.svc.CreateProject(context.Background(), owner, project.CreateProjectRequest{Name: "  "})
	assert.ErrorIs(t, err, project.ErrProjectNameEmpty)
}

func TestListProjects_OnlyMine(t *testing.T) {
	f := newFixture(t)
	p := f.seedProject()
	_ = p
	// other 不是成员
	list, err := f.svc.ListProjects(context.Background(), other)
	require.NoError(t, err)
	assert.Empty(t, list)

	list2, _ := f.svc.ListProjects(context.Background(), member)
	require.Len(t, list2, 1)
}

func TestGetProject_NonMemberForbidden(t *testing.T) {
	f := newFixture(t)
	p := f.seedProject()

	_, err := f.svc.GetProject(context.Background(), other, p.ID)
	assert.ErrorIs(t, err, project.ErrNotMember)
	_, err = f.svc.GetProject(context.Background(), member, p.ID)
	require.NoError(t, err)
}

func TestUpdateProject_OwnerOnly(t *testing.T) {
	f := newFixture(t)
	p := f.seedProject()

	// 成员(非 owner)→ 403
	_, err := f.svc.UpdateProject(context.Background(), member, p.ID, project.UpdateProjectRequest{Name: strPtr("hack")})
	assert.ErrorIs(t, err, project.ErrNotOwner)

	// owner → OK
	updated, err := f.svc.UpdateProject(context.Background(), owner, p.ID, project.UpdateProjectRequest{Name: strPtr("新名字")})
	require.NoError(t, err)
	assert.Equal(t, "新名字", updated.Name)
}

// ---- 成员 ----

func TestAddMember(t *testing.T) {
	f := newFixture(t)
	p := f.seedProject()

	// other 添加成员 → 403
	_, err := f.svc.AddMember(context.Background(), other, p.ID, project.AddMemberRequest{UserID: other})
	assert.ErrorIs(t, err, project.ErrNotOwner)

	// owner 添加 other → OK
	f.users.seed(other)
	view, err := f.svc.AddMember(context.Background(), owner, p.ID, project.AddMemberRequest{UserID: other})
	require.NoError(t, err)
	assert.Equal(t, other, view.User.ID)
	assert.Equal(t, "member", view.Role)

	// 重复添加 → 400
	_, err = f.svc.AddMember(context.Background(), owner, p.ID, project.AddMemberRequest{UserID: other})
	assert.ErrorIs(t, err, project.ErrMemberExists)
}

func TestRemoveMember(t *testing.T) {
	f := newFixture(t)
	p := f.seedProject()

	// 移除 owner 自己 → 400
	assert.ErrorIs(t, f.svc.RemoveMember(context.Background(), owner, p.ID, owner), project.ErrCannotRemoveOwner)

	// 移除 member → OK
	require.NoError(t, f.svc.RemoveMember(context.Background(), owner, p.ID, member))
	_, err := f.repo.GetMember(context.Background(), p.ID, member)
	assert.ErrorIs(t, err, project.ErrNotFound)

	// 非 owner 移除 → 403
	assert.ErrorIs(t, f.svc.RemoveMember(context.Background(), other, p.ID, member), project.ErrNotOwner)
}

// ---- 里程碑 ----

func TestMilestone_OwnerOnly(t *testing.T) {
	f := newFixture(t)
	p := f.seedProject()

	// 成员创建 → 403
	_, err := f.svc.CreateMilestone(context.Background(), member, p.ID, project.CreateMilestoneRequest{Name: "m1"})
	assert.ErrorIs(t, err, project.ErrNotOwner)

	// owner 创建
	m, err := f.svc.CreateMilestone(context.Background(), owner, p.ID, project.CreateMilestoneRequest{Name: "m1", DueDate: strPtr("2026-09-01")})
	require.NoError(t, err)
	assert.Equal(t, "2026-09-01", *m.DueDate)

	// 非法日期
	_, err = f.svc.CreateMilestone(context.Background(), owner, p.ID, project.CreateMilestoneRequest{Name: "m2", DueDate: strPtr("2026/09/01")})
	assert.ErrorIs(t, err, project.ErrInvalidDate)

	// 成员可见(GetProject 含 milestones)
	view, err := f.svc.GetProject(context.Background(), member, p.ID)
	require.NoError(t, err)
	require.Len(t, view.Milestones, 1)
}

// ---- 任务 ----

func TestCreateTask_Validation(t *testing.T) {
	f := newFixture(t)
	p := f.seedProject()

	// 空标题
	_, err := f.svc.CreateTask(context.Background(), owner, p.ID, project.CreateTaskRequest{Title: " "})
	assert.ErrorIs(t, err, project.ErrTaskTitleEmpty)

	// 非法优先级
	bad := project.PriorityHigh + "x"
	_, err = f.svc.CreateTask(context.Background(), owner, p.ID, project.CreateTaskRequest{Title: "t", Priority: bad})
	assert.ErrorIs(t, err, project.ErrInvalidPriority)

	// 非法日期
	_, err = f.svc.CreateTask(context.Background(), owner, p.ID, project.CreateTaskRequest{Title: "t", DueDate: strPtr("bad")})
	assert.ErrorIs(t, err, project.ErrInvalidDate)

	// assignee 非成员
	_, err = f.svc.CreateTask(context.Background(), owner, p.ID, project.CreateTaskRequest{Title: "t", AssigneeID: strPtr(other)})
	assert.ErrorIs(t, err, project.ErrAssigneeNotMember)

	// 非成员创建任务 → 403
	_, err = f.svc.CreateTask(context.Background(), other, p.ID, project.CreateTaskRequest{Title: "t"})
	assert.ErrorIs(t, err, project.ErrNotMember)
}

func TestCreateTask_WithLinks(t *testing.T) {
	f := newFixture(t)
	p := f.seedProject()
	docID := "doc-1"

	view, err := f.svc.CreateTask(context.Background(), owner, p.ID, project.CreateTaskRequest{
		Title: "任务A", AssigneeID: strPtr(member), Priority: project.PriorityHigh,
		LinkDocumentID: &docID,
	})
	require.NoError(t, err)
	assert.Equal(t, project.TaskStatusTodo, view.Status)
	require.Len(t, view.Links, 1)
	assert.Equal(t, "document", view.Links[0].TargetType)
	assert.NotNil(t, view.Assignee)
}

func TestTaskList_Filter(t *testing.T) {
	f := newFixture(t)
	p := f.seedProject()
	f.seedTask(p, member)
	req := project.CreateTaskRequest{Title: "B", AssigneeID: strPtr(owner), Priority: project.PriorityLow}
	_, _ = f.svc.CreateTask(context.Background(), owner, p.ID, req)

	// 按 assignee 筛选
	list, err := f.svc.ListTasks(context.Background(), member, p.ID, project.ListFilter{AssigneeID: member})
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, "任务A", list[0].Title)
}

func TestUpdateTask_Permission(t *testing.T) {
	f := newFixture(t)
	p := f.seedProject()
	task := f.seedTask(p, member)

	// other(非成员)→ 403
	_, err := f.svc.UpdateTask(context.Background(), other, task.ID, project.UpdateTaskRequest{Title: strPtr("hack")})
	assert.ErrorIs(t, err, project.ErrNotMember)

	// member(assignee)→ OK
	updated, err := f.svc.UpdateTask(context.Background(), member, task.ID, project.UpdateTaskRequest{Title: strPtr("改了")})
	require.NoError(t, err)
	assert.Equal(t, "改了", updated.Title)
}

// 状态机全迁移表(每例独立设置前置状态)
func TestTransitionTask_StateMachine(t *testing.T) {
	f := newFixture(t)
	p := f.seedProject()

	cases := []struct {
		from, to string
		ok       bool
	}{
		{project.TaskStatusTodo, project.TaskStatusInProgress, true},
		{project.TaskStatusInProgress, project.TaskStatusBlocked, true},
		{project.TaskStatusBlocked, project.TaskStatusTodo, true},
		{project.TaskStatusBlocked, project.TaskStatusInProgress, true},
		{project.TaskStatusInProgress, project.TaskStatusDone, true},
		{project.TaskStatusTodo, project.TaskStatusDone, false},
		{project.TaskStatusDone, project.TaskStatusInProgress, false},
		{project.TaskStatusInProgress, project.TaskStatusTodo, false},
		{project.TaskStatusDone, project.TaskStatusBlocked, false},
	}
	for _, tc := range cases {
		t.Run(tc.from+"_to_"+tc.to, func(t *testing.T) {
			task := f.seedTask(p, member)
			task.Status = tc.from // 直接设置前置状态(内存 repo)
			require.NoError(t, f.repo.UpdateTask(context.Background(), task))

			view, err := f.svc.TransitionTask(context.Background(), owner, task.ID, tc.to)
			if tc.ok {
				require.NoError(t, err, "%s→%s 应合法", tc.from, tc.to)
				assert.Equal(t, tc.to, view.Status)
			} else {
				assert.ErrorIs(t, err, project.ErrInvalidTransition, "%s→%s 应非法", tc.from, tc.to)
			}
		})
	}

	// 非法状态值
	task2 := f.seedTask(p, member)
	_, err := f.svc.TransitionTask(context.Background(), owner, task2.ID, "deleted")
	assert.ErrorIs(t, err, project.ErrInvalidTaskStatus)
}

func TestTransitionTask_Permission(t *testing.T) {
	f := newFixture(t)
	p := f.seedProject()
	task := f.seedTask(p, member)

	// other(非成员)→ 403
	_, err := f.svc.TransitionTask(context.Background(), other, task.ID, project.TaskStatusInProgress)
	assert.ErrorIs(t, err, project.ErrNotMember)
}

func TestDeleteTask_OwnerOnly(t *testing.T) {
	f := newFixture(t)
	p := f.seedProject()
	task := f.seedTask(p, member)

	// member(assignee)删除 → 403
	assert.ErrorIs(t, f.svc.DeleteTask(context.Background(), member, task.ID), project.ErrNotOwner)
	// owner 删除 → OK
	require.NoError(t, f.svc.DeleteTask(context.Background(), owner, task.ID))
	list, _ := f.svc.ListTasks(context.Background(), owner, p.ID, project.ListFilter{})
	assert.Empty(t, list)
}

func strPtr(s string) *string { return &s }
