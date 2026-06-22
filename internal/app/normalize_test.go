package app

import (
	"strings"
	"testing"

	"mihomo-st/internal/config"
)

func TestNormalizeDelayCommandAppliesTargetRootConfigPrecedence(t *testing.T) {
	cfg := config.Default()
	cfg.Delay.Timeout = 9000
	cfg.Delay.Headers = map[string]string{"X-Shared": "config", "X-Config": "config"}
	cfg.Delay.FollowRedirect = true
	cfg.Delay.Expected = "200-299"
	cfg.Delay.Rounds = 2
	cfg.Delay.Unified = true

	rootTimeout := 3000
	rootFollow := false
	rootExpected := "*"
	rootRounds := 4
	targetTimeout := 1200
	targetExpected := "204"
	targetUnified := false
	plan, err := NormalizeDelayCommand(DelayCommand{
		Timeout:        &rootTimeout,
		Headers:        map[string]string{"x-shared": "root", "X-Root": "root"},
		FollowRedirect: &rootFollow,
		Expected:       &rootExpected,
		Rounds:         &rootRounds,
		URLs: []DelayTargetCommand{{
			URL:      "https://delay.example/generate_204",
			Timeout:  &targetTimeout,
			Headers:  map[string]string{"X-SHARED": "target", "X-Target": "target"},
			Expected: &targetExpected,
			Unified:  &targetUnified,
		}},
	}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Targets) != 1 {
		t.Fatalf("targets = %+v", plan.Targets)
	}
	target := plan.Targets[0]
	if target.URL != "https://delay.example/generate_204" ||
		target.Timeout != 1200 ||
		target.Expected != "204" ||
		target.Rounds != 4 ||
		target.FollowRedirect ||
		target.Unified {
		t.Fatalf("normalized delay target = %+v", target)
	}
	requireHeaders(t, target.Headers, map[string]string{
		"X-Config": "config",
		"X-Root":   "root",
		"X-Target": "target",
		"X-SHARED": "target",
	})
}

