//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"labnexus/internal/app"
	"labnexus/internal/config"
	"labnexus/internal/database"
)

// truncateTables 清理的业务表(阶段 1+2+3;顺序无关,无外键约束,加 CASCADE 保险)
var truncateTables = []string{
	"document_tags", "reactions", "comments", "documents",
	"folders", "spaces", "invite_codes", "tags", "users",
	"resource_tags", "resources",
	"task_links", "tasks", "milestones", "project_members", "projects",
	"turnover_submissions", "turnover_items", "participants",
	"turnover_batches", "transactions", "accounts",
}

// setupServer 构建生产装配(与 main 完全一致),清空数据,返回路由。
// 环境未就绪(容器未启动)时跳过该测试。
func setupServer(t *testing.T) *gin.Engine {
	t.Helper()
	cfg := config.Load()
	// 集成测试 CWD 为 test/integration;web 在项目根(绝对路径,http.Dir 拒绝 "..")
	wd, _ := os.Getwd()
	cfg.WebDir = filepath.Join(wd, "..", "..", "web")
	t.Logf("CWD=%s WebDir=%s exists=%v", wd, cfg.WebDir, fileExists(cfg.WebDir))
	gin.SetMode(gin.TestMode)

	db, err := database.New(cfg)
	if err != nil {
		t.Skipf("集成环境未就绪(先 `make up` 启动 Postgres/Redis): %v", err)
	}
	// 先迁移建表(与生产一致),再清数据
	r, err := app.Build(cfg)
	require.NoError(t, err, "app.Build 失败")
	resetData(t, db, cfg)
	closeDB(db)
	return r
}

// resetData 清空业务表与 Redis(每个测试独立数据起点)。
func resetData(t *testing.T, db *gorm.DB, cfg *config.Config) {
	t.Helper()
	for _, tbl := range truncateTables {
		require.NoError(t, db.Exec("TRUNCATE TABLE "+tbl+" CASCADE").Error, "清表 %s", tbl)
	}
	rdb := redis.NewClient(&redis.Options{Addr: cfg.RedisAddr})
	defer rdb.Close()
	require.NoError(t, rdb.FlushDB(context.Background()).Err(), "Redis FLUSHDB")
}

// ---- HTTP 助手 ----

func doJSON(t *testing.T, r *gin.Engine, method, path, body, token string) *httptest.ResponseRecorder {
	t.Helper()
	var reqBody *bytes.Reader
	if body != "" {
		reqBody = bytes.NewReader([]byte(body))
	} else {
		reqBody = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, reqBody)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// doMultipart 文件上传请求(multipart/form-data,file 字段)。
func doMultipart(t *testing.T, r *gin.Engine, path, filename, content, token string) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("file", filename)
	require.NoError(t, err)
	_, _ = fw.Write([]byte(content))
	require.NoError(t, mw.Close())
	req := httptest.NewRequest(http.MethodPost, path, &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// doCookie 带 cookie 的请求(刷新/登出链路)。
func doCookie(t *testing.T, r *gin.Engine, method, path string, cookies []*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// refreshCookie 从响应中提取 ln_refresh cookie。
func refreshCookie(t *testing.T, w *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, c := range w.Result().Cookies() {
		if c.Name == "ln_refresh" {
			return c
		}
	}
	t.Fatal("响应中无 refresh cookie")
	return nil
}

func parseJSON(t *testing.T, w *httptest.ResponseRecorder, out any) {
	t.Helper()
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), out), "响应 JSON 解析失败: %s", w.Body.String())
}

func errorCode(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	parseJSON(t, w, &body)
	return body.Error.Code
}

// ---- 业务助手 ----

// seedInvite 直接向 DB 插入邀请码(管理端尚未实现,集成测试用)。
func seedInvite(t *testing.T, code string) {
	t.Helper()
	db := connectDB(t)
	defer closeDB(db)
	require.NoError(t, db.Exec(
		"INSERT INTO invite_codes (id, code, created_by) VALUES (gen_random_uuid(), ?, '00000000-0000-0000-0000-000000000000')",
		code,
	).Error)
}

func connectDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := database.New(config.Load())
	require.NoError(t, err)
	return db
}

