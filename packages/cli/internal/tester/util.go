package tester

import (
	"context"
	"errors"
)

var ErrAllRoundsFailed = errors.New("all rounds failed")

func boolPtr(value bool) *bool {
	return &value
}

func boolValue(value *bool) bool {
	return value != nil && *value
}

func roundError(ctx context.Context, err error) string {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return ctx.Err().Error()
	}
	if err != nil {
		return err.Error()
	}
	if ctx.Err() != nil {
		return ctx.Err().Error()
	}
	return "test failed"
}
