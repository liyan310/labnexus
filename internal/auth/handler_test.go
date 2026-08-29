package auth_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"labnexus/internal/auth"
)

// ---- 夹具:真实 service + 内存替身 + gin 路由 ----

func newTestRouter(t *testing.T) (*gin.Engine, *memInviteRepo) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	svc, _, invites, _, _ := newTestService(t)
	h := auth.NewHandler(svc)
	r := gin.New()
	h.RegisterRoutes(r, "test-secret")
	return r, invites
}

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

func refreshCookie(t *testing.T, w *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, c := range w.Result().Cookies() {
		if c.Name == auth.RefreshCookieName {
			return c
		}
	}
	t.Fatal("refresh cookie not set")
	return nil
}

func registerAndLogin(t *testing.T, r *gin.Engine, invites *memInviteRepo) (accessToken string, refreshCookieVal string) {
	t.Helper()
	seedInvite(t, invites, "INVITE-123", nil)

	w := doJSON(t, r, http.MethodPost, "/api/auth/register",
		`{"invite_code":"INVITE-123","username":"alice","display_name":"Alice","password":"password123"}`, "")
	require.Equal(t, http.StatusCreated, w.Code)

	var body struct {
		AccessToken string `json:"access_token"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.NotEmpty(t, body.AccessToken)
	return body.AccessToken, refreshCookie(t, w).Value
}

// ---- 注册 ----

func TestHandler_Register_Success(t *testing.T) {
	r, invites := newTestRouter(t)
	seedInvite(t, invites, "INVITE-123", nil)

	w := doJSON(t, r, http.MethodPost, "/api/auth/register",
		`{"invite_code":"INVITE-123","username":"alice","display_name":"Alice","password":"password123"}`, "")
	require.Equal(t, http.StatusCreated, w.Code)

	var body struct {
		AccessToken string `json:"access_token"`
		User        struct {
			ID       string `json:"id"`
			Username string `json:"username"`
			Role     string `json:"role"`
		} `json:"user"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.NotEmpty(t, body.AccessToken)
	assert.Equal(t, "alice", body.User.Username)
	assert.Equal(t, "student", body.User.Role)
	assert.NotEmpty(t, refreshCookie(t, w).Value)
}

func TestHandler_Register_InvalidInvite(t *testing.T) {
	r, _ := newTestRouter(t)
	w := doJSON(t, r, http.MethodPost, "/api/auth/register",
		`{"invite_code":"BAD","username":"alice","display_name":"Alice","password":"password123"}`, "")
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Contains(t, w.Body.String(), "INVALID_INVITE")
}

func TestHandler_Register_WeakPassword(t *testing.T) {
	r, invites := newTestRouter(t)
	seedInvite(t, invites, "INVITE-123", nil)
	w := doJSON(t, r, http.MethodPost, "/api/auth/register",
		`{"invite_code":"INVITE-123","username":"alice","display_name":"Alice","password":"short"}`, "")
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "VALIDATION")
}

func TestHandler_Register_UsernameTaken(t *testing.T) {
	r, invites := newTestRouter(t)
	registerAndLogin(t, r, invites)           // 注册 alice(消耗 INVITE-123)
	seedInvite(t, invites, "INVITE-456", nil) // 新邀请码,测用户名冲突

	w := doJSON(t, r, http.MethodPost, "/api/auth/register",
		`{"invite_code":"INVITE-456","username":"alice","display_name":"Alice2","password":"password123"}`, "")
	assert.Equal(t, http.StatusConflict, w.Code)
	assert.Contains(t, w.Body.String(), "CONFLICT")
}

func TestHandler_Register_BadJSON(t *testing.T) {
	r, _ := newTestRouter(t)
	w := doJSON(t, r, http.MethodPost, "/api/auth/register", `{not-json`, "")
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// ---- 登录 ----

func TestHandler_Login_Success(t *testing.T) {
	r, invites := newTestRouter(t)
	registerAndLogin(t, r, invites)

	w := doJSON(t, r, http.MethodPost, "/api/auth/login",
		`{"username":"alice","password":"password123"}`, "")
	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "access_token")
	assert.NotEmpty(t, refreshCookie(t, w).Value)
}

func TestHandler_Login_WrongPassword(t *testing.T) {
	r, invites := newTestRouter(t)
	registerAndLogin(t, r, invites)

	w := doJSON(t, r, http.MethodPost, "/api/auth/login",
		`{"username":"alice","password":"wrong"}`, "")
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Contains(t, w.Body.String(), "AUTH_FAILED")
}

// ---- 刷新 / 登出 ----

func TestHandler_Refresh_Rotation(t *testing.T) {
	r, invites := newTestRouter(t)
	_, oldRefresh := registerAndLogin(t, r, invites)

	// 带旧 refresh cookie 刷新
	req := httptest.NewRequest(http.MethodPost, "/api/auth/refresh", nil)
	req.AddCookie(&http.Cookie{Name: auth.RefreshCookieName, Value: oldRefresh})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "access_token")
	newRefresh := refreshCookie(t, w).Value
	assert.NotEqual(t, oldRefresh, newRefresh, "refresh token 必须轮换")

	// 旧 refresh 再用 → 401
	req2 := httptest.NewRequest(http.MethodPost, "/api/auth/refresh", nil)
	req2.AddCookie(&http.Cookie{Name: auth.RefreshCookieName, Value: oldRefresh})
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	assert.Equal(t, http.StatusUnauthorized, w2.Code)
}

func TestHandler_Refresh_NoCookie(t *testing.T) {
	r, _ := newTestRouter(t)
	w := doJSON(t, r, http.MethodPost, "/api/auth/refresh", "", "")
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestHandler_Logout_ClearsCookie(t *testing.T) {
	r, invites := newTestRouter(t)
	_, refresh := registerAndLogin(t, r, invites)

	req := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	req.AddCookie(&http.Cookie{Name: auth.RefreshCookieName, Value: refresh})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)
	// cookie 被清除
	var cleared bool
	for _, c := range w.Result().Cookies() {
		if c.Name == auth.RefreshCookieName && c.MaxAge < 0 {
			cleared = true
		}
	}
	assert.True(t, cleared, "登出后应清除 refresh cookie")
}

// ---- 个人资料 ----

func TestHandler_Me_RequiresAuth(t *testing.T) {
	r, _ := newTestRouter(t)
	w := doJSON(t, r, http.MethodGet, "/api/me", "", "")
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Contains(t, w.Body.String(), "AUTH_REQUIRED")
}

func TestHandler_Me_Success(t *testing.T) {
	r, invites := newTestRouter(t)
	access, _ := registerAndLogin(t, r, invites)

	w := doJSON(t, r, http.MethodGet, "/api/me", "", access)
	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"username":"alice"`)
}

func TestHandler_UpdateMe_DisplayName(t *testing.T) {
	r, invites := newTestRouter(t)
	access, _ := registerAndLogin(t, r, invites)

	w := doJSON(t, r, http.MethodPatch, "/api/me", `{"display_name":"Alice2"}`, access)
	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"display_name":"Alice2"`)
}
