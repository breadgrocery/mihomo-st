package tester

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	C "github.com/metacubex/mihomo/constant"
	"mihomo-st/internal/httpclient"
)

type downloadTargetMetrics struct {
	min     int
	max     int
	sum     int
	success int
	failed  int
	total   int
}

type downloadRound struct {
	speed int
}

func (t *Tester) Download(ctx context.Context, proxy C.Proxy, plan DownloadPlan) (DownloadMetrics, error) {
	results := make([]downloadTargetMetrics, len(plan.Targets))
	for idx, target := range plan.Targets {
		results[idx] = t.downloadTarget(ctx, proxy, target)
	}

	metrics := DownloadMetrics{}
	for _, result := range results {
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
		metrics.Score = -1
		metrics.Error = ErrAllRoundsFailed.Error()
		return metrics, ErrAllRoundsFailed
	}

	avg := float64(metrics.Avg) / float64(metrics.Success)
	metrics.Avg = roundFloat(avg)
	metrics.Score = roundFloat(avg * (float64(metrics.Success) / float64(metrics.Total)))
	return metrics, nil
}

func (t *Tester) downloadTarget(ctx context.Context, proxy C.Proxy, target DownloadTarget) downloadTargetMetrics {
	result := downloadTargetMetrics{total: target.Rounds}
	for i := 0; i < target.Rounds; i++ {
		roundCtx, cancel := context.WithTimeout(ctx, time.Duration(target.Timeout)*time.Millisecond)
		round, err := downloadOnce(roundCtx, proxy, target.URL, target.MaxBytes, target.Headers, target.FollowRedirect)
		cancel()
		if err != nil || round.speed <= 0 {
			result.failed++
			continue
		}
		result.success++
		result.sum += round.speed
		if result.min == 0 || round.speed < result.min {
			result.min = round.speed
		}
		if round.speed > result.max {
			result.max = round.speed
		}
	}
	return result
}

func downloadOnce(ctx context.Context, proxy C.Proxy, rawURL string, maxBytes int, headers map[string]string, followRedirect bool) (downloadRound, error) {
	client := httpclient.NewProxied(proxy, httpclient.Options{
		DisableRedirects: !followRedirect,
	})
	defer client.CloseIdleConnections()

	request, err := httpclient.NewRequest(ctx, http.MethodGet, rawURL, headers, nil)
	if err != nil {
		return downloadRound{}, err
	}

	start := time.Now()
	response, err := client.Do(request)
	if err != nil {
		return downloadRound{}, err
	}
	defer response.Body.Close()

	if response.StatusCode < 200 || response.StatusCode >= 400 {
		return downloadRound{}, fmt.Errorf("unexpected status: %d", response.StatusCode)
	}

	received, err := io.Copy(io.Discard, io.LimitReader(response.Body, int64(maxBytes)))
	duration := time.Since(start)
	if duration <= 0 {
		duration = time.Millisecond
	}
	if received <= 0 {
		if err != nil {
			return downloadRound{}, err
		}
		return downloadRound{}, nil
	}
	return downloadRound{
		speed: roundFloat(float64(received) / duration.Seconds()),
	}, nil
}
