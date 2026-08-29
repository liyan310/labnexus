// Package finance 经费管理域。
package finance

import (
	"context"
	"encoding/json"
	"time"

	"labnexus/internal/cache"
)

// previewKey 导入预览缓存 key。
const previewKey = "finance:import:preview:"

// cachePreviewStore 基于 cache.Store 的导入预览存储。
type cachePreviewStore struct {
	store cache.Store
}

// NewCachePreviewStore 用通用键值存储实现导入预览(生产 = Redis)。
func NewCachePreviewStore(store cache.Store) ImportPreviewStore {
	return &cachePreviewStore{store: store}
}

func (c *cachePreviewStore) SetPreview(ctx context.Context, id string, rows []ImportRow, ttl time.Duration) error {
	data, err := json.Marshal(rows)
	if err != nil {
		return err
	}
	return c.store.Set(ctx, previewKey+id, string(data), ttl)
}

func (c *cachePreviewStore) GetPreview(ctx context.Context, id string) ([]ImportRow, error) {
	data, err := c.store.Get(ctx, previewKey+id)
	if err != nil {
		return nil, ErrPreviewNotFound
	}
	var rows []ImportRow
	if err := json.Unmarshal([]byte(data), &rows); err != nil {
		return nil, ErrPreviewNotFound
	}
	return rows, nil
}
