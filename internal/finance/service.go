// Package finance 业务层:F10 经费管理。
// 核心模型:收入 + 支出 → 维护总金额。
// 依据规格:docs/specs/funds-management.md;契约:api-contract.md §F10。
package finance

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/xuri/excelize/v2"

	"labnexus/internal/database"
	"labnexus/internal/user"
)

// 哨兵错误(handler 层统一映射 HTTP)
var (
	ErrForbidden           = errors.New("finance: admin/supervisor permission required")
	ErrBatchNotFound       = errors.New("batch not found")
	ErrItemNotFound        = errors.New("item not found")
	ErrBatchNameEmpty      = errors.New("batch name is empty")
	ErrBatchDone           = errors.New("batch is done")
	ErrBatchNotDone        = errors.New("batch has unreturned items, cannot complete")
	ErrCannotDelete        = errors.New("cannot delete done batch")
	ErrItemFields          = errors.New("name, student_no, date and payroll_amount are required")
	ErrInvalidDate         = errors.New("invalid date, expected YYYY-MM-DD")
	ErrInvalidAmount       = errors.New("amount must be positive")
	ErrOverSubmit          = errors.New("submit amount exceeds unreturned balance")
	ErrParticipantNotFound = errors.New("participant not found")
	ErrPreviewNotFound     = errors.New("import preview not found or expired")
	ErrInvalidExcel        = errors.New("invalid excel file")
	ErrTooManyRows         = errors.New("excel rows exceed limit")
	ErrAccountNotFound     = errors.New("account not found")
)

// 常量
const (
	MaxImportRows = 500 // 单次导入最大行数
	previewTTL    = 30 * time.Minute
)

// ImportPreviewStore 导入预览存储(测试可注入内存实现)。
type ImportPreviewStore interface {
	SetPreview(ctx context.Context, id string, rows []ImportRow, ttl time.Duration) error
	GetPreview(ctx context.Context, id string) ([]ImportRow, error)
}

// Service 经费业务逻辑
type Service struct {
	repo     Repository
	users    user.Repository
	txRunner database.TxRunner
	previews ImportPreviewStore
	now      func() time.Time
}

// NewService 构造函数(依赖注入)。
func NewService(repo Repository, users user.Repository) *Service {
	return &Service{
		repo:     repo,
		users:    users,
		txRunner: database.NoopTxRunner(),
		now:      time.Now,
	}
}

// WithTxRunner 注入事务运行器。
func (s *Service) WithTxRunner(runner database.TxRunner) *Service {
	s.txRunner = runner
	return s
}

// WithPreviewStore 注入导入预览存储。
func (s *Service) WithPreviewStore(store ImportPreviewStore) *Service {
	s.previews = store
	return s
}

// WithClock 注入时钟(测试)。
func (s *Service) WithClock(now func() time.Time) *Service {
	s.now = now
	return s
}

// ---- 视图 ----

// ItemView 明细视图(含参与同学/上交记录/计算字段)
type ItemView struct {
	*TurnoverItem
	Participant *Participant          `json:"participant"`
	Submissions []*TurnoverSubmission `json:"submissions"`
	Unreturned  int64                 `json:"unreturned"`
	Status      string                `json:"status"`
}

// BatchSummary 批次汇总(分)
type BatchSummary struct {
	ItemCount         int64 `json:"item_count"`
	TotalPayroll      int64 `json:"total_payroll"`
	TotalShouldReturn int64 `json:"total_should_return"`
	TotalReturned     int64 `json:"total_returned"`
	TotalUnreturned   int64 `json:"total_unreturned"`
}

// BatchView 批次视图(列表含汇总,详情含明细)
type BatchView struct {
	*TurnoverBatch
	Summary *BatchSummary `json:"summary"`
	Items   []*ItemView   `json:"items,omitempty"`
}

// TransactionView 流水视图(含经手人)
type TransactionView struct {
	*Transaction
	Operator *user.User `json:"operator"`
}

