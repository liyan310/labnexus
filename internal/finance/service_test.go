package finance_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xuri/excelize/v2"

	"labnexus/internal/finance"
	"labnexus/internal/user"
)

// ---- 内存替身 ----

type memUserRepo struct {
	byID map[string]*user.User
}

func newMemUsers() *memUserRepo { return &memUserRepo{byID: map[string]*user.User{}} }

func (r *memUserRepo) seed(id, role string) *user.User {
	u := &user.User{ID: id, Username: id, DisplayName: id, Role: role}
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

type memRepo struct {
	batches      map[string]*finance.TurnoverBatch
	participants map[string]*finance.Participant
	byNameNo     map[string]string // "name|no" -> id
	items        map[string]*finance.TurnoverItem
	submissions  map[string]*finance.TurnoverSubmission
	txns         []*finance.Transaction
	account      *finance.Account
}

func newMemRepo() *memRepo {
	return &memRepo{
		batches:      map[string]*finance.TurnoverBatch{},
		participants: map[string]*finance.Participant{},
		byNameNo:     map[string]string{},
		items:        map[string]*finance.TurnoverItem{},
		submissions:  map[string]*finance.TurnoverSubmission{},
	}
}

func (r *memRepo) CreateBatch(_ context.Context, b *finance.TurnoverBatch) error {
	r.batches[b.ID] = b
	return nil
}
func (r *memRepo) GetBatch(_ context.Context, id string) (*finance.TurnoverBatch, error) {
	b, ok := r.batches[id]
	if !ok {
		return nil, finance.ErrNotFound
	}
	return b, nil
}
func (r *memRepo) UpdateBatch(_ context.Context, b *finance.TurnoverBatch) error {
	r.batches[b.ID] = b
	return nil
}
func (r *memRepo) DeleteBatch(ctx context.Context, id string) error {
	var itemIDs []string
	for _, it := range r.items {
		if it.BatchID == id {
			itemIDs = append(itemIDs, it.ID)
		}
	}
	for _, sid := range itemIDs {
		for k, s := range r.submissions {
			if s.ItemID == sid {
				delete(r.submissions, k)
			}
		}
		delete(r.items, sid)
	}
	delete(r.batches, id)
	return nil
}
func (r *memRepo) ListBatches(_ context.Context) ([]*finance.TurnoverBatch, error) {
	var out []*finance.TurnoverBatch
	for _, b := range r.batches {
		out = append(out, b)
	}
	return out, nil
}
func (r *memRepo) GetParticipant(_ context.Context, name, no string) (*finance.Participant, error) {
	id, ok := r.byNameNo[name+"|"+no]
	if !ok {
		return nil, finance.ErrNotFound
	}
	return r.participants[id], nil
}
func (r *memRepo) CreateParticipant(_ context.Context, p *finance.Participant) error {
	r.participants[p.ID] = p
	r.byNameNo[p.Name+"|"+p.StudentNo] = p.ID
	return nil
}
func (r *memRepo) ListParticipants(_ context.Context) ([]*finance.Participant, error) {
	var out []*finance.Participant
	for _, p := range r.participants {
		out = append(out, p)
	}
	return out, nil
}
func (r *memRepo) GetParticipantByID(_ context.Context, id string) (*finance.Participant, error) {
	p, ok := r.participants[id]
	if !ok {
		return nil, finance.ErrNotFound
	}
	return p, nil
}
func (r *memRepo) CreateItem(_ context.Context, it *finance.TurnoverItem) error {
	r.items[it.ID] = it
	return nil
}
func (r *memRepo) GetItem(_ context.Context, id string) (*finance.TurnoverItem, error) {
	it, ok := r.items[id]
	if !ok {
		return nil, finance.ErrNotFound
	}
	return it, nil
}
func (r *memRepo) UpdateItem(_ context.Context, it *finance.TurnoverItem) error {
	r.items[it.ID] = it
	return nil
}
func (r *memRepo) DeleteItemsByBatch(_ context.Context, batchID string) error {
	for k, it := range r.items {
		if it.BatchID == batchID {
			delete(r.items, k)
		}
	}
	return nil
}
func (r *memRepo) ListItems(_ context.Context, batchID string) ([]*finance.TurnoverItem, error) {
	var out []*finance.TurnoverItem
	for _, it := range r.items {
		if it.BatchID == batchID {
			out = append(out, it)
		}
	}
	return out, nil
}
func (r *memRepo) ListItemsByParticipant(_ context.Context, pid string) ([]*finance.TurnoverItem, error) {
	var out []*finance.TurnoverItem
	for _, it := range r.items {
		if it.ParticipantID == pid {
			out = append(out, it)
		}
	}
	return out, nil
}
func (r *memRepo) CreateSubmission(_ context.Context, s *finance.TurnoverSubmission) error {
	r.submissions[s.ID] = s
	return nil
}
func (r *memRepo) ListSubmissionsByItem(_ context.Context, itemID string) ([]*finance.TurnoverSubmission, error) {
	var out []*finance.TurnoverSubmission
	for _, s := range r.submissions {
		if s.ItemID == itemID {
			out = append(out, s)
		}
	}
	return out, nil
}
func (r *memRepo) DeleteSubmissionsByItem(_ context.Context, itemID string) error {
	for k, s := range r.submissions {
		if s.ItemID == itemID {
			delete(r.submissions, k)
		}
	}
	return nil
}
func (r *memRepo) EnsureDefaultAccount(_ context.Context) (*finance.Account, error) {
	if r.account == nil {
		r.account = finance.NewAccount("课题组经费账户", "", "")
	}
	return r.account, nil
}
func (r *memRepo) CreateTransaction(_ context.Context, t *finance.Transaction) error {
	r.txns = append(r.txns, t)
	return nil
}
func (r *memRepo) ListTransactions(_ context.Context) ([]*finance.Transaction, error) {
	return r.txns, nil
}
func (r *memRepo) Balance(_ context.Context) (int64, error) {
	var bal int64
	for _, t := range r.txns {
		if t.Type == finance.TxIncome {
			bal += t.Amount
		} else {
			bal -= t.Amount
		}
	}
	return bal, nil
}

type memPreviewStore struct {
	rows map[string][]finance.ImportRow
}

func newMemPreviewStore() *memPreviewStore {
	return &memPreviewStore{rows: map[string][]finance.ImportRow{}}
}

func (m *memPreviewStore) SetPreview(_ context.Context, id string, rows []finance.ImportRow, _ time.Duration) error {
	m.rows[id] = rows
	return nil
}
func (m *memPreviewStore) GetPreview(_ context.Context, id string) ([]finance.ImportRow, error) {
	rows, ok := m.rows[id]
	if !ok {
		return nil, finance.ErrPreviewNotFound
	}
	return rows, nil
}

// ---- 夹具 ----

type fixture struct {
	svc   *finance.Service
	users *memUserRepo
	repo  *memRepo
	prevs *memPreviewStore
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{
		users: newMemUsers(), repo: newMemRepo(), prevs: newMemPreviewStore(),
	}
	f.svc = finance.NewService(f.repo, f.users).
		WithPreviewStore(f.prevs).
		WithClock(func() time.Time { return time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC) })
	return f
}

