package finance_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"labnexus/internal/finance"
	"labnexus/internal/token"
	"labnexus/internal/user"
)

func newTestRouter(t *testing.T) (*gin.Engine, *fixture) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	f := newFixture(t)
	h := finance.NewHandler(f.svc)
	r := gin.New()
	h.RegisterRoutes(r, "test-secret")
	return r, f
}

func roleHeader(userID, role string) string {
	access, _ := token.GenerateAccessToken("test-secret", userID, role, 15*time.Minute)
	return "Bearer " + access
}

func finDo(t *testing.T, r *gin.Engine, method, path, body, tokenStr string) *httptest.ResponseRecorder {
	t.Helper()
	var reqBody *bytes.Reader
	if body != "" {
		reqBody = bytes.NewReader([]byte(body))
	} else {
		reqBody = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, reqBody)
	req.Header.Set("Content-Type", "application/json")
	if tokenStr != "" {
		req.Header.Set("Authorization", tokenStr)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func finUploadXLSX(t *testing.T, r *gin.Engine, path string, data []byte, tokenStr string) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("file", "import.xlsx")
	require.NoError(t, err)
	_, _ = fw.Write(data)
	require.NoError(t, mw.Close())
	req := httptest.NewRequest(http.MethodPost, path, &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", tokenStr)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestFinance_RequiresAuthAndRole(t *testing.T) {
	r, f := newTestRouter(t)
	f.seedRoles()

	// 未登录 → 401
	w := finDo(t, r, http.MethodGet, "/api/finance/batches", "", "")
	assert.Equal(t, http.StatusUnauthorized, w.Code)

	// student → 403
	w2 := finDo(t, r, http.MethodGet, "/api/finance/batches", "", roleHeader(studentID, user.RoleStudent))
	assert.Equal(t, http.StatusForbidden, w2.Code)

	// admin → 200
	w3 := finDo(t, r, http.MethodGet, "/api/finance/batches", "", roleHeader(adminID, user.RoleAdmin))
	assert.Equal(t, http.StatusOK, w3.Code)
}

func TestFinance_BatchAndItemFlow(t *testing.T) {
	r, f := newTestRouter(t)
	f.seedRoles()
	adminTok := roleHeader(adminID, user.RoleAdmin)

	// 新建批次
	w := finDo(t, r, http.MethodPost, "/api/finance/batches", `{"name":"2026-08"}`, adminTok)
	require.Equal(t, http.StatusCreated, w.Code, "%s", w.Body.String())
	assert.Contains(t, w.Body.String(), `"status":"active"`)
	var created struct {
		Batch struct {
			ID string `json:"id"`
		} `json:"batch"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &created))
	batchID := created.Batch.ID

	// 加明细(应交自动)
	w2 := finDo(t, r, http.MethodPost, "/api/finance/batches/"+batchID+"/items",
		`{"name":"张同学","student_no":"2023001","date":"2026-08-20","payroll_amount":250000,"tip_amount":10000}`, adminTok)
	require.Equal(t, http.StatusCreated, w2.Code, "%s", w2.Body.String())
	assert.Contains(t, w2.Body.String(), `"should_return":240000`)

	// 上交
	var item struct {
		Item struct {
			ID string `json:"id"`
		} `json:"item"`
	}
	require.NoError(t, json.Unmarshal(w2.Body.Bytes(), &item))
	w3 := finDo(t, r, http.MethodPost, "/api/finance/items/"+item.Item.ID+"/submit",
		`{"amount":240000,"date":"2026-08-21"}`, adminTok)
	require.Equal(t, http.StatusCreated, w3.Code, "%s", w3.Body.String())

	// 完成
	w4 := finDo(t, r, http.MethodPost, "/api/finance/batches/"+batchID+"/complete", "", adminTok)
	require.Equal(t, http.StatusOK, w4.Code)
	assert.Contains(t, w4.Body.String(), `"status":"done"`)

	// done 后不可再加明细 → 400
	w5 := finDo(t, r, http.MethodPost, "/api/finance/batches/"+batchID+"/items",
		`{"name":"李同学","student_no":"2","date":"2026-08-20","payroll_amount":100}`, adminTok)
	assert.Equal(t, http.StatusBadRequest, w5.Code)
}

func TestFinance_ImportTemplateEndpoint(t *testing.T) {
	r, f := newTestRouter(t)
	f.seedRoles()
	adminTok := roleHeader(adminID, user.RoleAdmin)

	w := finDo(t, r, http.MethodGet, "/api/finance/import-template", "", adminTok)
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
		w.Header().Get("Content-Type"))
	assert.Contains(t, w.Header().Get("Content-Disposition"), "attachment")
	assert.NotEmpty(t, w.Body.Bytes(), "模板内容非空")

	// student → 403
	w2 := finDo(t, r, http.MethodGet, "/api/finance/import-template", "", roleHeader(studentID, user.RoleStudent))
	assert.Equal(t, http.StatusForbidden, w2.Code)
}

func TestFinance_ImportEndpoint(t *testing.T) {
	r, f := newTestRouter(t)
	f.seedRoles()
	adminTok := roleHeader(adminID, user.RoleAdmin)

	w := finDo(t, r, http.MethodPost, "/api/finance/batches", `{"name":"2026-08"}`, adminTok)
	var created struct {
		Batch struct {
			ID string `json:"id"`
		} `json:"batch"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &created))
	batchID := created.Batch.ID

	data := makeXLSX(t,
		[]string{"姓名", "学号", "日期", "应发", "扣税", "辛苦费"},
		[][]string{
			{"张同学", "2023001", "2026-08-20", "2500", "0", "100"},
			{"李同学", "2023002", "2026-08-20", "2500", "0", "100"},
		})
	w2 := finUploadXLSX(t, r, "/api/finance/batches/"+batchID+"/items/import-preview", data, adminTok)
	require.Equal(t, http.StatusOK, w2.Code, "%s", w2.Body.String())
	assert.Contains(t, w2.Body.String(), `"valid_count":2`)

	var preview struct {
		PreviewID string `json:"preview_id"`
	}
	require.NoError(t, json.Unmarshal(w2.Body.Bytes(), &preview))
	require.NotEmpty(t, preview.PreviewID)

	w3 := finDo(t, r, http.MethodPost,
		"/api/finance/imports/"+preview.PreviewID+"/confirm?batch_id="+batchID, "", adminTok)
	require.Equal(t, http.StatusOK, w3.Code, "%s", w3.Body.String())
	assert.Contains(t, w3.Body.String(), `"imported_count":2`)
}

func TestFinance_LedgerEndpoint(t *testing.T) {
	r, f := newTestRouter(t)
	f.seedRoles()
	adminTok := roleHeader(adminID, user.RoleAdmin)

	w := finDo(t, r, http.MethodPost, "/api/finance/ledger/income",
		`{"amount":500000,"date":"2026-08-25","note":"导师补充"}`, adminTok)
	require.Equal(t, http.StatusCreated, w.Code)
	w2 := finDo(t, r, http.MethodPost, "/api/finance/ledger/expense",
		`{"amount":150000,"date":"2026-08-26","note":"发劳务"}`, adminTok)
	require.Equal(t, http.StatusCreated, w2.Code)

	w3 := finDo(t, r, http.MethodGet, "/api/finance/ledger", "", adminTok)
	require.Equal(t, http.StatusOK, w3.Code)
	assert.Contains(t, w3.Body.String(), `"balance":350000`)
	assert.Contains(t, w3.Body.String(), `"operator"`)
}

func TestFinance_ParticipantsEndpoint(t *testing.T) {
	r, f := newTestRouter(t)
	f.seedRoles()
	adminTok := roleHeader(adminID, user.RoleAdmin)

	w := finDo(t, r, http.MethodPost, "/api/finance/batches", `{"name":"2026-08"}`, adminTok)
	var created struct {
		Batch struct {
			ID string `json:"id"`
		} `json:"batch"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &created))
	batchID := created.Batch.ID

	finDo(t, r, http.MethodPost, "/api/finance/batches/"+batchID+"/items",
		`{"name":"张同学","student_no":"2023001","date":"2026-08-20","payroll_amount":250000,"tip_amount":10000}`, adminTok)

	w2 := finDo(t, r, http.MethodGet, "/api/finance/participants", "", adminTok)
	require.Equal(t, http.StatusOK, w2.Code)
	assert.Contains(t, w2.Body.String(), "张同学")
	assert.Contains(t, w2.Body.String(), `"total_items":1`)

	var list struct {
		Participants []struct {
			ID string `json:"id"`
		} `json:"participants"`
	}
	require.NoError(t, json.Unmarshal(w2.Body.Bytes(), &list))
	require.NotEmpty(t, list.Participants)
	w3 := finDo(t, r, http.MethodGet, "/api/finance/participants/"+list.Participants[0].ID+"/bills", "", adminTok)
	require.Equal(t, http.StatusOK, w3.Code)
	assert.Contains(t, w3.Body.String(), "2026-08")
}

var _ = fmt.Sprintf