// closeDB 关闭 gorm.DB 底层连接池。
func closeDB(db *gorm.DB) {
	if sqlDB, err := db.DB(); err == nil {
		_ = sqlDB.Close()
	}
}

// registerUser 注册用户并返回 access token(内部自动生成邀请码)。
func registerUser(t *testing.T, r *gin.Engine, username, display string) string {
	t.Helper()
	code := fmt.Sprintf("INV-%s-%d", username, hashSeed(username))
	seedInvite(t, code)
	w := doJSON(t, r, http.MethodPost, "/api/auth/register",
		fmt.Sprintf(`{"invite_code":%q,"username":%q,"display_name":%q,"password":"password123"}`, code, username, display), "")
	require.Equal(t, http.StatusCreated, w.Code, "注册失败: %s", w.Body.String())
	var body struct {
		AccessToken string `json:"access_token"`
	}
	parseJSON(t, w, &body)
	require.NotEmpty(t, body.AccessToken)
	return body.AccessToken
}

// loginToken 登录并返回 access token。
func loginToken(t *testing.T, r *gin.Engine, username string) string {
	t.Helper()
	w := doJSON(t, r, http.MethodPost, "/api/auth/login",
		fmt.Sprintf(`{"username":%q,"password":"password123"}`, username), "")
	require.Equal(t, http.StatusOK, w.Code, "登录失败: %s", w.Body.String())
	var body struct {
		AccessToken string `json:"access_token"`
	}
	parseJSON(t, w, &body)
	return body.AccessToken
}

func createTag(t *testing.T, r *gin.Engine, token, name string) string {
	t.Helper()
	w := doJSON(t, r, http.MethodPost, "/api/tags",
		fmt.Sprintf(`{"name":%q}`, name), token)
	require.Equal(t, http.StatusCreated, w.Code, "建标签失败: %s", w.Body.String())
	var body struct {
		Tag struct {
			ID string `json:"id"`
		} `json:"tag"`
	}
	parseJSON(t, w, &body)
	return body.Tag.ID
}

// createFolder 创建目录,返回目录 ID。
func createFolder(t *testing.T, r *gin.Engine, token, name string, parentID *string) string {
	t.Helper()
	parent := "null"
	if parentID != nil {
		parent = fmt.Sprintf("%q", *parentID)
	}
	w := doJSON(t, r, http.MethodPost, "/api/me/folders",
		fmt.Sprintf(`{"name":%q,"parent_id":%s}`, name, parent), token)
	require.Equal(t, http.StatusCreated, w.Code, "建目录失败: %s", w.Body.String())
	var body struct {
		Folder struct {
			ID string `json:"id"`
		} `json:"folder"`
	}
	parseJSON(t, w, &body)
	return body.Folder.ID
}

// createDoc 创建文档,返回文档 ID。
func createDoc(t *testing.T, r *gin.Engine, token, title, visibility string) string {
	t.Helper()
	w := doJSON(t, r, http.MethodPost, "/api/me/documents",
		fmt.Sprintf(`{"title":%q,"content":"content-of-%s","visibility":%q}`, title, title, visibility), token)
	require.Equal(t, http.StatusCreated, w.Code, "建文档失败: %s", w.Body.String())
	var body struct {
		Document struct {
			ID string `json:"id"`
		} `json:"document"`
	}
	parseJSON(t, w, &body)
	return body.Document.ID
}

// hashSeed 简易确定性哈希(生成稳定邀请码后缀)。
func hashSeed(s string) int {
	h := 0
	for _, c := range s {
		h = h*31 + int(c)
	}
	if h < 0 {
		h = -h
	}
	return h % 100000
}

// assertError 断言错误响应格式与状态码(契约 §通用约定)。
func assertError(t *testing.T, w *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	assert.Equal(t, status, w.Code, "状态码不符: %s", w.Body.String())
	assert.Equal(t, code, errorCode(t, w), "错误码不符: %s", w.Body.String())
}

func fileExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}

// newReq 构造带 body 的 HTTP 请求(用于 multipart 等非 JSON 场景)。
func newReq(t *testing.T, method, path string, body *bytes.Buffer) *http.Request {
	t.Helper()
	req, err := http.NewRequest(method, path, body)
	require.NoError(t, err)
	return req
}

// doReq 执行请求并返回响应。
func doReq(t *testing.T, r *gin.Engine, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}