const (
	adminID   = "u-admin"
	superID   = "u-super"
	studentID = "u-student"
)

func (f *fixture) seedRoles() {
	f.users.seed(adminID, user.RoleAdmin)
	f.users.seed(superID, user.RoleSupervisor)
	f.users.seed(studentID, user.RoleStudent)
}

func (f *fixture) newBatch(t *testing.T, name string) string {
	t.Helper()
	view, err := f.svc.CreateBatch(context.Background(), adminID, finance.CreateBatchRequest{Name: name})
	require.NoError(t, err)
	return view.ID
}

func (f *fixture) addItem(t *testing.T, batchID string, req finance.CreateItemRequest) *finance.ItemView {
	t.Helper()
	view, err := f.svc.CreateItem(context.Background(), adminID, batchID, req)
	require.NoError(t, err)
	return view
}

// ---- 权限 ----

func TestFinance_Permission(t *testing.T) {
	f := newFixture(t)
	f.seedRoles()

	// student → 403
	_, err := f.svc.CreateBatch(context.Background(), studentID, finance.CreateBatchRequest{Name: "x"})
	assert.ErrorIs(t, err, finance.ErrForbidden)

	// 未登录(不存在用户)→ 403
	_, err = f.svc.CreateBatch(context.Background(), "no-such", finance.CreateBatchRequest{Name: "x"})
	assert.ErrorIs(t, err, user.ErrNotFound)

	// admin/supervisor → OK
	_, err = f.svc.CreateBatch(context.Background(), adminID, finance.CreateBatchRequest{Name: "2026-08"})
	assert.NoError(t, err)
	_, err = f.svc.CreateBatch(context.Background(), superID, finance.CreateBatchRequest{Name: "2026-09"})
	assert.NoError(t, err)
}

