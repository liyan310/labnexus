// Package finance 经费管理域:F10 周转批次/明细/上交/资金池/参与同学。
// 核心模型:收入 + 支出 → 维护总金额。
// 依据规格:docs/specs/funds-management.md;契约:api-contract.md §F10。
package finance

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"labnexus/internal/database"
)

// ErrNotFound 记录不存在
var ErrNotFound = errors.New("finance: not found")

// 批次状态
const (
	BatchStatusActive = "active"
	BatchStatusDone   = "done"
)

// 明细状态(由 returned 推导)
const (
	ItemStatusPending = "pending" // 未交
	ItemStatusPartial = "partial" // 部分交
	ItemStatusDone    = "done"    // 已交清
)

// 流水类型与类别
const (
	TxIncome  = "income"
	TxExpense = "expense"

	CategoryTurnover = "turnover" // 上交回笼
	CategoryLabor    = "labor"    // 劳务发放(支出)
	CategoryOther    = "other"    // 其他
)

// TurnoverBatch 周转批次(schema.sql: turnover_batches)
type TurnoverBatch struct {
	ID        string    `gorm:"type:uuid;primaryKey" json:"id"`
	Name      string    `gorm:"size:100" json:"name"`
	Status    string    `gorm:"size:20;default:active" json:"status"`
	Note      string    `gorm:"type:text" json:"note"`
	CreatedBy string    `gorm:"type:uuid" json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// NewBatch 构造批次。
func NewBatch(name, note, createdBy string) *TurnoverBatch {
	now := time.Now()
	return &TurnoverBatch{
		ID: uuid.NewString(), Name: name, Status: BatchStatusActive,
		Note: note, CreatedBy: createdBy, CreatedAt: now, UpdatedAt: now,
	}
}

// Participant 参与同学(schema.sql: participants;name+student_no 唯一)
type Participant struct {
	ID        string    `gorm:"type:uuid;primaryKey" json:"id"`
	Name      string    `gorm:"size:50" json:"name"`
	StudentNo string    `gorm:"size:50" json:"student_no"`
	Note      string    `gorm:"type:text" json:"note"`
	CreatedAt time.Time `json:"created_at"`
}

// NewParticipant 构造参与同学。
func NewParticipant(name, studentNo, note string) *Participant {
	return &Participant{
		ID: uuid.NewString(), Name: name, StudentNo: studentNo,
		Note: note, CreatedAt: time.Now(),
	}
}

// TurnoverItem 发放明细(schema.sql: turnover_items)
type TurnoverItem struct {
	ID            string    `gorm:"type:uuid;primaryKey" json:"id"`
	BatchID       string    `gorm:"type:uuid;index" json:"batch_id"`
	ParticipantID string    `gorm:"type:uuid;index" json:"participant_id"`
	Date          string    `gorm:"type:date" json:"date"`
	PayrollAmount int64     `json:"payroll_amount"` // 应发(分)
	TaxAmount     int64     `json:"tax_amount"`     // 扣税(分)
	TipAmount     int64     `json:"tip_amount"`     // 辛苦费(分)
	ShouldReturn  int64     `json:"should_return"`  // 应交(分)
	Returned      int64     `json:"returned"`       // 已交(分)
	Note          string    `gorm:"type:text" json:"note"`
	CreatedBy     string    `gorm:"type:uuid" json:"created_by"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// NewItem 构造明细(应交默认 = 应发−扣税−辛苦费,可覆盖)。
func NewItem(batchID, participantID, date string, payroll, tax, tip, shouldReturn int64, note, createdBy string) *TurnoverItem {
	if shouldReturn <= 0 {
		shouldReturn = payroll - tax - tip
	}
	now := time.Now()
	return &TurnoverItem{
		ID: uuid.NewString(), BatchID: batchID, ParticipantID: participantID,
		Date: date, PayrollAmount: payroll, TaxAmount: tax, TipAmount: tip,
		ShouldReturn: shouldReturn, Note: note, CreatedBy: createdBy,
		CreatedAt: now, UpdatedAt: now,
	}
}

// Unreturned 未交额(计算字段)。
func (i *TurnoverItem) Unreturned() int64 {
	if d := i.ShouldReturn - i.Returned; d > 0 {
		return d
	}
	return 0
}

// Status 明细状态(由 returned 推导)。
func (i *TurnoverItem) Status() string {
	switch {
	case i.Returned <= 0:
		return ItemStatusPending
	case i.Returned >= i.ShouldReturn:
		return ItemStatusDone
	default:
		return ItemStatusPartial
	}
}

// TurnoverSubmission 上交记录(schema.sql: turnover_submissions)
type TurnoverSubmission struct {
	ID         string    `gorm:"type:uuid;primaryKey" json:"id"`
	ItemID     string    `gorm:"type:uuid;index" json:"item_id"`
	Amount     int64     `json:"amount"`
	Date       string    `gorm:"type:date" json:"date"`
	Note       string    `gorm:"type:text" json:"note"`
	OperatorID string    `gorm:"type:uuid" json:"operator_id"`
	CreatedAt  time.Time `json:"created_at"`
}

// NewSubmission 构造上交记录。
func NewSubmission(itemID string, amount int64, date, note, operatorID string) *TurnoverSubmission {
	return &TurnoverSubmission{
		ID: uuid.NewString(), ItemID: itemID, Amount: amount,
		Date: date, Note: note, OperatorID: operatorID, CreatedAt: time.Now(),
	}
}

// Account 资金账户(schema.sql: accounts;v1 单账户)
type Account struct {
	ID        string    `gorm:"type:uuid;primaryKey" json:"id"`
	Name      string    `gorm:"size:100" json:"name"`
	AccountNo string    `gorm:"size:50" json:"account_no"`
	Note      string    `gorm:"type:text" json:"note"`
	CreatedAt time.Time `json:"created_at"`
}

// NewAccount 构造账户。
func NewAccount(name, accountNo, note string) *Account {
	return &Account{
		ID: uuid.NewString(), Name: name, AccountNo: accountNo,
		Note: note, CreatedAt: time.Now(),
	}
}

// Transaction 资金流水(schema.sql: transactions;余额=Σincome−Σexpense)
type Transaction struct {
	ID          string    `gorm:"type:uuid;primaryKey" json:"id"`
	AccountID   string    `gorm:"type:uuid;index" json:"account_id"`
	Type        string    `gorm:"size:10" json:"type"`
	Amount      int64     `json:"amount"`
	Category    string    `gorm:"size:30;default:other" json:"category"`
	RelatedType string    `gorm:"size:20;default:none" json:"related_type"`
	RelatedID   *string   `gorm:"type:uuid" json:"related_id,omitempty"`
	Note        string    `gorm:"type:text" json:"note"`
	OccurredAt  time.Time `json:"occurred_at"`
	OperatorID  string    `gorm:"type:uuid" json:"operator_id"`
	CreatedAt   time.Time `json:"created_at"`
}

// NewTransaction 构造流水。
func NewTransaction(accountID, typ string, amount int64, category, relatedType, relatedID, note string, occurredAt time.Time, operatorID string) *Transaction {
	var relID *string
	if relatedID != "" {
		relID = &relatedID
	}
	return &Transaction{
		ID: uuid.NewString(), AccountID: accountID, Type: typ, Amount: amount,
		Category: category, RelatedType: relatedType, RelatedID: relID,
		Note: note, OccurredAt: occurredAt, OperatorID: operatorID, CreatedAt: time.Now(),
	}
}

// Repository 经费域数据访问接口
type Repository interface {
	// 批次
	CreateBatch(ctx context.Context, b *TurnoverBatch) error
	GetBatch(ctx context.Context, id string) (*TurnoverBatch, error)
	UpdateBatch(ctx context.Context, b *TurnoverBatch) error
	DeleteBatch(ctx context.Context, id string) error
	ListBatches(ctx context.Context) ([]*TurnoverBatch, error)

	// 参与同学
	GetParticipant(ctx context.Context, name, studentNo string) (*Participant, error)
	CreateParticipant(ctx context.Context, p *Participant) error
	ListParticipants(ctx context.Context) ([]*Participant, error)
	GetParticipantByID(ctx context.Context, id string) (*Participant, error)

	// 明细
	CreateItem(ctx context.Context, it *TurnoverItem) error
	GetItem(ctx context.Context, id string) (*TurnoverItem, error)
	UpdateItem(ctx context.Context, it *TurnoverItem) error
	DeleteItemsByBatch(ctx context.Context, batchID string) error
	ListItems(ctx context.Context, batchID string) ([]*TurnoverItem, error)
	ListItemsByParticipant(ctx context.Context, participantID string) ([]*TurnoverItem, error)

	// 上交
	CreateSubmission(ctx context.Context, s *TurnoverSubmission) error
	ListSubmissionsByItem(ctx context.Context, itemID string) ([]*TurnoverSubmission, error)
	DeleteSubmissionsByItem(ctx context.Context, itemID string) error

	// 资金池
	EnsureDefaultAccount(ctx context.Context) (*Account, error)
	CreateTransaction(ctx context.Context, t *Transaction) error
	ListTransactions(ctx context.Context) ([]*Transaction, error)
	Balance(ctx context.Context) (int64, error)
}

// GormRepository 经费域 GORM 实现
type GormRepository struct {
	db *gorm.DB
}

func NewGormRepository(db *gorm.DB) *GormRepository {
	return &GormRepository{db: db}
}

func (r *GormRepository) CreateBatch(ctx context.Context, b *TurnoverBatch) error {
	return database.TxFromContext(ctx, r.db).WithContext(ctx).Create(b).Error
}

func (r *GormRepository) GetBatch(ctx context.Context, id string) (*TurnoverBatch, error) {
	var b TurnoverBatch
	err := database.TxFromContext(ctx, r.db).WithContext(ctx).First(&b, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &b, nil
}

func (r *GormRepository) UpdateBatch(ctx context.Context, b *TurnoverBatch) error {
	return database.TxFromContext(ctx, r.db).WithContext(ctx).Save(b).Error
}

func (r *GormRepository) DeleteBatch(ctx context.Context, id string) error {
	// 级联:删除明细与上交记录(participant 保留)
	db := database.TxFromContext(ctx, r.db).WithContext(ctx)
	var itemIDs []string
	if err := db.Model(&TurnoverItem{}).Where("batch_id = ?", id).Pluck("id", &itemIDs).Error; err != nil {
		return err
	}
	if len(itemIDs) > 0 {
		if err := db.Where("item_id IN ?", itemIDs).Delete(&TurnoverSubmission{}).Error; err != nil {
			return err
		}
		if err := db.Where("batch_id = ?", id).Delete(&TurnoverItem{}).Error; err != nil {
			return err
		}
	}
	return db.Delete(&TurnoverBatch{}, "id = ?", id).Error
}

func (r *GormRepository) ListBatches(ctx context.Context) ([]*TurnoverBatch, error) {
	var list []*TurnoverBatch
	err := database.TxFromContext(ctx, r.db).WithContext(ctx).
		Order("created_at DESC").Find(&list).Error
	return list, err
}

func (r *GormRepository) GetParticipant(ctx context.Context, name, studentNo string) (*Participant, error) {
	var p Participant
	err := database.TxFromContext(ctx, r.db).WithContext(ctx).
		First(&p, "name = ? AND student_no = ?", name, studentNo).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (r *GormRepository) CreateParticipant(ctx context.Context, p *Participant) error {
	return database.TxFromContext(ctx, r.db).WithContext(ctx).Create(p).Error
}

func (r *GormRepository) ListParticipants(ctx context.Context) ([]*Participant, error) {
	var list []*Participant
	err := database.TxFromContext(ctx, r.db).WithContext(ctx).
		Order("created_at ASC").Find(&list).Error
	return list, err
}

func (r *GormRepository) GetParticipantByID(ctx context.Context, id string) (*Participant, error) {
	var p Participant
	err := database.TxFromContext(ctx, r.db).WithContext(ctx).First(&p, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (r *GormRepository) CreateItem(ctx context.Context, it *TurnoverItem) error {
	return database.TxFromContext(ctx, r.db).WithContext(ctx).Create(it).Error
}

func (r *GormRepository) GetItem(ctx context.Context, id string) (*TurnoverItem, error) {
	var it TurnoverItem
	err := database.TxFromContext(ctx, r.db).WithContext(ctx).First(&it, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &it, nil
}

func (r *GormRepository) UpdateItem(ctx context.Context, it *TurnoverItem) error {
	return database.TxFromContext(ctx, r.db).WithContext(ctx).Save(it).Error
}

func (r *GormRepository) DeleteItemsByBatch(ctx context.Context, batchID string) error {
	return database.TxFromContext(ctx, r.db).WithContext(ctx).
		Where("batch_id = ?", batchID).Delete(&TurnoverItem{}).Error
}

func (r *GormRepository) ListItems(ctx context.Context, batchID string) ([]*TurnoverItem, error) {
	var list []*TurnoverItem
	err := database.TxFromContext(ctx, r.db).WithContext(ctx).
		Where("batch_id = ?", batchID).Order("created_at ASC").Find(&list).Error
	return list, err
}

func (r *GormRepository) ListItemsByParticipant(ctx context.Context, participantID string) ([]*TurnoverItem, error) {
	var list []*TurnoverItem
	err := database.TxFromContext(ctx, r.db).WithContext(ctx).
		Where("participant_id = ?", participantID).Order("date ASC").Find(&list).Error
	return list, err
}

func (r *GormRepository) CreateSubmission(ctx context.Context, s *TurnoverSubmission) error {
	return database.TxFromContext(ctx, r.db).WithContext(ctx).Create(s).Error
}

func (r *GormRepository) ListSubmissionsByItem(ctx context.Context, itemID string) ([]*TurnoverSubmission, error) {
	var list []*TurnoverSubmission
	err := database.TxFromContext(ctx, r.db).WithContext(ctx).
		Where("item_id = ?", itemID).Order("created_at ASC").Find(&list).Error
	return list, err
}

func (r *GormRepository) DeleteSubmissionsByItem(ctx context.Context, itemID string) error {
	return database.TxFromContext(ctx, r.db).WithContext(ctx).
		Where("item_id = ?", itemID).Delete(&TurnoverSubmission{}).Error
}

func (r *GormRepository) EnsureDefaultAccount(ctx context.Context) (*Account, error) {
	db := database.TxFromContext(ctx, r.db).WithContext(ctx)
	var acc Account
	if err := db.First(&acc).Error; err == nil {
		return &acc, nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	acc = *NewAccount("课题组经费账户", "", "")
	if err := db.Create(&acc).Error; err != nil {
		return nil, err
	}
	return &acc, nil
}

func (r *GormRepository) CreateTransaction(ctx context.Context, t *Transaction) error {
	return database.TxFromContext(ctx, r.db).WithContext(ctx).Create(t).Error
}

func (r *GormRepository) ListTransactions(ctx context.Context) ([]*Transaction, error) {
	var list []*Transaction
	err := database.TxFromContext(ctx, r.db).WithContext(ctx).
		Order("occurred_at DESC, created_at DESC").Find(&list).Error
	return list, err
}

func (r *GormRepository) Balance(ctx context.Context) (int64, error) {
	db := database.TxFromContext(ctx, r.db).WithContext(ctx)
	var income, expense int64
	if err := db.Model(&Transaction{}).Where("type = ?", TxIncome).Select("COALESCE(SUM(amount),0)").Scan(&income).Error; err != nil {
		return 0, err
	}
	if err := db.Model(&Transaction{}).Where("type = ?", TxExpense).Select("COALESCE(SUM(amount),0)").Scan(&expense).Error; err != nil {
		return 0, err
	}
	return income - expense, nil
}
