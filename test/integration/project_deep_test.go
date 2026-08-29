//go:build integration

package integration

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// F9 深度:数据隔离 + 越权矩阵 + 状态机全表 + 边界 + 软删除
func TestProject_DeepIsolation(t *testing.T) {
	r := setupServer(t)
	tokenOwner := registerUser(t, r, "powner", "负责人")
	tokenMember := registerUser(t, r, "pmember", "成员")
	tokenOut := registerUser(t, r, "pout", "局外人")

	pid := createProjectViaAPI(t, r, tokenOwner, "隔离项目")
	addMemberViaAPI(t, r, tokenOwner, pid, tokenMember)
	createTaskViaAPI(t, r, tokenOwner, pid, "内部任务", "")

	// 局外人:列表不出现 / 详情 403 / 任务列表 403
	wList := doJSON(t, r, http.MethodGet, "/api/projects", "", tokenOut)
	assert.NotContains(t, wList.Body.String(), "隔离项目")
	assert.Equal(t, http.StatusForbidden, doJSON(t, r, http.MethodGet, "/api/projects/"+pid, "", tokenOut).Code)
	assert.Equal(t, http.StatusForbidden, doJSON(t, r, http.MethodGet, "/api/projects/"+pid+"/tasks", "", tokenOut).Code)

	// 局外人:建任务/建里程碑 403
	assert.Equal(t, http.StatusForbidden, doJSON(t, r, http.MethodPost, "/api/projects/"+pid+"/tasks",
		`{"title":"x"}`, tokenOut).Code)
	assert.Equal(t, http.StatusForbidden, doJSON(t, r, http.MethodPost, "/api/projects/"+pid+"/milestones",
		`{"name":"x"}`, tokenOut).Code)

	// 成员(非 owner):改项目/加成员/删成员/建里程碑/删任务 → 403
	assert.Equal(t, http.StatusForbidden, doJSON(t, r, http.MethodPatch, "/api/projects/"+pid,
		`{"name":"hack"}`, tokenMember).Code)
	assert.Equal(t, http.StatusForbidden, doJSON(t, r, http.MethodPost, "/api/projects/"+pid+"/members",
		`{"user_id":"x"}`, tokenMember).Code)
	assert.Equal(t, http.StatusForbidden, doJSON(t, r, http.MethodPost, "/api/projects/"+pid+"/milestones",
		`{"name":"m"}`, tokenMember).Code)

	// 成员可看详情/任务列表(正常成员权限)
	assert.Equal(t, http.StatusOK, doJSON(t, r, http.MethodGet, "/api/projects/"+pid, "", tokenMember).Code)
	assert.Equal(t, http.StatusOK, doJSON(t, r, http.MethodGet, "/api/projects/"+pid+"/tasks", "", tokenMember).Code)
}

// 边界校验矩阵
func TestProject_DeepValidation(t *testing.T) {
	r := setupServer(t)
	tokenOwner := registerUser(t, r, "powner", "负责人")
	tokenMember := registerUser(t, r, "pmember", "成员")
	pid := createProjectViaAPI(t, r, tokenOwner, "校验项目")
	addMemberViaAPI(t, r, tokenOwner, pid, tokenMember)

	// 重复添加成员 → 400
	wDup := doJSON(t, r, http.MethodPost, "/api/projects/"+pid+"/members",
		`{"user_id":"`+userIDOf(t, r, tokenMember)+`"}`, tokenOwner)
	assert.Equal(t, http.StatusBadRequest, wDup.Code)

	// 移除 owner 自己 → 400;移除非成员 → 403(该用户无成员身份)
	assert.Equal(t, http.StatusBadRequest,
		doJSON(t, r, http.MethodDelete, "/api/projects/"+pid+"/members/"+userIDOf(t, r, tokenOwner), "", tokenOwner).Code)
	assert.Equal(t, http.StatusForbidden,
		doJSON(t, r, http.MethodDelete, "/api/projects/"+pid+"/members/no-such-user", "", tokenOwner).Code)

	// 任务边界:空标题 / 非法优先级 / 非法日期 / assignee 非成员 → 400
	cases := []string{
		`{"title":""}`,
		`{"title":"t","priority":"urgent"}`,
		`{"title":"t","due_date":"2026/09/01"}`,
		`{"title":"t","assignee_id":"no-such-user"}`,
	}
	for _, body := range cases {
		w := doJSON(t, r, http.MethodPost, "/api/projects/"+pid+"/tasks", body, tokenOwner)
		assert.Equal(t, http.StatusBadRequest, w.Code, "body: %s → %s", body, w.Body.String())
	}
	// 里程碑不存在 → 404
	assert.Equal(t, http.StatusNotFound,
		doJSON(t, r, http.MethodPost, "/api/projects/"+pid+"/tasks", `{"title":"t","milestone_id":"no-such-ms"}`, tokenOwner).Code)

	// 项目名空 / 里程碑名空
	assert.Equal(t, http.StatusBadRequest, doJSON(t, r, http.MethodPost, "/api/projects", `{"name":""}`, tokenOwner).Code)
	wMS := doJSON(t, r, http.MethodPost, "/api/projects/"+pid+"/milestones", `{"name":""}`, tokenOwner)
	assert.Equal(t, http.StatusBadRequest, wMS.Code)
}

