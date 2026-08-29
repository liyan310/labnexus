package tag_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"labnexus/internal/tag"
)

// ---- 内存替身 ----

type memTagRepo struct {
	byID    map[string]*tag.Tag
	byName  map[string]*tag.Tag
	docTags map[string][]string // docID -> tagIDs
}

func newMemTagRepo() *memTagRepo {
	return &memTagRepo{
		byID:    map[string]*tag.Tag{},
		byName:  map[string]*tag.Tag{},
		docTags: map[string][]string{},
	}
}

func (r *memTagRepo) Create(_ context.Context, t *tag.Tag) error {
	if _, dup := r.byName[t.Name]; dup {
		return tag.ErrNotFound // 模拟唯一冲突(service 预检查已拦截)
	}
	r.byID[t.ID] = t
	r.byName[t.Name] = t
	return nil
}

func (r *memTagRepo) GetByID(_ context.Context, id string) (*tag.Tag, error) {
	t, ok := r.byID[id]
	if !ok {
		return nil, tag.ErrNotFound
	}
	return t, nil
}

func (r *memTagRepo) GetByName(_ context.Context, name string) (*tag.Tag, error) {
	t, ok := r.byName[name]
	if !ok {
		return nil, tag.ErrNotFound
	}
	return t, nil
}

func (r *memTagRepo) List(_ context.Context) ([]*tag.Tag, error) {
	var out []*tag.Tag
	for _, t := range r.byID {
		out = append(out, t)
	}
	return out, nil
}

func (r *memTagRepo) ListByDocumentIDs(_ context.Context, docIDs []string) (map[string][]*tag.Tag, error) {
	out := map[string][]*tag.Tag{}
	for _, docID := range docIDs {
		for _, tagID := range r.docTags[docID] {
			if t, ok := r.byID[tagID]; ok {
				out[docID] = append(out[docID], t)
			}
		}
	}
	return out, nil
}

func (r *memTagRepo) ListDocumentIDsByTag(_ context.Context, tagID string) ([]string, error) {
	var ids []string
	for docID, tagIDs := range r.docTags {
		for _, id := range tagIDs {
			if id == tagID {
				ids = append(ids, docID)
			}
		}
	}
	return ids, nil
}

func (r *memTagRepo) ListByResourceIDs(_ context.Context, _ []string) (map[string][]*tag.Tag, error) {
	return nil, nil
}

// ---- 测试 ----

func TestCreateTag_Success(t *testing.T) {
	svc := tag.NewService(newMemTagRepo())
	created, err := svc.CreateTag(context.Background(), "文献-2025", "#ff0000")
	require.NoError(t, err)
	assert.Equal(t, "文献-2025", created.Name)
	assert.Equal(t, "#ff0000", created.Color)
}

func TestCreateTag_DefaultColor(t *testing.T) {
	svc := tag.NewService(newMemTagRepo())
	created, err := svc.CreateTag(context.Background(), "实验方法", "")
	require.NoError(t, err)
	assert.Equal(t, "#3b82f6", created.Color)
}

func TestCreateTag_Duplicate(t *testing.T) {
	svc := tag.NewService(newMemTagRepo())
	_, err := svc.CreateTag(context.Background(), "文献", "")
	require.NoError(t, err)

	_, err = svc.CreateTag(context.Background(), "文献", "")
	assert.ErrorIs(t, err, tag.ErrTagExists)
}

func TestCreateTag_EmptyName(t *testing.T) {
	svc := tag.NewService(newMemTagRepo())
	_, err := svc.CreateTag(context.Background(), "  ", "")
	assert.ErrorIs(t, err, tag.ErrTagNameEmpty)
}

func TestListTags(t *testing.T) {
	svc := tag.NewService(newMemTagRepo())
	_, _ = svc.CreateTag(context.Background(), "A", "")
	_, _ = svc.CreateTag(context.Background(), "B", "")

	tags, err := svc.ListTags(context.Background())
	require.NoError(t, err)
	assert.Len(t, tags, 2)
}
