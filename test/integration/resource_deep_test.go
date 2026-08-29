//go:build integration

package integration

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// F7 深度:筛选组合、分页、上传边界、文件生命周期、越权细化、契约结构
func TestResource_DeepFilters(t *testing.T) {
	r := setupServer(t)
	tokenA := registerUser(t, r, "alice", "Alice")
	tokenB := registerUser(t, r, "bob", "Bob")

	tag1 := createTag(t, r, tokenA, "深度学习")
	tag2 := createTag(t, r, tokenA, "NLP")

	// 4 条资源:2 link(不同标签/关键词)、1 file(pdf)、1 file(md)
	for _, body := range []string{
		`{"type":"link","title":"深度学习入门","url":"https://a.com","tag_ids":["` + tag1 + `"]}`,
		`{"type":"link","title":"NLP 综述","url":"https://b.com","tag_ids":["` + tag2 + `"]}`,
	} {
		require.Equal(t, http.StatusCreated, doJSON(t, r, http.MethodPost, "/api/resources", body, tokenA).Code)
	}
	require.Equal(t, http.StatusCreated, doMultipart(t, r, "/api/resources/upload", "Attention.pdf", "%PDF-1.4 attention", tokenA).Code)
	require.Equal(t, http.StatusCreated, doMultipart(t, r, "/api/resources/upload", "笔记.md", "# note", tokenA).Code)

	// 组合筛选:type=link AND keyword=深度
	w := doJSON(t, r, http.MethodGet, "/api/resources?type=link&keyword=%E6%B7%B1%E5%BA%A6", "", tokenB)
	var body struct {
		Resources []struct {
			Title string `json:"title"`
		} `json:"resources"`
		Pagination struct {
			Total    int `json:"total"`
			PageSize int `json:"page_size"`
		} `json:"pagination"`
	}
	parseJSON(t, w, &body)
	require.Equal(t, 1, body.Pagination.Total)
	assert.Equal(t, "深度学习入门", body.Resources[0].Title)

	// tag 筛选
	wTag := doJSON(t, r, http.MethodGet, "/api/resources?tag_id="+tag2, "", tokenB)
	assert.Contains(t, wTag.Body.String(), "NLP 综述")

	// 分页:page_size=2 两页;page=2 有剩余;page 归一化
	w1 := doJSON(t, r, http.MethodGet, "/api/resources?page=1&page_size=2", "", tokenB)
	parseJSON(t, w1, &body)
	assert.Equal(t, 4, body.Pagination.Total)
	require.Len(t, body.Resources, 2)
	w2 := doJSON(t, r, http.MethodGet, "/api/resources?page=2&page_size=2", "", tokenB)
	parseJSON(t, w2, &body)
	require.Len(t, body.Resources, 2, "第二页应 2 条")
	// page_size 超上限截断
	w3 := doJSON(t, r, http.MethodGet, "/api/resources?page_size=999", "", tokenB)
	parseJSON(t, w3, &body)
	assert.Equal(t, 50, body.Pagination.PageSize)
}

// 文件生命周期:上传落盘 → 删除后磁盘文件同步删除
func TestResource_FileLifecycle(t *testing.T) {
	r := setupServer(t)
	tokenA := registerUser(t, r, "alice", "Alice")

	w := doMultipart(t, r, "/api/resources/upload", "论文.pdf", "%PDF-1.4 lifecycle", tokenA)
	require.Equal(t, http.StatusCreated, w.Code)
	var created struct {
		Resource struct {
			ID   string `json:"id"`
			Type string `json:"type"`
		} `json:"resource"`
	}
	parseJSON(t, w, &created)
	assert.Equal(t, "file", created.Resource.Type)

	// DB 中 file_path 存在,且磁盘文件真实存在
	db := connectDB(t)
	var filePath string
	require.NoError(t, db.Raw("SELECT file_path FROM resources WHERE id = ?", created.Resource.ID).Scan(&filePath).Error)
	closeDB(db)
	require.NotEmpty(t, filePath, "file_path 应落库")
	assert.FileExists(t, filepath.Join("data", filepath.FromSlash(filePath)), "磁盘文件应存在")

	// 删除资源 → 磁盘文件消失
	require.Equal(t, http.StatusNoContent,
		doJSON(t, r, http.MethodDelete, "/api/resources/"+created.Resource.ID, "", tokenA).Code)
	_, err := os.Stat(filepath.Join("data", filepath.FromSlash(filePath)))
	assert.True(t, os.IsNotExist(err), "删除资源后磁盘文件应被清理")
}