// ParticipantStat 参与同学统计
type ParticipantStat struct {
	*Participant
	TotalItems        int64 `json:"total_items"`
	TotalShouldReturn int64 `json:"total_should_return"`
	TotalReturned     int64 `json:"total_returned"`
}

// BillView 历史账单条目
type BillView struct {
	BatchName     string `json:"batch_name"`
	Date          string `json:"date"`
	PayrollAmount int64  `json:"payroll_amount"`
	ShouldReturn  int64  `json:"should_return"`
	Returned      int64  `json:"returned"`
	Unreturned    int64  `json:"unreturned"`
	Note          string `json:"note"`
}

// ImportRow 导入解析行(预览用,金额为分)
type ImportRow struct {
	Name          string `json:"name"`
	StudentNo     string `json:"student_no"`
	Date          string `json:"date"`
	PayrollAmount int64  `json:"payroll_amount"`
	TaxAmount     int64  `json:"tax_amount"`
	TipAmount     int64  `json:"tip_amount"`
	Note          string `json:"note"`
}

// ---- 请求 ----

type CreateBatchRequest struct {
	Name string `json:"name"`
	Note string `json:"note"`
}

type CreateItemRequest struct {
	Name          string `json:"name"`
	StudentNo     string `json:"student_no"`
	Date          string `json:"date"`
	PayrollAmount int64  `json:"payroll_amount"`
	TaxAmount     int64  `json:"tax_amount"`
	TipAmount     int64  `json:"tip_amount"`
	ShouldReturn  int64  `json:"should_return"` // 0 = 自动按公式
	Note          string `json:"note"`
}

type SubmitRequest struct {
	Amount int64  `json:"amount"`
	Date   string `json:"date"`
	Note   string `json:"note"`
}

type LedgerRequest struct {
	Amount int64  `json:"amount"`
	Date   string `json:"date"`
	Note   string `json:"note"`
}

// ---- 权限 ----

// requireFinance 校验 admin/supervisor 角色。
func (s *Service) requireFinance(ctx context.Context, userID string) error {
	u, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return err
	}
	if u.Role != user.RoleAdmin && u.Role != user.RoleSupervisor {
		return ErrForbidden
	}
	return nil
}

// ---- 批次 ----

// CreateBatch 新建批次。
func (s *Service) CreateBatch(ctx context.Context, userID string, req CreateBatchRequest) (*BatchView, error) {
	if err := s.requireFinance(ctx, userID); err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.Name) == "" {
		return nil, ErrBatchNameEmpty
	}
	b := NewBatch(strings.TrimSpace(req.Name), req.Note, userID)
	if err := s.repo.CreateBatch(ctx, b); err != nil {
		return nil, err
	}
	return s.buildBatchView(ctx, b, false)
}

// ListBatches 批次列表(含汇总)。
func (s *Service) ListBatches(ctx context.Context, userID string) ([]*BatchView, error) {
	if err := s.requireFinance(ctx, userID); err != nil {
		return nil, err
	}
	list, err := s.repo.ListBatches(ctx)
	if err != nil {
		return nil, err
	}
	views := make([]*BatchView, 0, len(list))
	for _, b := range list {
		view, err := s.buildBatchView(ctx, b, false)
		if err != nil {
			return nil, err
		}
		views = append(views, view)
	}
	return views, nil
}

// GetBatch 批次详情(含明细)。
func (s *Service) GetBatch(ctx context.Context, userID, batchID string) (*BatchView, error) {
	if err := s.requireFinance(ctx, userID); err != nil {
		return nil, err
	}
	b, err := s.repo.GetBatch(ctx, batchID)
	if err != nil {
		return nil, ErrBatchNotFound
	}
	return s.buildBatchView(ctx, b, true)
}

