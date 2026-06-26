package tester

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestBoolPointerHelpers(t *testing.T) {
	truePtr := boolPtr(true)
	falsePtr := boolPtr(false)
	if truePtr == nil || !*truePtr {
		t.Fatalf("boolPtr(true) = %v", truePtr)
	}
	if falsePtr == nil || *falsePtr {
		t.Fatalf("boolPtr(false) = %v", falsePtr)
	}
	if boolValue(nil) || !boolValue(truePtr) || boolValue(falsePtr) {
		t.Fatalf("boolValue results: nil=%v true=%v false=%v", boolValue(nil), boolValue(truePtr), boolValue(falsePtr))
	}
}

func TestRoundErrorChoosesMostUsefulMessage(t *testing.T) {
	if got := roundError(context.Background(), errors.New("transport failed")); got != "transport failed" {
		t.Fatalf("roundError explicit error = %q", got)
	}
	if got := roundError(context.Background(), nil); got != "test failed" {
		t.Fatalf("roundError no signal = %q", got)
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if got := roundError(canceled, nil); got != context.Canceled.Error() {
		t.Fatalf("roundError canceled context = %q", got)
	}
	if got := roundError(canceled, errors.New("body failed")); got != "body failed" {
		t.Fatalf("roundError should prefer explicit non-timeout error = %q", got)
	}

	expired, stop := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer stop()
	if got := roundError(expired, errors.New("transport failed")); got != context.DeadlineExceeded.Error() {
		t.Fatalf("roundError deadline = %q", got)
	}
}
