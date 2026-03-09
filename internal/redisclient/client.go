package redisclient

import (
	"os"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

func NewUniversal() redis.UniversalClient {
	mode := strings.ToLower(strings.TrimSpace(os.Getenv("REDIS_MODE")))
	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		addr = "localhost:6379"
	}
	addrs := []string{addr}
	if mode == "cluster" {
		raw := os.Getenv("REDIS_CLUSTER_ENDPOINTS")
		if raw != "" {
			parts := strings.Split(raw, ",")
			tmp := make([]string, 0, len(parts))
			for _, p := range parts {
				if t := strings.TrimSpace(p); t != "" {
					tmp = append(tmp, t)
				}
			}
			if len(tmp) > 0 {
				addrs = tmp
			}
		}
	}
	return redis.NewUniversalClient(&redis.UniversalOptions{
		Addrs:        addrs,
		Password:     os.Getenv("REDIS_PASSWORD"),
		DB:           0,
		ReadTimeout:  2 * time.Second,
		WriteTimeout: 2 * time.Second,
	})
}