// CompleteBatch 标记批次完成(全部交清才可)。
func (s *Service) CompleteBatch(ctx context.Context, userID, batchID string) (*BatchView, error) {
	if err := s.requireFinance(ctx, userID); err != nil {
		return nil, err
	}
	b, err := s.repo.GetBatch(ctx, batchID)
	if err != nil {
		return nil, ErrBatchNotFound
	}
	items, err := s.repo.ListItems(ctx, batchID)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, ErrBatchNotDone
	}
	for _, it := range items {
		if it.Unreturned() > 0 {
			return nil, ErrBatchNotDone
		}
	}
	b.Status = BatchStatusDone
	b.UpdatedAt = s.now()
	if err := s.repo.UpdateBatch(ctx, b); err != nil {
		return nil, err
	}
	return s.buildBatchView(ctx, b, true)
}

// DeleteBatch 删除 active 批次(级联明细/上交)。
func (s *Service) DeleteBatch(ctx context.Context, userID, batchID string) error {
	if err := s.requireFinance(ctx, userID); err != nil {
		return err
	}
	b, err := s.repo.GetBatch(ctx, batchID)
	if err != nil {
		return ErrBatchNotFound
	}
	if b.Status == BatchStatusDone {
		return ErrCannotDelete
	}
	return s.repo.DeleteBatch(ctx, batchID)
}

// ---- 明细 ----

// CreateItem 手动添加明细。
func (s *Service) CreateItem(ctx context.Context, userID, batchID string, req CreateItemRequest) (*ItemView, error) {
	if err := s.requireFinance(ctx, userID); err != nil {
		return nil, err
	}
	b, err := s.repo.GetBatch(ctx, batchID)
	if err != nil {
		return nil, ErrBatchNotFound
	}
	if b.Status == BatchStatusDone {
		return nil, ErrBatchDone
	}
	if err := validateItemRequest(req); err != nil {
		return nil, err
	}
	// 归一化日期(兼容 2026/8/22 → 2026-08-22)
	normDate, err := normalizeDate(req.Date)
	if err != nil {
		return nil, ErrInvalidDate
	}
	req.Date = normDate

	var item *TurnoverItem
	err = s.txRunner(ctx, func(tctx context.Context) error {
		p, err := s.getOrCreateParticipant(tctx, req.Name, req.StudentNo, "")
		if err != nil {
			return err
		}
		item = NewItem(batchID, p.ID, req.Date, req.PayrollAmount, req.TaxAmount, req.TipAmount, req.ShouldReturn, req.Note, userID)
		return s.repo.CreateItem(tctx, item)
	})
	if err != nil {
		return nil, err
	}
	return s.buildItemView(ctx, item)
}

// ImportPreview 解析 xlsx 生成导入预览。
func (s *Service) ImportPreview(ctx context.Context, userID, batchID string, file io.Reader) (string, []ImportRow, []string, error) {
	if err := s.requireFinance(ctx, userID); err != nil {
		return "", nil, nil, err
	}
	if _, err := s.repo.GetBatch(ctx, batchID); err != nil {
		return "", nil, nil, ErrBatchNotFound
	}
	f, err := excelize.OpenReader(file)
	if err != nil {
		return "", nil, nil, ErrInvalidExcel
	}
	defer f.Close()

	rows, err := f.GetRows("Sheet1")
	if err != nil || len(rows) == 0 {
		return "", nil, nil, ErrInvalidExcel
	}
	// 首行表头 → 列索引映射(顺序无关,按列名识别)
	header := rows[0]
	col := map[string]int{}
	for i, h := range header {
		col[strings.TrimSpace(h)] = i
	}
	get := func(row []string, name string) string {
		idx, ok := col[name]
		if !ok || idx >= len(row) {
			return ""
		}
		return strings.TrimSpace(row[idx])
	}

	var valid []ImportRow
	var errs []string
	for i := 1; i < len(rows); i++ {
		if i > MaxImportRows {
			return "", nil, nil, ErrTooManyRows
		}
		r := ImportRow{
			Name:      get(rows[i], "姓名"),
			StudentNo: get(rows[i], "学号"),
			Date:      get(rows[i], "日期"),
			Note:      get(rows[i], "备注"),
		}
		payroll, err1 := parseFen(get(rows[i], "应发"))
		tax, err2 := parseFen(get(rows[i], "扣税"))
		tip, err3 := parseFen(get(rows[i], "辛苦费"))
		r.PayrollAmount, r.TaxAmount, r.TipAmount = payroll, tax, tip
		line := "第" + strconv.Itoa(i+1) + "行"
		switch {
		case r.Name == "" || r.StudentNo == "" || r.Date == "":
			errs = append(errs, line+":姓名/学号/日期缺失")
		case err1 != nil || err2 != nil || err3 != nil:
			errs = append(errs, line+":金额格式错误")
		case r.PayrollAmount <= 0:
			errs = append(errs, line+":应发必须为正数")
		case !validDate(r.Date):
			errs = append(errs, line+":日期格式错误")
		default:
			// 归一化日期(2026/8/22 → 2026-08-22)
			normDate, err := normalizeDate(r.Date)
			if err != nil {
				errs = append(errs, line+":日期格式错误")
			} else {
				r.Date = normDate
				valid = append(valid, r)
			}
		}
	}

	if s.previews == nil {
		return "", nil, nil, ErrPreviewNotFound
	}
	previewID := newID()
	if err := s.previews.SetPreview(ctx, previewID, valid, previewTTL); err != nil {
		return "", nil, nil, err
	}
	return previewID, valid, errs, nil
}

