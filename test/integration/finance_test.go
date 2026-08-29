//go:build integration

package integration

import (
	"bytes"
	"fmt"
	"mime/multipart"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xuri/excelize/v2"
)

// 经费管理全流程:注册 admin → 建批次 → 手动/导入明细 → 上交/补交 → 资金池核对 → 完成
func TestFinance_FullFlow(t *testing.T) {
	r := setupServer(t)
	// 经费负责人 = admin(通过 DB 提升角色)
	tokenA := registerUser(t, r, "finance_admin", "财务")
	db := connectDB(t)
	require.NoError(t, db.Exec("UPDATE users SET role = 'admin' WHERE username = 'finance_admin'").Error)
	closeDB(db)
	tokenB := registerUser(t, r, "finance_student", "普通学生")

	// 1. 学生访问 → 403
	w := doJSON(t, r, http.MethodGet, "/api/finance/batches", "", tokenB)
	assert.Equal(t, http.StatusForbidden, w.Code)

	// 2. 建批次
	wBatch := doJSON(t, r, http.MethodPost, "/api/finance/batches", `{"name":"2026-08","note":"暑假周转"}`, tokenA)
	require.Equal(t, http.StatusCreated, wBatch.Code, "%s", wBatch.Body.String())
	var batch struct {
		Batch struct {
			ID string `json:"id"`
		} `json:"batch"`
	}
	parseJSON(t, wBatch, &batch)

	// 3. 手动加明细(应交自动 = 应发−扣税−辛苦费)
	wItem := doJSON(t, r, http.MethodPost, "/api/finance/batches/"+batch.Batch.ID+"/items",
		`{"name":"张同学","student_no":"2023001","date":"2026-08-20","payroll_amount":250000,"tip_amount":10000}`, tokenA)
	require.Equal(t, http.StatusCreated, wItem.Code, "%s", wItem.Body.String())
	assert.Contains(t, wItem.Body.String(), `"should_return":240000`)
	var item struct {
		Item struct {
			ID string `json:"id"`
		} `json:"item"`
	}
	parseJSON(t, wItem, &item)

	// 4. 上交 2100 → 资金池 +2100
	wSub := doJSON(t, r, http.MethodPost, "/api/finance/items/"+item.Item.ID+"/submit",
		`{"amount":210000,"date":"2026-08-21","note":"微信转账"}`, tokenA)
	require.Equal(t, http.StatusCreated, wSub.Code, "%s", wSub.Body.String())

	wLedger := doJSON(t, r, http.MethodGet, "/api/finance/ledger", "", tokenA)
	require.Equal(t, http.StatusOK, wLedger.Code)
	assert.Contains(t, wLedger.Body.String(), `"balance":210000`)

	// 5. 补交 300 → 交清
	wSub2 := doJSON(t, r, http.MethodPost, "/api/finance/items/"+item.Item.ID+"/submit",
		`{"amount":30000,"date":"2026-08-28","note":"补交"}`, tokenA)
	require.Equal(t, http.StatusCreated, wSub2.Code)

	wLedger2 := doJSON(t, r, http.MethodGet, "/api/finance/ledger", "", tokenA)
	assert.Contains(t, wLedger2.Body.String(), `"balance":240000`)

	// 6. 批次完成
	wDone := doJSON(t, r, http.MethodPost, "/api/finance/batches/"+batch.Batch.ID+"/complete", "", tokenA)
	require.Equal(t, http.StatusOK, wDone.Code)
	assert.Contains(t, wDone.Body.String(), `"status":"done"`)

	// 7. 批次列表含汇总
	wList := doJSON(t, r, http.MethodGet, "/api/finance/batches", "", tokenA)
	require.Equal(t, http.StatusOK, wList.Code)
	assert.Contains(t, wList.Body.String(), `"total_unreturned":0`)
}