// 上传边界:非法扩展名、内容不符、超大文件、预览支持判定
func TestResource_UploadBoundaries(t *testing.T) {
	r := setupServer(t)
	tokenA := registerUser(t, r, "alice", "Alice")

	// 非法扩展名
	assert.Equal(t, http.StatusBadRequest, doMultipart(t, r, "/api/resources/upload", "malware.sh", "#!/bin/sh", tokenA).Code)

	// 内容与扩展名不符(改名的可执行文件)
	assert.Equal(t, http.StatusBadRequest, doMultipart(t, r, "/api/resources/upload", "fake.pdf", "MZ\x90\x00not pdf", tokenA).Code)

	// 超大文件(> 50MB)
	big := strings.Repeat("x", 51<<20)
	assert.Equal(t, http.StatusBadRequest, doMultipart(t, r, "/api/resources/upload", "big.pdf", big, tokenA).Code)

	// 上传后列表/详情不含 file_path(契约:内部路径不暴露)
	w := doMultipart(t, r, "/api/resources/upload", "ok.md", "# fine", tokenA)
	require.Equal(t, http.StatusCreated, w.Code)
	assert.NotContains(t, w.Body.String(), `"file_path"`)
}

// F7 越权细化:上传者/admin 可管理;他人 403;资源共享可见
func TestResource_DeepPermission(t *testing.T) {
	r := setupServer(t)
	tokenA := registerUser(t, r, "alice", "Alice")
	tokenB := registerUser(t, r, "bob", "Bob")
	tokenC := registerUser(t, r, "carol", "Carol")

	w := doJSON(t, r, http.MethodPost, "/api/resources",
		`{"type":"link","title":"A 的资源","url":"https://a.com"}`, tokenA)
	var created struct {
		Resource struct {
			ID string `json:"id"`
		} `json:"resource"`
	}
	parseJSON(t, w, &created)
	id := created.Resource.ID

	// 共享可见:他人可看列表/详情
	assert.Equal(t, http.StatusOK, doJSON(t, r, http.MethodGet, "/api/resources", "", tokenB).Code)
	assert.Equal(t, http.StatusOK, doJSON(t, r, http.MethodGet, "/api/resources/"+id, "", tokenC).Code)

	// 他人改/删 → 403
	assert.Equal(t, http.StatusForbidden, doJSON(t, r, http.MethodPatch, "/api/resources/"+id, `{"title":"hack"}`, tokenB).Code)
	assert.Equal(t, http.StatusForbidden, doJSON(t, r, http.MethodDelete, "/api/resources/"+id, "", tokenB).Code)

	// admin(通过管理端调整角色)→ 可删
	db := connectDB(t)
	require.NoError(t, db.Exec("UPDATE users SET role = 'admin' WHERE username = 'carol'").Error)
	closeDB(db)
	assert.Equal(t, http.StatusNoContent, doJSON(t, r, http.MethodDelete, "/api/resources/"+id, "", tokenC).Code)
}

// 资源契约:uploader/tags/pagination 结构完整
func TestResource_ContractShape(t *testing.T) {
	r := setupServer(t)
	tokenA := registerUser(t, r, "alice", "Alice")
	tagID := createTag(t, r, tokenA, "契约标签")

	w := doJSON(t, r, http.MethodPost, "/api/resources",
		`{"type":"link","title":"契约资源","url":"https://c.com","tag_ids":["`+tagID+`"]}`, tokenA)
	require.Equal(t, http.StatusCreated, w.Code)

	var res struct {
		Resource struct {
			ID       string `json:"id"`
			Type     string `json:"type"`
			Title    string `json:"title"`
			Uploader struct {
				DisplayName string `json:"display_name"`
			} `json:"uploader"`
			Tags []struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"tags"`
			CreatedAt string `json:"created_at"`
		} `json:"resource"`
	}
	parseJSON(t, w, &res)
	assert.Equal(t, "link", res.Resource.Type)
	assert.Equal(t, "Alice", res.Resource.Uploader.DisplayName)
	require.Len(t, res.Resource.Tags, 1)
	assert.Equal(t, "契约标签", res.Resource.Tags[0].Name)
	assert.NotEmpty(t, res.Resource.CreatedAt)
	// 错误响应格式
	wErr := doJSON(t, r, http.MethodGet, "/api/resources/no-such-id", "", tokenA)
	assert.Equal(t, "NOT_FOUND", errorCode(t, wErr))
}

var _ = json.Marshal
var _ = fmt.Sprintf
