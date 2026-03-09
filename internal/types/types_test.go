package types

import "testing"

func TestIsValidStatus(t *testing.T) {
	valid := []ProcessStatus{StatusPending, StatusRunning, StatusCompleted, StatusFailed}
	for _, v := range valid {
		if !IsValidStatus(v) {
			t.Fatalf("expected valid: %s", v)
		}
	}
	if IsValidStatus(ProcessStatus("BAD")) {
		t.Fatalf("expected invalid")
	}
}