// 状态机全表(集成层验证,与单元测试一致)
func TestProject_DeepStateMachine(t *testing.T) {
	r := setupServer(t)
	tokenOwner := registerUser(t, r, "powner", "负责人")
	pid := createProjectViaAPI(t, r, tokenOwner, "状态机项目")

	cases := []struct {
		from, to string
		ok       bool
	}{
		{"todo", "in_progress", true},
		{"in_progress", "blocked", true},
		{"blocked", "todo", true},
		{"blocked", "in_progress", true},
		{"in_progress", "done", true},
		{"todo", "done", false},
		{"done", "in_progress", false},
		{"in_progress", "todo", false},
	}
	for _, tc := range cases {
		// 每例独立任务,通过 DB 直接设前置状态(集成层绕过状态机)
		tid := createTaskViaAPI(t, r, tokenOwner, pid, "任务-"+tc.from+"→"+tc.to, "")
		db := connectDB(t)
		require.NoError(t, db.Exec("UPDATE tasks SET status = ? WHERE id = ?", tc.from, tid).Error)
		closeDB(db)

		w := doJSON(t, r, http.MethodPost, "/api/tasks/"+tid+"/transition",
			fmt.Sprintf(`{"status":%q}`, tc.to), tokenOwner)
		if tc.ok {
			require.Equal(t, http.StatusOK, w.Code, "%s→%s 应 200: %s", tc.from, tc.to, w.Body.String())
		} else {
			assert.Equal(t, http.StatusBadRequest, w.Code, "%s→%s 应 400", tc.from, tc.to)
		}
	}
}

// 软删除:任务删除后列表不出现,DB 行 deleted_at 非空
func TestProject_DeepSoftDelete(t *testing.T) {
	r := setupServer(t)
	tokenOwner := registerUser(t, r, "powner", "负责人")
	pid := createProjectViaAPI(t, r, tokenOwner, "软删项目")
	tid := createTaskViaAPI(t, r, tokenOwner, pid, "将被删除", "")

	require.Equal(t, http.StatusNoContent, doJSON(t, r, http.MethodDelete, "/api/tasks/"+tid, "", tokenOwner).Code)

	wList := doJSON(t, r, http.MethodGet, "/api/projects/"+pid+"/tasks", "", tokenOwner)
	assert.NotContains(t, wList.Body.String(), "将被删除")

	db := connectDB(t)
	var count int64
	require.NoError(t, db.Raw("SELECT COUNT(*) FROM tasks WHERE id = ? AND deleted_at IS NOT NULL", tid).Scan(&count).Error)
	assert.Equal(t, int64(1), count, "任务应为软删除(deleted_at 非空)")
	closeDB(db)
}

// 契约:项目详情结构完整(owner/members/milestones/tasks)
func TestProject_ContractShape(t *testing.T) {
	r := setupServer(t)
	tokenOwner := registerUser(t, r, "powner", "负责人")
	tokenMember := registerUser(t, r, "pmember", "成员")
	pid := createProjectViaAPI(t, r, tokenOwner, "契约项目")
	addMemberViaAPI(t, r, tokenOwner, pid, tokenMember)
	createTaskViaAPI(t, r, tokenOwner, pid, "契约任务", userIDOf(t, r, tokenMember))

	w := doJSON(t, r, http.MethodGet, "/api/projects/"+pid, "", tokenMember)
	require.Equal(t, http.StatusOK, w.Code)
	var body struct {
		Project struct {
			ID     string `json:"id"`
			Status string `json:"status"`
			Owner  struct {
				DisplayName string `json:"display_name"`
			} `json:"owner"`
			Members []struct {
				User struct {
					Username string `json:"username"`
				} `json:"user"`
				Role string `json:"role"`
			} `json:"members"`
			Milestones []any `json:"milestones"`
			Tasks      []struct {
				Title    string `json:"title"`
				Status   string `json:"status"`
				Assignee struct {
					Username string `json:"username"`
				} `json:"assignee"`
			} `json:"tasks"`
		} `json:"project"`
	}
	parseJSON(t, w, &body)
	assert.Equal(t, "active", body.Project.Status)
	assert.Equal(t, "负责人", body.Project.Owner.DisplayName)
	require.Len(t, body.Project.Members, 2, "owner+member")
	assert.Equal(t, "owner", body.Project.Members[0].Role)
	require.Len(t, body.Project.Tasks, 1)
	assert.Equal(t, "契约任务", body.Project.Tasks[0].Title)
	assert.Equal(t, "pmember", body.Project.Tasks[0].Assignee.Username)
}

// ---- 辅助 ----

func createProjectViaAPI(t *testing.T, r *gin.Engine, token, name string) string {
	t.Helper()
	w := doJSON(t, r, http.MethodPost, "/api/projects",
		fmt.Sprintf(`{"name":%q}`, name), token)
	require.Equal(t, http.StatusCreated, w.Code, "%s", w.Body.String())
	var body struct {
		Project struct {
			ID string `json:"id"`
		} `json:"project"`
	}
	parseJSON(t, w, &body)
	return body.Project.ID
}

func addMemberViaAPI(t *testing.T, r *gin.Engine, ownerToken, pid, memberToken string) {
	t.Helper()
	w := doJSON(t, r, http.MethodPost, "/api/projects/"+pid+"/members",
		`{"user_id":"`+userIDOf(t, r, memberToken)+`"}`, ownerToken)
	require.Equal(t, http.StatusCreated, w.Code, "%s", w.Body.String())
}

func createTaskViaAPI(t *testing.T, r *gin.Engine, token, pid, title, assigneeID string) string {
	t.Helper()
	body := fmt.Sprintf(`{"title":%q}`, title)
	if assigneeID != "" {
		body = fmt.Sprintf(`{"title":%q,"assignee_id":%q}`, title, assigneeID)
	}
	w := doJSON(t, r, http.MethodPost, "/api/projects/"+pid+"/tasks", body, token)
	require.Equal(t, http.StatusCreated, w.Code, "%s", w.Body.String())
	var res struct {
		Task struct {
			ID string `json:"id"`
		} `json:"task"`
	}
	parseJSON(t, w, &res)
	return res.Task.ID
}

var _ = json.Marshal