// ConfirmImport 确认导入(按 preview_id 批量生成明细,同事务)。
func (s *Service) ConfirmImport(ctx context.Context, userID, previewID, batchID string) (int, int, error) {
	if err := s.requireFinance(ctx, userID); err != nil {
		return 0, 0, err
	}
	if s.previews == nil {
		return 0, 0, ErrPreviewNotFound
	}
	rows, err := s.previews.GetPreview(ctx, previewID)
	if err != nil {
		return 0, 0, ErrPreviewNotFound
	}
	b, err := s.repo.GetBatch(ctx, batchID)
	if err != nil {
		return 0, 0, ErrBatchNotFound
	}
	if b.Status == BatchStatusDone {
		return 0, 0, ErrBatchDone
	}
	imported := 0
	err = s.txRunner(ctx, func(tctx context.Context) error {
		for _, r := range rows {
			p, err := s.getOrCreateParticipant(tctx, r.Name, r.StudentNo, "")
			if err != nil {
				return err
			}
			item := NewItem(batchID, p.ID, r.Date, r.PayrollAmount, r.TaxAmount, r.TipAmount, 0, r.Note, userID)
			if err := s.repo.CreateItem(tctx, item); err != nil {
				return err
			}
			imported++
		}
		return nil
	})
	if err != nil {
		return 0, 0, err
	}
	return imported, len(rows) - imported, nil
}

// ---- 上交 ----

// Submit 上交登记:生成上交记录 + item.returned 累加 + 资金池自动入账(同事务)。
func (s *Service) Submit(ctx context.Context, userID, itemID string, req SubmitRequest) (*TurnoverSubmission, error) {
	if err := s.requireFinance(ctx, userID); err != nil {
		return nil, err
	}
	if req.Amount <= 0 {
		return nil, ErrInvalidAmount
	}
	if req.Date == "" || !validDate(req.Date) {
		return nil, ErrInvalidDate
	}
	normDate, err := normalizeDate(req.Date)
	if err != nil {
		return nil, ErrInvalidDate
	}
	req.Date = normDate
	item, err := s.repo.GetItem(ctx, itemID)
	if err != nil {
		return nil, ErrItemNotFound
	}
	b, err := s.repo.GetBatch(ctx, item.BatchID)
	if err != nil {
		return nil, ErrBatchNotFound
	}
	if b.Status == BatchStatusDone {
		return nil, ErrBatchDone
	}

	var sub *TurnoverSubmission
	err = s.txRunner(ctx, func(tctx context.Context) error {
		// 事务内重读,防并发超扣
		cur, err := s.repo.GetItem(tctx, itemID)
		if err != nil {
			return ErrItemNotFound
		}
		if req.Amount > cur.Unreturned() {
			return ErrOverSubmit
		}
		cur.Returned += req.Amount
		cur.UpdatedAt = s.now()
		if err := s.repo.UpdateItem(tctx, cur); err != nil {
			return err
		}
		sub = NewSubmission(itemID, req.Amount, req.Date, req.Note, userID)
		if err := s.repo.CreateSubmission(tctx, sub); err != nil {
			return err
		}
		acc, err := s.repo.EnsureDefaultAccount(tctx)
		if err != nil {
			return err
		}
		tx := NewTransaction(acc.ID, TxIncome, req.Amount, CategoryTurnover, "turnover_item", itemID, req.Note, s.now(), userID)
		return s.repo.CreateTransaction(tctx, tx)
	})
	if err != nil {
		return nil, err
	}
	return sub, nil
}