// Excel 导入全流程:上传 → 预览 → 确认 → 明细入库
func TestFinance_ImportFlow(t *testing.T) {
	r := setupServer(t)
	tokenA := registerUser(t, r, "finance_imp", "导入员")
	db := connectDB(t)
	require.NoError(t, db.Exec("UPDATE users SET role = 'admin' WHERE username = 'finance_imp'").Error)
	closeDB(db)

	wBatch := doJSON(t, r, http.MethodPost, "/api/finance/batches", `{"name":"2026-09"}`, tokenA)
	var batch struct {
		Batch struct {
			ID string `json:"id"`
		} `json:"batch"`
	}
	parseJSON(t, wBatch, &batch)

	// 生成 xlsx(含 1 行错误)
	f := excelize.NewFile()
	_ = f.SetCellValue("Sheet1", "A1", "姓名")
	_ = f.SetCellValue("Sheet1", "B1", "学号")
	_ = f.SetCellValue("Sheet1", "C1", "日期")
	_ = f.SetCellValue("Sheet1", "D1", "应发")
	_ = f.SetCellValue("Sheet1", "E1", "扣税")
	_ = f.SetCellValue("Sheet1", "F1", "辛苦费")
	_ = f.SetCellValue("Sheet1", "A2", "张同学")
	_ = f.SetCellValue("Sheet1", "B2", "2023001")
	_ = f.SetCellValue("Sheet1", "C2", "2026-09-10")
	_ = f.SetCellValue("Sheet1", "D2", "2500")
	_ = f.SetCellValue("Sheet1", "E2", "0")
	_ = f.SetCellValue("Sheet1", "F2", "100")
	_ = f.SetCellValue("Sheet1", "A3", "李同学")
	_ = f.SetCellValue("Sheet1", "B3", "2023002")
	_ = f.SetCellValue("Sheet1", "C3", "2026-09-10")
	_ = f.SetCellValue("Sheet1", "D3", "abc") // 错误行
	var buf bytes.Buffer
	require.NoError(t, f.Write(&buf))

	// 上传预览
	var mw bytes.Buffer
	mp := multipart.NewWriter(&mw)
	fw, _ := mp.CreateFormFile("file", "import.xlsx")
	_, _ = fw.Write(buf.Bytes())
	require.NoError(t, mp.Close())
	req := newReq(t, http.MethodPost, "/api/finance/batches/"+batch.Batch.ID+"/items/import-preview", &mw)
	req.Header.Set("Content-Type", mp.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+tokenA)
	wPrev := doReq(t, r, req)
	require.Equal(t, http.StatusOK, wPrev.Code, "%s", wPrev.Body.String())
	assert.Contains(t, wPrev.Body.String(), `"valid_count":1`)
	assert.Contains(t, wPrev.Body.String(), `"error_count":1`)
	var preview struct {
		PreviewID string `json:"preview_id"`
	}
	parseJSON(t, wPrev, &preview)
	require.NotEmpty(t, preview.PreviewID)

	// 确认导入
	wConf := doJSON(t, r, http.MethodPost,
		"/api/finance/imports/"+preview.PreviewID+"/confirm?batch_id="+batch.Batch.ID, "", tokenA)
	require.Equal(t, http.StatusOK, wConf.Code, "%s", wConf.Body.String())
	assert.Contains(t, wConf.Body.String(), `"imported_count":1`)

	// 批次汇总:1 条明细,应交 2400 元
	wDetail := doJSON(t, r, http.MethodGet, "/api/finance/batches/"+batch.Batch.ID, "", tokenA)
	require.Equal(t, http.StatusOK, wDetail.Code)
	assert.Contains(t, wDetail.Body.String(), `"item_count":1`)
	assert.Contains(t, wDetail.Body.String(), `"total_should_return":240000`)
}

// 越权与边界:学生 403、超交 400、未交清不能完成
func TestFinance_PermissionAndBoundaries(t *testing.T) {
	r := setupServer(t)
	tokenA := registerUser(t, r, "finance_edge", "边界员")
	db := connectDB(t)
	require.NoError(t, db.Exec("UPDATE users SET role = 'admin' WHERE username = 'finance_edge'").Error)
	closeDB(db)
	tokenS := registerUser(t, r, "finance_edge_s", "学生")

	// 学生全程 403
	for _, p := range []string{
		"/api/finance/batches",
		"/api/finance/ledger",
		"/api/finance/participants",
	} {
		assert.Equal(t, http.StatusForbidden, doJSON(t, r, http.MethodGet, p, "", tokenS).Code, p)
	}

	wBatch := doJSON(t, r, http.MethodPost, "/api/finance/batches", `{"name":"2026-10"}`, tokenA)
	var batch struct {
		Batch struct {
			ID string `json:"id"`
		} `json:"batch"`
	}
	parseJSON(t, wBatch, &batch)

	wItem := doJSON(t, r, http.MethodPost, "/api/finance/batches/"+batch.Batch.ID+"/items",
		`{"name":"王同学","student_no":"3","date":"2026-10-01","payroll_amount":100000}`, tokenA)
	var item struct {
		Item struct {
			ID string `json:"id"`
		} `json:"item"`
	}
	parseJSON(t, wItem, &item)

	// 超交 → 400
	wOver := doJSON(t, r, http.MethodPost, "/api/finance/items/"+item.Item.ID+"/submit",
		`{"amount":9999999,"date":"2026-10-02"}`, tokenA)
	assert.Equal(t, http.StatusBadRequest, wOver.Code)

	// 未交清不能完成 → 400
	wDone := doJSON(t, r, http.MethodPost, "/api/finance/batches/"+batch.Batch.ID+"/complete", "", tokenA)
	assert.Equal(t, http.StatusBadRequest, wDone.Code)
	assert.Contains(t, wDone.Body.String(), "unreturned")

	// 非法金额 → 400
	wBad := doJSON(t, r, http.MethodPost, "/api/finance/ledger/income",
		`{"amount":-1,"date":"2026-10-03"}`, tokenA)
	assert.Equal(t, http.StatusBadRequest, wBad.Code)
}

var _ = fmt.Sprintf
