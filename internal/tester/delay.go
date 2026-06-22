package tester

import (
	"context"
	"fmt"
	"io"
	"math"
	"net/http"
	"time"

	"github.com/metacubex/mihomo/common/utils"
	C "github.com/metacubex/mihomo/constant"
	"golang.org/x/sync/errgroup"
	"mihomo-st/internal/httpclient"
)

type delayTargetMetrics struct {
	min     int
	max     int
	sum     int
	success int
	failed  int
	total   int
}

func (t *Tester) Delay(ctx context.Context, proxy C.Proxy, plan DelayPlan) (DelayMetrics, error) {
	results := make([]delayTargetMetrics, len(plan.Targets))
	errs := make([]error, len(plan.Targets))
	var group errgroup.Group
	for idx, target := range plan.Targets {
		idx, target := idx, target
		group.Go(func() error {
			result, err := t.delayTarget(ctx, proxy, target)
			results[idx] = result
			errs[idx] = err
			return nil
		})
	}
	_ = group.Wait()

	metrics := DelayMetrics{}
	for idx, result := range results {
		if errs[idx] != nil {
			return DelayMetrics{}, errs[idx]
		}
		metrics.Success += result.success
		metrics.Failed += result.failed
		metrics.Total += result.total
		if result.success == 0 {
			continue
		}
		if metrics.Min == 0 || result.min < metrics.Min {
			metrics.Min = result.min
		}
		if result.max > metrics.Max {
			metrics.Max = result.max
		}
		metrics.Avg += result.sum
	}

	if metrics.Success == 0 {
		metrics.Min = -1
		metrics.Max = -1
		metrics.Avg = -1
		metrics.Cost = -1
		metrics.Error = ErrAllRoundsFailed.Error()
		return metrics, ErrAllRoundsFailed
	}

	avg := float64(metrics.Avg) / float64(metrics.Success)
	metrics.Avg = roundFloat(avg)
	metrics.Cost = roundFloat(avg / (float64(metrics.Success) / float64(metrics.Total)))
	return metrics, nil
}

func (t *Tester) delayTarget(ctx context.Context, proxy C.Proxy, target DelayTarget) (delayTargetMetrics, error) {
	expected, err := delayExpected(target.Expected)
	if err != nil {
		return delayTargetMetrics{}, fmt.Errorf("invalid expected for %q: %w", target.URL, err)
	}

	result := delayTargetMetrics{total: target.Rounds}
	for i := 0; i < target.Rounds; i++ {
		roundCtx, cancel := context.WithTimeout(ctx, time.Duration(target.Timeout)*time.Millisecond)
		delay, err := delayOnce(roundCtx, proxy, target.URL, expected, target.Unified, target.Headers, target.FollowRedirect)
		cancel()
		if err != nil || delay == 0 {
			result.failed++
			continue
		}

		result.success++
		result.sum += delay
		if result.min == 0 || delay < result.min {
			result.min = delay
		}
		if delay > result.max {
			result.max = delay
		}
	}
	return result, nil
}

func delayExpected(expected string) (utils.IntRanges[uint16], error) {
	if expected == "*" {
		return nil, nil
	}
	return utils.NewUnsignedRanges[uint16](expected)
}

func delayOnce(ctx context.Context, proxy C.Proxy, rawURL string, expected utils.IntRanges[uint16], unified bool, headers map[string]string, followRedirect bool) (int, error) {
	client := httpclient.NewProxied(proxy, httpclient.Options{
		DisableRedirects: !followRedirect,
	})
	defer client.CloseIdleConnections()

	start := time.Now()
	response, err := doDelayRequest(ctx, client, rawURL, headers)
	if err != nil {
		return 0, err
	}

	if unified {
		secondStart := time.Now()
		secondResponse, secondErr := doDelayRequest(ctx, client, rawURL, headers)
		if secondErr == nil {
			response = secondResponse
			start = secondStart
		}
	}

	if expected != nil && !expected.Check(uint16(response.StatusCode)) {
		return 0, fmt.Errorf("unexpected status: %d", response.StatusCode)
	}
	return int(time.Since(start) / time.Millisecond), nil
}

func doDelayRequest(ctx context.Context, client *http.Client, rawURL string, headers map[string]string) (*http.Response, error) {
	request, err := httpclient.NewRequest(ctx, http.MethodHead, rawURL, headers, nil)
	if err != nil {
		return nil, err
	}

	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, response.Body)
	return response, nil
}

func roundFloat(value float64) int {
	return int(math.Round(value))
}