// ---- 资金池 ----

// Ledger 资金池余额 + 流水。
func (s *Service) Ledger(ctx context.Context, userID string) (int64, []*TransactionView, error) {
	if err := s.requireFinance(ctx, userID); err != nil {
		return 0, nil, err
	}
	balance, err := s.repo.Balance(ctx)
	if err != nil {
		return 0, nil, err
	}
	list, err := s.repo.ListTransactions(ctx)
	if err != nil {
		return 0, nil, err
	}
	views, err := s.buildTransactionViews(ctx, list)
	if err != nil {
		return 0, nil, err
	}
	return balance, views, nil
}

// AddIncome 导师补充(收入)。
func (s *Service) AddIncome(ctx context.Context, userID string, req LedgerRequest) (*TransactionView, error) {
	return s.addLedgerTx(ctx, userID, TxIncome, CategoryOther, req)
}

// AddExpense 资金支出。
func (s *Service) AddExpense(ctx context.Context, userID string, req LedgerRequest) (*TransactionView, error) {
	return s.addLedgerTx(ctx, userID, TxExpense, CategoryLabor, req)
}

func (s *Service) addLedgerTx(ctx context.Context, userID, typ, category string, req LedgerRequest) (*TransactionView, error) {
	if err := s.requireFinance(ctx, userID); err != nil {
		return nil, err
	}
	if req.Amount <= 0 {
		return nil, ErrInvalidAmount
	}
	if req.Date == "" || !validDate(req.Date) {
		return nil, ErrInvalidDate
	}
	normDate, err := normalizeDate(req.Date)
	if err != nil {
		return nil, ErrInvalidDate
	}
	var tx *Transaction
	err = s.txRunner(ctx, func(tctx context.Context) error {
		acc, err := s.repo.EnsureDefaultAccount(tctx)
		if err != nil {
			return err
		}
		occurred, err := time.Parse("2006-01-02", normDate)
		if err != nil {
			return ErrInvalidDate
		}
		tx = NewTransaction(acc.ID, typ, req.Amount, category, "none", "", req.Note, occurred, userID)
		return s.repo.CreateTransaction(tctx, tx)
	})
	if err != nil {
		return nil, err
	}
	return s.buildTransactionView(ctx, tx)
}

// ---- 参与同学 ----

// ListParticipants 参与同学库(去重 + 累计)。
func (s *Service) ListParticipants(ctx context.Context, userID string) ([]*ParticipantStat, error) {
	if err := s.requireFinance(ctx, userID); err != nil {
		return nil, err
	}
	list, err := s.repo.ListParticipants(ctx)
	if err != nil {
		return nil, err
	}
	stats := make([]*ParticipantStat, 0, len(list))
	for _, p := range list {
		items, err := s.repo.ListItemsByParticipant(ctx, p.ID)
		if err != nil {
			return nil, err
		}
		st := &ParticipantStat{Participant: p}
		for _, it := range items {
			st.TotalItems++
			st.TotalShouldReturn += it.ShouldReturn
			st.TotalReturned += it.Returned
		}
		stats = append(stats, st)
	}
	return stats, nil
}