// ---- 批次 ----

func TestFinance_BatchLifecycle(t *testing.T) {
	f := newFixture(t)
	f.seedRoles()

	// 名称空 → 400
	_, err := f.svc.CreateBatch(context.Background(), adminID, finance.CreateBatchRequest{Name: "  "})
	assert.ErrorIs(t, err, finance.ErrBatchNameEmpty)

	// 新建
	batchID := f.newBatch(t, "2026-08")
	got, err := f.svc.GetBatch(context.Background(), adminID, batchID)
	require.NoError(t, err)
	assert.Equal(t, "active", got.Status)
	assert.Equal(t, int64(0), got.Summary.ItemCount)

	// 未交清不能完成
	_, err = f.svc.CompleteBatch(context.Background(), adminID, batchID)
	assert.ErrorIs(t, err, finance.ErrBatchNotDone)

	// done 批次不可删
	// (先建一个交清的)
	batch2 := f.newBatch(t, "2026-09")
	item := f.addItem(t, batch2, finance.CreateItemRequest{
		Name: "张同学", StudentNo: "2023001", Date: "2026-08-20",
		PayrollAmount: 250000, TaxAmount: 0, TipAmount: 10000,
	})
	_, err = f.svc.Submit(context.Background(), adminID, item.ID, finance.SubmitRequest{
		Amount: 240000, Date: "2026-08-21",
	})
	require.NoError(t, err)
	_, err = f.svc.CompleteBatch(context.Background(), adminID, batch2)
	require.NoError(t, err)
	assert.ErrorIs(t, f.svc.DeleteBatch(context.Background(), adminID, batch2), finance.ErrCannotDelete)

	// active 可删
	require.NoError(t, f.svc.DeleteBatch(context.Background(), adminID, batchID))
}

// ---- 明细与应交公式 ----

func TestFinance_ItemFormulaAndParticipantReuse(t *testing.T) {
	f := newFixture(t)
	f.seedRoles()
	batchID := f.newBatch(t, "2026-08")

	// 应交自动 = 应发−扣税−辛苦费
	item := f.addItem(t, batchID, finance.CreateItemRequest{
		Name: "张同学", StudentNo: "2023001", Date: "2026-08-20",
		PayrollAmount: 250000, TaxAmount: 0, TipAmount: 10000,
	})
	assert.Equal(t, int64(240000), item.ShouldReturn)
	assert.Equal(t, int64(240000), item.Unreturned)
	assert.Equal(t, finance.ItemStatusPending, item.Status)

	// 手动覆盖应交
	item2 := f.addItem(t, batchID, finance.CreateItemRequest{
		Name: "李同学", StudentNo: "2023002", Date: "2026-08-20",
		PayrollAmount: 250000, TaxAmount: 5000, TipAmount: 10000, ShouldReturn: 235000,
	})
	assert.Equal(t, int64(235000), item2.ShouldReturn)

	// 缺字段 → 400
	_, err := f.svc.CreateItem(context.Background(), adminID, batchID, finance.CreateItemRequest{
		Name: "王同学", StudentNo: "", Date: "2026-08-20", PayrollAmount: 100,
	})
	assert.ErrorIs(t, err, finance.ErrItemFields)

	// 姓名+学号重复 → 复用 participant
	item3 := f.addItem(t, batchID, finance.CreateItemRequest{
		Name: "张同学", StudentNo: "2023001", Date: "2026-08-21",
		PayrollAmount: 100000, TaxAmount: 0, TipAmount: 0,
	})
	assert.Equal(t, item.ParticipantID, item3.ParticipantID, "同姓名学号应复用 participant")
}

// ---- 上交与资金池联动 ----

