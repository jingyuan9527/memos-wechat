package dedupe

import (
	"testing"
	"time"
)

func TestMarkSeenDetectsDuplicate(t *testing.T) {
	cache := NewCache(time.Minute)

	if cache.MarkSeen("k1") {
		t.Fatal("首次登记不应判定为重复")
	}
	if !cache.MarkSeen("k1") {
		t.Fatal("重复登记应判定为重复")
	}
	if cache.MarkSeen("k2") {
		t.Fatal("不同 key 之间不应互相影响")
	}
}

func TestMarkSeenForgetsExpiredKey(t *testing.T) {
	cache := NewCache(30 * time.Millisecond)
	cache.MarkSeen("k1")

	time.Sleep(90 * time.Millisecond)

	if cache.MarkSeen("k1") {
		t.Fatal("TTL 过期后应视为新记录")
	}
}
