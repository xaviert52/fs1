package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

type Metrics struct {
	UpdatesPublished prometheus.Counter
	UpdatesReceived  prometheus.Counter
	Subscriptions    *prometheus.GaugeVec
	RedisLatency     *prometheus.HistogramVec
	InstanceHealth   prometheus.Gauge
}

func New() *Metrics {
	return &Metrics{
		UpdatesPublished: promauto.NewCounter(prometheus.CounterOpts{
			Name: "subscription_updates_published_total",
			Help: "Total number of status updates published",
		}),
		UpdatesReceived: promauto.NewCounter(prometheus.CounterOpts{
			Name: "subscription_updates_received_total",
			Help: "Total number of status updates received",
		}),
		Subscriptions: promauto.NewGaugeVec(prometheus.GaugeOpts{
			Name: "subscription_active_subscriptions",
			Help: "Number of active subscriptions",
		}, []string{"instance", "uuid"}),
		RedisLatency: promauto.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "subscription_redis_operation_duration_seconds",
			Help:    "Redis operation latency",
			Buckets: prometheus.DefBuckets,
		}, []string{"operation"}),
		InstanceHealth: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "subscription_instance_health",
			Help: "Instance health status (1=healthy, 0=unhealthy)",
		}),
	}
}