func TestFinance_SubmitAndLedger(t *testing.T) {
	f := newFixture(t)
	f.seedRoles()
	batchID := f.newBatch(t, "2026-08")
	item := f.addItem(t, batchID, finance.CreateItemRequest{
		Name: "张同学", StudentNo: "2023001", Date: "2026-08-20",
		PayrollAmount: 250000, TaxAmount: 0, TipAmount: 10000,
	})

	// 首次交 2100(210000 分)
	_, err := f.svc.Submit(context.Background(), adminID, item.ID, finance.SubmitRequest{
		Amount: 210000, Date: "2026-08-21",
	})
	require.NoError(t, err)
	got, _ := f.svc.GetBatch(context.Background(), adminID, batchID)
	it := got.Items[0]
	assert.Equal(t, int64(210000), it.Returned)
	assert.Equal(t, int64(30000), it.Unreturned)
	assert.Equal(t, finance.ItemStatusPartial, it.Status)

	// 补交 300 → 交清
	_, err = f.svc.Submit(context.Background(), adminID, item.ID, finance.SubmitRequest{
		Amount: 30000, Date: "2026-08-28",
	})
	require.NoError(t, err)
	got, _ = f.svc.GetBatch(context.Background(), adminID, batchID)
	assert.Equal(t, int64(240000), got.Items[0].Returned)
	assert.Equal(t, int64(0), got.Items[0].Unreturned)
	assert.Equal(t, finance.ItemStatusDone, got.Items[0].Status)

	// 超交 → 400
	_, err = f.svc.Submit(context.Background(), adminID, item.ID, finance.SubmitRequest{
		Amount: 100, Date: "2026-08-29",
	})
	assert.ErrorIs(t, err, finance.ErrOverSubmit)

	// 资金池:两次上交 2100+300=2400 元;余额 240000 分
	balance, txns, err := f.svc.Ledger(context.Background(), adminID)
	require.NoError(t, err)
	assert.Equal(t, int64(240000), balance)
	require.Len(t, txns, 2)
	for _, tx := range txns {
		assert.Equal(t, finance.TxIncome, tx.Type)
		assert.Equal(t, finance.CategoryTurnover, tx.Category)
	}
}

// ---- 导师补充与支出 ----

func TestFinance_LedgerIncomeExpense(t *testing.T) {
	f := newFixture(t)
	f.seedRoles()

	// 导师补充 5000 元
	_, err := f.svc.AddIncome(context.Background(), adminID, finance.LedgerRequest{
		Amount: 500000, Date: "2026-08-25", Note: "导师补充",
	})
	require.NoError(t, err)

	// 支出 1500 元(发劳务)
	_, err = f.svc.AddExpense(context.Background(), adminID, finance.LedgerRequest{
		Amount: 150000, Date: "2026-08-26", Note: "给小李发劳务",
	})
	require.NoError(t, err)

	balance, txns, err := f.svc.Ledger(context.Background(), superID)
	require.NoError(t, err)
	assert.Equal(t, int64(350000), balance, "5000−1500=3500 元")
	require.Len(t, txns, 2)

	// 负数金额 → 400
	_, err = f.svc.AddExpense(context.Background(), adminID, finance.LedgerRequest{Amount: -1, Date: "2026-08-26"})
	assert.ErrorIs(t, err, finance.ErrInvalidAmount)
}

// ---- 参与同学历史账单 ----

func TestFinance_ParticipantBills(t *testing.T) {
	f := newFixture(t)
	f.seedRoles()
	b1 := f.newBatch(t, "2026-08")
	b2 := f.newBatch(t, "2026-09")

	f.addItem(t, b1, finance.CreateItemRequest{
		Name: "张同学", StudentNo: "2023001", Date: "2026-08-20",
		PayrollAmount: 250000, TaxAmount: 0, TipAmount: 10000,
	})
	item2 := f.addItem(t, b2, finance.CreateItemRequest{
		Name: "张同学", StudentNo: "2023001", Date: "2026-09-20",
		PayrollAmount: 100000, TaxAmount: 0, TipAmount: 0,
	})
	_, _ = f.svc.Submit(context.Background(), adminID, item2.ID, finance.SubmitRequest{
		Amount: 100000, Date: "2026-09-21",
	})

	stats, err := f.svc.ListParticipants(context.Background(), adminID)
	require.NoError(t, err)
	require.Len(t, stats, 1, "同一人跨批次只出现一次")
	assert.Equal(t, int64(2), stats[0].TotalItems)
	assert.Equal(t, int64(340000), stats[0].TotalShouldReturn)
	assert.Equal(t, int64(100000), stats[0].TotalReturned)

	p, bills, err := f.svc.ParticipantBills(context.Background(), adminID, stats[0].ID)
	require.NoError(t, err)
	assert.Equal(t, "张同学", p.Name)
	require.Len(t, bills, 2)
	assert.Equal(t, "2026-08", bills[0].BatchName)
	assert.Equal(t, "2026-09", bills[1].BatchName)
}