func TestNormalizeDelayCommandUsesRuntimeURLsWhenRequestOmitsTargets(t *testing.T) {
	cfg := config.Default()
	cfg.Delay.Headers = map[string]string{"X-Config": "config"}
	cfg.Delay.URLs = []config.DelayURL{{
		URL:            "https://configured.example/one",
		Timeout:        2500,
		Headers:        map[string]string{"X-URL": "config-url"},
		FollowRedirect: boolPtr(false),
		Expected:       "204",
		Rounds:         5,
		Unified:        boolPtr(false),
	}}
	rootExpected := "*"
	plan, err := NormalizeDelayCommand(DelayCommand{Expected: &rootExpected}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	target := plan.Targets[0]
	if target.URL != "https://configured.example/one" ||
		target.Timeout != 2500 ||
		target.Expected != "204" ||
		target.Rounds != 5 ||
		target.FollowRedirect ||
		target.Unified {
		t.Fatalf("runtime delay URL target = %+v", target)
	}
	requireHeaders(t, target.Headers, map[string]string{"X-Config": "config", "X-URL": "config-url"})
}

func TestNormalizeDownloadCommandAppliesTargetRootConfigPrecedence(t *testing.T) {
	cfg := config.Default()
	cfg.Download.Timeout = 8000
	cfg.Download.Headers = map[string]string{"X-Shared": "config", "X-Config": "config"}
	cfg.Download.FollowRedirect = true
	cfg.Download.Rounds = 2
	cfg.Download.MaxBytes = 4096

	rootTimeout := 2000
	rootFollow := false
	rootRounds := 3
	rootMaxBytes := 2048
	targetMaxBytes := 512
	targetFollow := true
	plan, err := NormalizeDownloadCommand(DownloadCommand{
		Timeout:        &rootTimeout,
		Headers:        map[string]string{"x-shared": "root", "X-Root": "root"},
		FollowRedirect: &rootFollow,
		Rounds:         &rootRounds,
		MaxBytes:       &rootMaxBytes,
		URLs: []DownloadTargetCommand{{
			URL:            "https://download.example/file",
			Headers:        map[string]string{"X-SHARED": "target", "X-Target": "target"},
			FollowRedirect: &targetFollow,
			MaxBytes:       &targetMaxBytes,
		}},
	}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	target := plan.Targets[0]
	if target.URL != "https://download.example/file" ||
		target.Timeout != 2000 ||
		target.Rounds != 3 ||
		target.MaxBytes != 512 ||
		!target.FollowRedirect {
		t.Fatalf("normalized download target = %+v", target)
	}
	requireHeaders(t, target.Headers, map[string]string{
		"X-Config": "config",
		"X-Root":   "root",
		"X-Target": "target",
		"X-SHARED": "target",
	})
}

func TestNormalizeDownloadCommandUsesRuntimeURLsWhenRequestOmitsTargets(t *testing.T) {
	cfg := config.Default()
	cfg.Download.Headers = map[string]string{"X-Config": "config"}
	cfg.Download.URLs = []config.DownloadURL{{
		URL:            "https://configured.example/file",
		Timeout:        3333,
		Headers:        map[string]string{"X-URL": "config-url"},
		FollowRedirect: boolPtr(false),
		Rounds:         6,
		MaxBytes:       700,
	}}
	plan, err := NormalizeDownloadCommand(DownloadCommand{}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	target := plan.Targets[0]
	if target.URL != "https://configured.example/file" ||
		target.Timeout != 3333 ||
		target.Rounds != 6 ||
		target.MaxBytes != 700 ||
		target.FollowRedirect {
		t.Fatalf("runtime download URL target = %+v", target)
	}
	requireHeaders(t, target.Headers, map[string]string{"X-Config": "config", "X-URL": "config-url"})
}

func TestNormalizeCollectionConcurrencyUsesRequestOrRuntimeConfig(t *testing.T) {
	cfg := config.Default()
	cfg.Delay.Concurrency = 7
	cfg.Download.Concurrency = 4
	requestConcurrency := 2

	_, delayFallback, err := NormalizeDelayCollectionCommand(DelayCollectionCommand{}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if delayFallback != 7 {
		t.Fatalf("delay fallback concurrency = %d", delayFallback)
	}
	_, delayRequest, err := NormalizeDelayCollectionCommand(DelayCollectionCommand{Concurrency: &requestConcurrency}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if delayRequest != 2 {
		t.Fatalf("delay request concurrency = %d", delayRequest)
	}

	_, downloadFallback, err := NormalizeDownloadCollectionCommand(DownloadCollectionCommand{}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if downloadFallback != 4 {
		t.Fatalf("download fallback concurrency = %d", downloadFallback)
	}
	_, downloadRequest, err := NormalizeDownloadCollectionCommand(DownloadCollectionCommand{Concurrency: &requestConcurrency}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if downloadRequest != 2 {
		t.Fatalf("download request concurrency = %d", downloadRequest)
	}
}

func TestNormalizeCommandsRejectInvalidPresenceAndURLs(t *testing.T) {
	cfg := config.Default()
	zero := 0
	empty := ""
	cases := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "delay root timeout zero even when target timeout exists",
			err: func() error {
				positive := 1
				_, err := NormalizeDelayCommand(DelayCommand{
					Timeout: &zero,
					URLs:    []DelayTargetCommand{{URL: "https://delay.example", Timeout: &positive}},
				}, cfg)
				return err
			}(),
			want: "timeout",
		},
		{
			name: "delay item expected empty",
			err: func() error {
				_, err := NormalizeDelayCommand(DelayCommand{
					URLs: []DelayTargetCommand{{URL: "https://delay.example", Expected: &empty}},
				}, cfg)
				return err
			}(),
			want: "expected",
		},
		{
			name: "download root max bytes zero even when item max bytes exists",
			err: func() error {
				positive := 1
				_, err := NormalizeDownloadCommand(DownloadCommand{
					MaxBytes: &zero,
					URLs:     []DownloadTargetCommand{{URL: "https://download.example/file", MaxBytes: &positive}},
				}, cfg)
				return err
			}(),
			want: "max-bytes",
		},
		{
			name: "invalid delay URL",
			err: func() error {
				_, err := NormalizeDelayCommand(DelayCommand{URLs: []DelayTargetCommand{{URL: "ftp://delay.example"}}}, cfg)
				return err
			}(),
			want: "http or https",
		},
		{
			name: "delay concurrency zero",
			err: func() error {
				_, _, err := NormalizeDelayCollectionCommand(DelayCollectionCommand{Concurrency: &zero}, cfg)
				return err
			}(),
			want: "concurrency",
		},
		{
			name: "download concurrency zero",
			err: func() error {
				_, _, err := NormalizeDownloadCollectionCommand(DownloadCollectionCommand{Concurrency: &zero}, cfg)
				return err
			}(),
			want: "concurrency",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.err == nil || !strings.Contains(tc.err.Error(), tc.want) {
				t.Fatalf("error = %v, want containing %q", tc.err, tc.want)
			}
		})
	}
}

func requireHeaders(t *testing.T, got, want map[string]string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("headers = %+v, want %+v", got, want)
	}
	for key, wantValue := range want {
		if got[key] != wantValue {
			t.Fatalf("header %s = %q, want %q in %+v", key, got[key], wantValue, got)
		}
	}
}
