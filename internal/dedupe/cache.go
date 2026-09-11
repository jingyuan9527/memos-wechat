// Package dedupe 提供带 TTL 的内存去重能力。
package dedupe

import (
	"sync"
	"time"
)

// Cache 用于吞掉微信在回调超时后的重复推送，避免同一条消息被写入多次 Memos。
type Cache struct {
	mu     sync.Mutex
	ttl    time.Duration
	seenAt map[string]time.Time
}

// NewCache 创建去重缓存，ttl 为单条记录的最长保留时间。
func NewCache(ttl time.Duration) *Cache {
	return &Cache{
		ttl:    ttl,
		seenAt: make(map[string]time.Time),
	}
}

// MarkSeen 原子地登记 key，并返回该 key 是否已经出现过。
// 去重判定必须同步完成：微信重试可能在下游 Memos 调用返回之前就再次到达。
func (c *Cache) MarkSeen(key string) (duplicated bool) {
	now := time.Now()

	c.mu.Lock()
	defer c.mu.Unlock()

	// 借每次写入顺带清理过期项，省去额外的后台清理协程。
	for k, seenAt := range c.seenAt {
		if now.Sub(seenAt) >= c.ttl {
			delete(c.seenAt, k)
		}
	}

	if _, ok := c.seenAt[key]; ok {
		return true
	}
	c.seenAt[key] = now
	return false
}
