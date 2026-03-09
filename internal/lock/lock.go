package lock

import (
	"context"
	"fmt"
	"time"

	"github.com/bsm/redislock"
	"github.com/redis/go-redis/v9"
)

func AcquireCleanupLock(client redis.UniversalClient, key string, ttl time.Duration) (*redislock.Lock, error) {
	locker := redislock.New(client)
	ctx := context.Background()
	return locker.Obtain(ctx, fmt.Sprintf("lock:cleanup:%s", key), ttl, nil)
}

func ReleaseCleanupLock(l *redislock.Lock) {
	if l == nil {
		return
	}
	_ = l.Release(context.Background())
}