// ---- Excel 导入 ----

func makeXLSX(t *testing.T, header []string, rows [][]string) []byte {
	t.Helper()
	f := excelizeTestFile(t, header, rows)
	var buf bytes.Buffer
	require.NoError(t, f.Write(&buf))
	return buf.Bytes()
}

func TestFinance_ImportPreviewAndConfirm(t *testing.T) {
	f := newFixture(t)
	f.seedRoles()
	batchID := f.newBatch(t, "2026-08")

	data := makeXLSX(t,
		[]string{"姓名", "学号", "日期", "应发", "扣税", "辛苦费", "备注"},
		[][]string{
			{"张同学", "2023001", "2026-08-20", "2500", "0", "100", "微信"},
			{"李同学", "2023002", "2026-08-20", "2500", "", "100", ""},
			{"", "2023003", "2026-08-20", "2500", "0", "100", "缺姓名"},
			{"王同学", "2023004", "2026-08-20", "abc", "0", "100", "金额错"},
		})

	previewID, valid, errs, err := f.svc.ImportPreview(context.Background(), adminID, batchID, bytes.NewReader(data))
	require.NoError(t, err)
	require.Len(t, valid, 2, "2 行有效")
	require.Len(t, errs, 2, "2 行错误")
	assert.Equal(t, int64(250000), valid[0].PayrollAmount)

	// 确认导入
	imported, skipped, err := f.svc.ConfirmImport(context.Background(), adminID, previewID, batchID)
	require.NoError(t, err)
	assert.Equal(t, 2, imported)
	assert.Equal(t, 0, skipped)

	got, err := f.svc.GetBatch(context.Background(), adminID, batchID)
	require.NoError(t, err)
	assert.Equal(t, int64(2), got.Summary.ItemCount)
	assert.Equal(t, int64(500000), got.Summary.TotalPayroll)

	// 预览过期/不存在 → 404
	_, _, err = f.svc.ConfirmImport(context.Background(), adminID, "no-such-preview", batchID)
	assert.ErrorIs(t, err, finance.ErrPreviewNotFound)

	// 非法 Excel → 400
	_, _, _, err = f.svc.ImportPreview(context.Background(), adminID, batchID, strings.NewReader("not an xlsx"))
	assert.ErrorIs(t, err, finance.ErrInvalidExcel)
}

// ---- 批次完成 ----

func TestFinance_CompleteBatchRequiresAllPaid(t *testing.T) {
	f := newFixture(t)
	f.seedRoles()
	batchID := f.newBatch(t, "2026-08")

	itemA := f.addItem(t, batchID, finance.CreateItemRequest{
		Name: "A", StudentNo: "1", Date: "2026-08-20", PayrollAmount: 100000,
	})
	f.addItem(t, batchID, finance.CreateItemRequest{
		Name: "B", StudentNo: "2", Date: "2026-08-20", PayrollAmount: 100000,
	})
	_, _ = f.svc.Submit(context.Background(), adminID, itemA.ID, finance.SubmitRequest{Amount: 100000, Date: "2026-08-21"})

	// B 未交 → 不能完成
	_, err := f.svc.CompleteBatch(context.Background(), adminID, batchID)
	assert.ErrorIs(t, err, finance.ErrBatchNotDone)
}

// ---- 工具 ----

// excelizeTestFile 生成测试 xlsx 文件(首行表头 + 数据行)。
func excelizeTestFile(t *testing.T, header []string, rows [][]string) *excelize.File {
	t.Helper()
	f := excelize.NewFile()
	sheet := "Sheet1"
	for i, h := range header {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		require.NoError(t, f.SetCellValue(sheet, cell, h))
	}
	for ri, row := range rows {
		for ci, v := range row {
			cell, _ := excelize.CoordinatesToCellName(ci+1, ri+2)
			require.NoError(t, f.SetCellValue(sheet, cell, v))
		}
	}
	return f
}

var _ = errors.Is
