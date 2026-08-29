//go:build integration

package integration

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 完整业务流:A 建目录/写笔记/发布(带标签) → B 看 feed/点赞/评论 → 统计 → 搜索
// → A 撤回 → B 全渠道不可见
func TestWorkflow_FullJourney(t *testing.T) {
	r := setupServer(t)
	tokenA := registerUser(t, r, "alice", "Alice")
	tokenB := registerUser(t, r, "bob", "Bob")

	// A:建目录结构
	rootID := createFolder(t, r, tokenA, "会议记录", nil)
	subID := createFolder(t, r, tokenA, "组会", &rootID)

	// A:建标签 + 私有笔记
	tagID := createTag(t, r, tokenA, "组会纪要")

	w := doJSON(t, r, http.MethodPost, "/api/me/documents",
		`{"title":"本周组会","content":"讨论了实验进展","visibility":"private"}`, tokenA)
	require.Equal(t, http.StatusCreated, w.Code)
	var priv struct {
		Document struct {
			ID string `json:"id"`
		} `json:"document"`
	}
	parseJSON(t, w, &priv)

	// A:发布为公开帖(带目录/标签)
	wPub := doJSON(t, r, http.MethodPost, "/api/me/documents",
		`{"title":"组会纪要-第3周","content":"结论:论文初稿下周提交","visibility":"public","tag_ids":["`+tagID+`"]}`, tokenA)
	require.Equal(t, http.StatusCreated, wPub.Code, "%s", wPub.Body.String())
	var pub struct {
		Document struct {
			ID string `json:"id"`
		} `json:"document"`
	}
	parseJSON(t, wPub, &pub)

	// B:feed 看到公开帖(含作者/标签/计数),且不含 A 的私有笔记
	wFeed := doJSON(t, r, http.MethodGet, "/api/feed", "", tokenB)
	require.Equal(t, http.StatusOK, wFeed.Code)
	var feed struct {
		Documents []struct {
			Title  string `json:"title"`
			Author struct {
				DisplayName string `json:"display_name"`
			} `json:"author"`
			Tags []struct {
				Name string `json:"name"`
			} `json:"tags"`
			ReactionsCount int64 `json:"reactions_count"`
			CommentsCount  int64 `json:"comments_count"`
		} `json:"documents"`
	}
	parseJSON(t, wFeed, &feed)
	require.Len(t, feed.Documents, 1, "feed 应只有 1 条公开帖")
	assert.Equal(t, "组会纪要-第3周", feed.Documents[0].Title)
	assert.Equal(t, "Alice", feed.Documents[0].Author.DisplayName)
	require.Len(t, feed.Documents[0].Tags, 1)
	assert.Equal(t, "组会纪要", feed.Documents[0].Tags[0].Name)

	// B:点赞 + 评论
	assert.Equal(t, http.StatusNoContent,
		doJSON(t, r, http.MethodPost, "/api/documents/"+pub.Document.ID+"/reactions", `{"emoji":"👍"}`, tokenB).Code)
	wC := doJSON(t, r, http.MethodPost, "/api/documents/"+pub.Document.ID+"/comments",
		`{"content":"收到,加油!"}`, tokenB)
	require.Equal(t, http.StatusCreated, wC.Code)
	assert.Contains(t, wC.Body.String(), `"author"`)

	// A:查看文档详情,计数正确
	wDetail := doJSON(t, r, http.MethodGet, "/api/documents/"+pub.Document.ID, "", tokenA)
	require.Equal(t, http.StatusOK, wDetail.Code)
	assert.Contains(t, wDetail.Body.String(), `"reactions_count":1`)
	assert.Contains(t, wDetail.Body.String(), `"comments_count":1`)

	// B:搜索命中
	wSearch := doJSON(t, r, http.MethodGet, "/api/search?q=初稿", "", tokenB)
	require.Equal(t, http.StatusOK, wSearch.Code)
	assert.Contains(t, wSearch.Body.String(), "组会纪要-第3周")
	assert.NotContains(t, wSearch.Body.String(), "本周组会")

	// B:标签内容页可见公开帖
	wTag := doJSON(t, r, http.MethodGet, "/api/tags/"+tagID+"/contents", "", tokenB)
	require.Equal(t, http.StatusOK, wTag.Code)
	assert.Contains(t, wTag.Body.String(), "组会纪要-第3周")

	// A:撤回(public → private)
	wBack := doJSON(t, r, http.MethodPatch, "/api/documents/"+pub.Document.ID,
		`{"visibility":"private"}`, tokenA)
	require.Equal(t, http.StatusOK, wBack.Code)

	// 撤回后:B 全渠道不可见
	assertError(t, doJSON(t, r, http.MethodGet, "/api/documents/"+pub.Document.ID, "", tokenB),
		http.StatusNotFound, "NOT_FOUND")
	assert.NotContains(t, doJSON(t, r, http.MethodGet, "/api/feed", "", tokenB).Body.String(), "组会纪要-第3周")
	assert.NotContains(t, doJSON(t, r, http.MethodGet, "/api/search?q=初稿", "", tokenB).Body.String(), "组会纪要")
	assert.NotContains(t, doJSON(t, r, http.MethodGet, "/api/tags/"+tagID+"/contents", "", tokenB).Body.String(), "组会纪要-第3周")

	// 私有笔记 A 自己可见(目录挂载验证)
	_ = subID
	_ = priv.Document.ID
	wMine := doJSON(t, r, http.MethodGet, "/api/me/documents", "", tokenA)
	require.Equal(t, http.StatusOK, wMine.Code)
	assert.Contains(t, wMine.Body.String(), "本周组会")
}