// ParticipantBills 某同学历史账单(跨批次)。
func (s *Service) ParticipantBills(ctx context.Context, userID, participantID string) (*Participant, []*BillView, error) {
	if err := s.requireFinance(ctx, userID); err != nil {
		return nil, nil, err
	}
	p, err := s.repo.GetParticipantByID(ctx, participantID)
	if err != nil {
		return nil, nil, ErrParticipantNotFound
	}
	items, err := s.repo.ListItemsByParticipant(ctx, participantID)
	if err != nil {
		return nil, nil, err
	}
	bills := make([]*BillView, 0, len(items))
	for _, it := range items {
		b, err := s.repo.GetBatch(ctx, it.BatchID)
		if err != nil {
			return nil, nil, err
		}
		bills = append(bills, &BillView{
			BatchName: b.Name, Date: deref(it.Date), PayrollAmount: it.PayrollAmount,
			ShouldReturn: it.ShouldReturn, Returned: it.Returned,
			Unreturned: it.Unreturned(), Note: it.Note,
		})
	}
	return p, bills, nil
}

// ---- 模板 ----

// ImportTemplate 生成导入模板 xlsx(表头 + 示例行)。
func (s *Service) ImportTemplate(ctx context.Context, userID string) ([]byte, error) {
	if err := s.requireFinance(ctx, userID); err != nil {
		return nil, err
	}
	f := excelize.NewFile()
	sheet := "Sheet1"
	headers := []string{"姓名", "学号", "日期", "应发", "扣税", "辛苦费", "备注"}
	for i, h := range headers {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		if err := f.SetCellValue(sheet, cell, h); err != nil {
			return nil, err
		}
	}
	// 示例行(日期用 2026/8/22 展示兼容格式)
	sample := []string{"张三", "20230001", "2026/8/22", "2500", "0", "100", "示例行,导入前请删除"}
	for i, v := range sample {
		cell, _ := excelize.CoordinatesToCellName(i+1, 2)
		if err := f.SetCellValue(sheet, cell, v); err != nil {
			return nil, err
		}
	}
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// ---- 内部 helper ----

func (s *Service) getOrCreateParticipant(ctx context.Context, name, studentNo, note string) (*Participant, error) {
	p, err := s.repo.GetParticipant(ctx, name, studentNo)
	if err == nil {
		return p, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return nil, err
	}
	p = NewParticipant(name, studentNo, note)
	if err := s.repo.CreateParticipant(ctx, p); err != nil {
		return nil, err
	}
	return p, nil
}

func (s *Service) buildBatchView(ctx context.Context, b *TurnoverBatch, full bool) (*BatchView, error) {
	items, err := s.repo.ListItems(ctx, b.ID)
	if err != nil {
		return nil, err
	}
	summary := &BatchSummary{ItemCount: int64(len(items))}
	for _, it := range items {
		summary.TotalPayroll += it.PayrollAmount
		summary.TotalShouldReturn += it.ShouldReturn
		summary.TotalReturned += it.Returned
		summary.TotalUnreturned += it.Unreturned()
	}
	view := &BatchView{TurnoverBatch: b, Summary: summary, Items: []*ItemView{}}
	if !full {
		return view, nil
	}
	itemViews, err := s.buildItemViews(ctx, items)
	if err != nil {
		return nil, err
	}
	view.Items = itemViews
	return view, nil
}

func (s *Service) buildItemView(ctx context.Context, it *TurnoverItem) (*ItemView, error) {
	views, err := s.buildItemViews(ctx, []*TurnoverItem{it})
	if err != nil {
		return nil, err
	}
	return views[0], nil
}

func (s *Service) buildItemViews(ctx context.Context, items []*TurnoverItem) ([]*ItemView, error) {
	if len(items) == 0 {
		return []*ItemView{}, nil
	}
	views := make([]*ItemView, 0, len(items))
	for _, it := range items {
		p, err := s.repo.GetParticipantByID(ctx, it.ParticipantID)
		if err != nil {
			return nil, err
		}
		subs, err := s.repo.ListSubmissionsByItem(ctx, it.ID)
		if err != nil {
			return nil, err
		}
		if subs == nil {
			subs = []*TurnoverSubmission{}
		}
		views = append(views, &ItemView{
			TurnoverItem: it, Participant: p, Submissions: subs,
			Unreturned: it.Unreturned(), Status: it.Status(),
		})
	}
	return views, nil
}

func (s *Service) buildTransactionView(ctx context.Context, tx *Transaction) (*TransactionView, error) {
	views, err := s.buildTransactionViews(ctx, []*Transaction{tx})
	if err != nil {
		return nil, err
	}
	return views[0], nil
}

func (s *Service) buildTransactionViews(ctx context.Context, list []*Transaction) ([]*TransactionView, error) {
	if len(list) == 0 {
		return []*TransactionView{}, nil
	}
	opIDs := make([]string, 0, len(list))
	for _, t := range list {
		opIDs = append(opIDs, t.OperatorID)
	}
	users, err := s.users.GetByIDs(ctx, opIDs)
	if err != nil {
		return nil, err
	}
	userByID := make(map[string]*user.User, len(users))
	for _, u := range users {
		userByID[u.ID] = u
	}
	views := make([]*TransactionView, 0, len(list))
	for _, t := range list {
		views = append(views, &TransactionView{Transaction: t, Operator: userByID[t.OperatorID]})
	}
	return views, nil
}

func validateItemRequest(req CreateItemRequest) error {
	if strings.TrimSpace(req.Name) == "" || strings.TrimSpace(req.StudentNo) == "" ||
		strings.TrimSpace(req.Date) == "" || req.PayrollAmount <= 0 {
		return ErrItemFields
	}
	if !validDate(req.Date) {
		return ErrInvalidDate
	}
	if req.TaxAmount < 0 || req.TipAmount < 0 {
		return ErrInvalidAmount
	}
	if req.ShouldReturn < 0 {
		return ErrInvalidAmount
	}
	return nil
}

// normalizeDate 归一化日期:兼容 "2026-08-22" 与 "2026/8/22",统一返回 "2026-08-22"。
func normalizeDate(s string) (string, error) {
	s = strings.TrimSpace(s)
	// 替换分隔符为 "-"(兼容 "/" 与 "-",月份/日期可无前导零)
	clean := strings.ReplaceAll(s, "/", "-")
	t, err := time.Parse("2006-1-2", clean)
	if err != nil {
		return "", err
	}
	return t.Format("2006-01-02"), nil
}

// validDate 校验日期格式(兼容 "/" 与 "-")。
func validDate(s string) bool {
	_, err := normalizeDate(s)
	return err == nil
}

// parseFen 解析金额字符串为"分"("2500"→250000;"2500.5"→250050)。
func parseFen(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	parts := strings.Split(s, ".")
	if len(parts) > 2 {
		return 0, errors.New("bad amount")
	}
	yuan, err := strconv.ParseInt(strings.TrimSpace(parts[0]), 10, 64)
	if err != nil {
		return 0, errors.New("bad amount")
	}
	if yuan < 0 {
		return 0, errors.New("bad amount")
	}
	frac := ""
	if len(parts) == 2 {
		frac = parts[1]
	}
	switch len(frac) {
	case 0:
	case 1:
		frac += "0"
	case 2:
	default:
		return 0, errors.New("bad amount")
	}
	fen := int64(0)
	if frac != "" {
		fen, err = strconv.ParseInt(frac, 10, 64)
		if err != nil {
			return 0, errors.New("bad amount")
		}
	}
	return yuan*100 + fen, nil
}

func newID() string {
	return uuid.NewString()
}

// deref 指针解引用,空指针返回空串。
func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
