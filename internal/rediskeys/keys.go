package rediskeys

import "time"

const (
	KeyStatusPrefix  = "status:"
	KeySubscribers   = "subscribers:"
	KeyInstanceSubs  = "instance:"
	KeyHeartbeat     = "heartbeat:"
	KeyGlobalChannel = "global_updates"
)

const (
	StatusTTL    = 10 * time.Minute
	HeartbeatTTL = 30 * time.Second
	InstanceTTL  = 1 * time.Minute
)