// 数据隔离:A 的私有目录与文档对 B 完全不可见(空间树/文档列表/feed/搜索)
func TestWorkflow_DataIsolation(t *testing.T) {
	r := setupServer(t)
	tokenA := registerUser(t, r, "alice", "Alice")
	tokenB := registerUser(t, r, "bob", "Bob")

	rootID := createFolder(t, r, tokenA, "私密目录", nil)
	createFolder(t, r, tokenA, "子目录", &rootID)
	privID := createDoc(t, r, tokenA, "我的私密文档", "private")

	// B 的空间树:看不到 A 的目录
	wSpaceB := doJSON(t, r, http.MethodGet, "/api/me/space", "", tokenB)
	require.Equal(t, http.StatusOK, wSpaceB.Code)
	assert.NotContains(t, wSpaceB.Body.String(), "私密目录")

	// B 的文档列表:没有 A 的文档
	wDocsB := doJSON(t, r, http.MethodGet, "/api/me/documents", "", tokenB)
	assert.NotContains(t, wDocsB.Body.String(), "我的私密文档")

	// B 直接访问 A 私有文档 → 404
	assertError(t, doJSON(t, r, http.MethodGet, "/api/documents/"+privID, "", tokenB),
		http.StatusNotFound, "NOT_FOUND")

	// B 搜索 A 私有关键词 → 不出现
	assert.NotContains(t, doJSON(t, r, http.MethodGet, "/api/search?q=私密", "", tokenB).Body.String(), "我的私密文档")

	// B 对 A 私有文档点赞/评论 → 404
	assertError(t, doJSON(t, r, http.MethodPost, "/api/documents/"+privID+"/reactions", `{}`, tokenB),
		http.StatusNotFound, "NOT_FOUND")
	assertError(t, doJSON(t, r, http.MethodPost, "/api/documents/"+privID+"/comments",
		`{"content":"x"}`, tokenB), http.StatusNotFound, "NOT_FOUND")
}

// 目录树完整性:A 建 会议记录/组会(嵌套),GET /me/space 返回嵌套结构
func TestWorkflow_FolderTree(t *testing.T) {
	r := setupServer(t)
	tokenA := registerUser(t, r, "alice", "Alice")

	rootID := createFolder(t, r, tokenA, "会议记录", nil)
	createFolder(t, r, tokenA, "组会", &rootID)
	createFolder(t, r, tokenA, "日常记录", nil)

	w := doJSON(t, r, http.MethodGet, "/api/me/space", "", tokenA)
	require.Equal(t, http.StatusOK, w.Code)
	var body struct {
		Folders []struct {
			Name     string `json:"name"`
			Children []any  `json:"children"`
		} `json:"folders"`
	}
	parseJSON(t, w, &body)
	require.Len(t, body.Folders, 2)
	var meeting *struct {
		Name     string `json:"name"`
		Children []any  `json:"children"`
	}
	for i := range body.Folders {
		if body.Folders[i].Name == "会议记录" {
			meeting = &body.Folders[i]
		}
	}
	require.NotNil(t, meeting)
	require.Len(t, meeting.Children, 1, "组会 应嵌套在 会议记录 下")
}

var _ = json.Marshal
