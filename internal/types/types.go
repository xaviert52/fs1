package types

import "time"

type ProcessStatus string

const (
	StatusPending   ProcessStatus = "PENDING"
	StatusRunning   ProcessStatus = "RUNNING"
	StatusCompleted ProcessStatus = "COMPLETED"
	StatusFailed    ProcessStatus = "FAILED"
)

type StatusUpdate struct {
	UUID      string        `json:"uuid"`
	Status    ProcessStatus `json:"status"`
	Timestamp time.Time     `json:"timestamp"`
}

func IsValidStatus(s ProcessStatus) bool {
	switch s {
	case StatusPending, StatusRunning, StatusCompleted, StatusFailed:
		return true
	default:
		return false
	}
}

func ValidStatuses() []ProcessStatus {
	return []ProcessStatus{
		StatusPending,
		StatusRunning,
		StatusCompleted,
		StatusFailed,
	}
}
