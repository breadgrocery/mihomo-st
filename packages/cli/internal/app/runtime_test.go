package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/metacubex/mihomo/adapter/outbound"
	"github.com/metacubex/mihomo/common/utils"
	C "github.com/metacubex/mihomo/constant"
	"mihomo-st/internal/config"
	"mihomo-st/internal/httpclient"
	"mihomo-st/internal/proxyconfig"
)

func TestRuntimeResultTypesStayOutsideHTTPDTOBoundary(t *testing.T) {
	for _, value := range []any{
		ProxyImportResult{},
		ProxyListResult{},
		DelayResult{},
		DelayCollectionResult{},
		DownloadResult{},
		DownloadCollectionResult{},
	} {
		typ := reflect.TypeOf(value)
		for i := 0; i < typ.NumField(); i++ {
			if tag := typ.Field(i).Tag.Get("json"); tag != "" {
				t.Fatalf("%s.%s has JSON tag %q", typ.Name(), typ.Field(i).Name, tag)
			}
		}
	}

	var _ int = ProxyListResult{}.Version
	var _ int = DelayResult{}.Version
	var _ *int = DownloadCommand{}.MaxBytes
	var _ *int = DownloadTargetCommand{}.MaxBytes
}

func TestRuntimeConfigPatchIsRuntimeOnlyAndLeavesProxySnapshotAlone(t *testing.T) {
	rt := makeRuntime(t, config.Default(), directRecords("initial"))
	before, err := rt.ListProxies()
	if err != nil {
		t.Fatal(err)
	}

	next, err := rt.PatchConfig(map[string]json.RawMessage{
		"delay": json.RawMessage(`{"rounds":5}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if next.Delay.Rounds != 5 {
		t.Fatalf("patched config = %+v", next)
	}
	after, err := rt.ListProxies()
	if err != nil {
		t.Fatal(err)
	}
	if after.Version != before.Version || len(after.Proxies) != len(before.Proxies) {
		t.Fatalf("config patch changed proxy snapshot: before=%+v after=%+v", before, after)
	}
}

func TestRuntimeConfigPatchUpdatesSkipCertVerifyForFutureHTTPClients(t *testing.T) {
	httpclient.SetSkipCertVerify(false)
	t.Cleanup(func() { httpclient.SetSkipCertVerify(false) })

	remote := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(proxyYAML("tls", "tls.example")))
	}))
	defer remote.Close()

	rt := makeRuntime(t, config.Default(), nil)
	_, err := rt.ImportProxies(context.Background(), ProxyImportCommand{
		Type:    SourceRemote,
		Payload: remote.URL,
		Timeout: intPtr(1000),
	})
	if err == nil {
		t.Fatal("self-signed remote import succeeded before skip-cert-verify was enabled")
	}

	if _, err := rt.PatchConfig(map[string]json.RawMessage{
		"skip-cert-verify": json.RawMessage(`true`),
	}); err != nil {
		t.Fatal(err)
	}
	imported, err := rt.ImportProxies(context.Background(), ProxyImportCommand{
		Type:    SourceRemote,
		Payload: remote.URL,
		Timeout: intPtr(1000),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(imported.Proxies) != 1 || imported.Proxies[0].Name != "tls" {
		t.Fatalf("skip-cert-verify import = %+v", imported)
	}

	if _, err := rt.PatchConfig(map[string]json.RawMessage{
		"skip-cert-verify": json.RawMessage(`false`),
	}); err != nil {
		t.Fatal(err)
	}
	_, err = rt.ImportProxies(context.Background(), ProxyImportCommand{
		Type:    SourceRemote,
		Payload: remote.URL,
		Timeout: intPtr(1000),
	})
	if err == nil {
		t.Fatal("self-signed remote import succeeded after skip-cert-verify was disabled")
	}
}

func TestRuntimeImportTextReplaceAppendEmptyAndFailureBoundaries(t *testing.T) {
	rt := makeRuntime(t, config.Default(), nil)

	replaced, err := rt.ImportProxies(context.Background(), ProxyImportCommand{
		Type:    SourceText,
		Payload: proxyYAML("alpha", "alpha.example"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if replaced.Version != 1 || len(replaced.Proxies) != 1 || replaced.Proxies[0].Name != "alpha" || len(replaced.Warnings) != 0 {
		t.Fatalf("replace import = %+v", replaced)
	}

	appended, err := rt.ImportProxies(context.Background(), ProxyImportCommand{
		Type:    SourceText,
		Payload: proxyYAML("bravo", "bravo.example"),
		Mode:    ImportAppend,
	})
	if err != nil {
		t.Fatal(err)
	}
	if appended.Version != 2 || len(appended.Proxies) != 2 {
		t.Fatalf("append import = %+v", appended)
	}

	empty, err := rt.ImportProxies(context.Background(), ProxyImportCommand{Type: SourceText, Payload: "proxies: []\n"})
	if err != nil {
		t.Fatal(err)
	}
	if empty.Version != 3 || len(empty.Proxies) != 0 {
		t.Fatalf("empty import = %+v", empty)
	}

	if _, err := rt.ImportProxies(context.Background(), ProxyImportCommand{Type: SourceText, Payload: "proxies:\n - name: ["}); err == nil {
		t.Fatal("malformed YAML import returned nil error")
	}
	list, err := rt.ListProxies()
	if err != nil {
		t.Fatal(err)
	}
	if list.Version != 3 || len(list.Proxies) != 0 {
		t.Fatalf("failed import changed snapshot: %+v", list)
	}

	if _, err := rt.ImportProxies(context.Background(), ProxyImportCommand{Type: SourceText}); !errors.Is(err, ErrProxySourceRequired) {
		t.Fatalf("missing payload error = %v", err)
	}
	if _, err := rt.ImportProxies(context.Background(), ProxyImportCommand{Type: "file", Payload: "x"}); err == nil {
		t.Fatal("invalid source type returned nil error")
	}
	if _, err := rt.ImportProxies(context.Background(), ProxyImportCommand{Type: SourceText, Payload: "proxies: []", Mode: "merge"}); err == nil {
		t.Fatal("invalid import mode returned nil error")
	}
}

func TestRuntimeImportSerializesConcurrentRequestsAndAppendUsesLatestSnapshot(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		_, _ = w.Write([]byte(proxyYAML("remote", "remote.example")))
	}))
	defer remote.Close()

	rt := makeRuntime(t, config.Default(), nil)
	firstDone := make(chan ProxyImportResult, 1)
	firstErr := make(chan error, 1)
	go func() {
		result, err := rt.ImportProxies(context.Background(), ProxyImportCommand{Type: SourceRemote, Payload: remote.URL})
		firstDone <- result
		firstErr <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("first remote import did not start")
	}

	secondDone := make(chan ProxyImportResult, 1)
	secondErr := make(chan error, 1)
	go func() {
		result, err := rt.ImportProxies(context.Background(), ProxyImportCommand{
			Type:    SourceText,
			Payload: proxyYAML("local", "local.example"),
			Mode:    ImportAppend,
		})
		secondDone <- result
		secondErr <- err
	}()
	select {
	case result := <-secondDone:
		t.Fatalf("second import completed before first released: %+v", result)
	case <-time.After(25 * time.Millisecond):
	}

	close(release)
	if err := <-firstErr; err != nil {
		t.Fatal(err)
	}
	if first := <-firstDone; first.Version != 1 || len(first.Proxies) != 1 {
		t.Fatalf("first import = %+v", first)
	}
	if err := <-secondErr; err != nil {
		t.Fatal(err)
	}
	if second := <-secondDone; second.Version != 2 || len(second.Proxies) != 2 {
		t.Fatalf("second import = %+v", second)
	}
}

func TestRuntimeLocalAndRemoteImportOptions(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "proxies.yaml"), []byte(proxyYAML("local", "local.example")), 0o600); err != nil {
		t.Fatal(err)
	}
	rt := makeRuntime(t, config.Default(), nil)
	rt.executableDir = dir

	local, err := rt.ImportProxies(context.Background(), ProxyImportCommand{
		Type:    SourceLocal,
		Payload: "proxies.yaml",
		Headers: map[string]string{"Authorization": "ignored"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(local.Proxies) != 1 || local.Proxies[0].Name != "local" {
		t.Fatalf("local import = %+v", local)
	}

	var sawAuth bool
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawAuth = r.Header.Get("Authorization") == "Bearer token"
		_, _ = w.Write([]byte(proxyYAML("remote", "remote.example")))
	}))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer redirect.Close()
	remote, err := rt.ImportProxies(context.Background(), ProxyImportCommand{
		Type:    SourceRemote,
		Payload: redirect.URL,
		Headers: map[string]string{"Authorization": "Bearer token"},
		Timeout: intPtr(1000),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(remote.Proxies) != 1 || remote.Proxies[0].Name != "remote" || !sawAuth {
		t.Fatalf("remote import = %+v sawAuth=%v", remote, sawAuth)
	}

	_, err = rt.ImportProxies(context.Background(), ProxyImportCommand{
		Type:           SourceRemote,
		Payload:        redirect.URL,
		Timeout:        intPtr(1000),
		FollowRedirect: boolPtr(false),
	})
	if !errors.Is(err, proxyconfig.ErrRemoteStatus) {
		t.Fatalf("disabled redirect error = %v", err)
	}

	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
		_, _ = w.Write([]byte(proxyYAML("slow", "slow.example")))
	}))
	defer slow.Close()
	if _, err := rt.ImportProxies(context.Background(), ProxyImportCommand{Type: SourceRemote, Payload: slow.URL, Timeout: intPtr(1)}); !errors.Is(err, proxyconfig.ErrRemoteTimeout) {
		t.Fatalf("remote timeout error = %v", err)
	}
}

func TestRuntimeImportExpansionUsesRuntimeOrOverrideProxyServerConfig(t *testing.T) {
	originalExpand := expandServerDomains
	t.Cleanup(func() { expandServerDomains = originalExpand })
	var seen []proxyconfig.ServerExpandOptions
	expandedRecord := directRecords("expanded")[0]
	expandServerDomains = func(ctx context.Context, result proxyconfig.Result, opts proxyconfig.ServerExpandOptions) proxyconfig.Result {
		seen = append(seen, opts)
		return proxyconfig.Result{Records: append(append([]*proxyconfig.Record(nil), result.Records...), expandedRecord)}
	}

	cfg := config.Default()
	cfg.ProxyServer.Expand = true
	cfg.ProxyServer.Nameservers = []string{"9.9.9.9"}
	cfg.ProxyServer.Timeout = 2500
	rt := makeRuntime(t, cfg, nil)

	runtimeConfig, err := rt.ImportProxies(context.Background(), ProxyImportCommand{Type: SourceText, Payload: proxyYAML("base", "base.example")})
	if err != nil {
		t.Fatal(err)
	}
	if len(runtimeConfig.Proxies) != 2 || len(seen) != 1 || !seen[0].Enabled || seen[0].Nameservers[0] != "9.9.9.9" || seen[0].Timeout != 2500*time.Millisecond {
		t.Fatalf("runtime expansion result=%+v opts=%+v", runtimeConfig, seen)
	}
	listed, err := rt.ListProxies()
	if err != nil {
		t.Fatal(err)
	}
	if listed.Version != runtimeConfig.Version || len(listed.Proxies) != 2 || len(seen) != 1 {
		t.Fatalf("list mutated expansion state: listed=%+v seen=%+v", listed, seen)
	}

	seen = nil
	override, err := rt.ImportProxies(context.Background(), ProxyImportCommand{
		Type:    SourceText,
		Payload: proxyYAML("override", "override.example"),
		ProxyServer: &ProxyServerOverride{
			Expand: boolPtr(true),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(override.Proxies) != 2 ||
		len(seen) != 1 ||
		seen[0].Nameservers[0] != config.DefaultProxyServerNameserver ||
		seen[0].Timeout != time.Duration(config.DefaultProxyServerTimeout)*time.Millisecond {
		t.Fatalf("override expansion result=%+v opts=%+v", override, seen)
	}

	if _, err := rt.ImportProxies(context.Background(), ProxyImportCommand{
		Type:        SourceText,
		Payload:     proxyYAML("bad", "bad.example"),
		ProxyServer: &ProxyServerOverride{Expand: boolPtr(true), Timeout: intPtr(0)},
	}); err == nil {
		t.Fatal("invalid proxy-server override returned nil error")
	}
}

func TestRuntimeDelayDownloadSingleCollectionAndEmptyMissingCases(t *testing.T) {
	delayURL, downloadURL, cleanup := testEndpoints(t)
	defer cleanup()
	rt := makeRuntime(t, config.Default(), directRecords("one"))

	delay, err := rt.Delay(context.Background(), "one", DelayCommand{
		URLs:     []DelayTargetCommand{{URL: delayURL}},
		Timeout:  intPtr(1000),
		Expected: stringPtr("204"),
		Rounds:   intPtr(1),
		Unified:  boolPtr(false),
	})
	if err != nil {
		t.Fatal(err)
	}
	if delay.Version != 0 || delay.Digest != "one" || delay.Metrics.Success != 1 {
		t.Fatalf("single delay = %+v", delay)
	}

	download, err := rt.Download(context.Background(), "one", DownloadCommand{
		URLs:     []DownloadTargetCommand{{URL: downloadURL}},
		Timeout:  intPtr(1000),
		Rounds:   intPtr(1),
		MaxBytes: intPtr(4),
	})
	if err != nil {
		t.Fatal(err)
	}
	if download.Version != 0 || download.Digest != "one" || download.Metrics.Success != 1 {
		t.Fatalf("single download = %+v", download)
	}

	delayAll, err := rt.DelayAll(context.Background(), DelayCollectionCommand{DelayCommand: DelayCommand{
		URLs:     []DelayTargetCommand{{URL: delayURL}},
		Timeout:  intPtr(1000),
		Expected: stringPtr("*"),
		Rounds:   intPtr(1),
		Unified:  boolPtr(false),
	}})
	if err != nil {
		t.Fatal(err)
	}
	if delayAll.Version != 0 || len(delayAll.Results) != 1 || delayAll.Results[0].Digest != "one" {
		t.Fatalf("collection delay = %+v", delayAll)
	}

	downloadAll, err := rt.DownloadAll(context.Background(), DownloadCollectionCommand{DownloadCommand: DownloadCommand{
		URLs:     []DownloadTargetCommand{{URL: downloadURL}},
		Timeout:  intPtr(1000),
		Rounds:   intPtr(1),
		MaxBytes: intPtr(4),
	}})
	if err != nil {
		t.Fatal(err)
	}
	if downloadAll.Version != 0 || len(downloadAll.Results) != 1 || downloadAll.Results[0].Digest != "one" {
		t.Fatalf("collection download = %+v", downloadAll)
	}

	empty := makeRuntime(t, config.Default(), nil)
	if resp, err := empty.DelayAll(context.Background(), DelayCollectionCommand{}); err != nil || resp.Version != 0 || len(resp.Results) != 0 {
		t.Fatalf("empty delay = %+v err=%v", resp, err)
	}
	if resp, err := empty.DownloadAll(context.Background(), DownloadCollectionCommand{}); err != nil || resp.Version != 0 || len(resp.Results) != 0 {
		t.Fatalf("empty download = %+v err=%v", resp, err)
	}
	if _, err := rt.Delay(context.Background(), "missing", DelayCommand{}); !errors.Is(err, ErrProxyNotFound) {
		t.Fatalf("missing delay error = %v", err)
	}
	if _, err := rt.Download(context.Background(), "missing", DownloadCommand{}); !errors.Is(err, ErrProxyNotFound) {
		t.Fatalf("missing download error = %v", err)
	}
}

func TestRuntimeCollectionConcurrencyRequestFallbackOrderAndValidation(t *testing.T) {
	cfg := config.Default()
	cfg.Delay.Concurrency = 2
	cfg.Download.Concurrency = 2
	rt := makeRuntime(t, cfg, directRecords("first", "second", "third"))

	delayTracker := &concurrencyTracker{}
	delayServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer delayTracker.enter()()
		time.Sleep(25 * time.Millisecond)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer delayServer.Close()
	delayAll, err := rt.DelayAll(context.Background(), DelayCollectionCommand{DelayCommand: DelayCommand{
		URLs:     []DelayTargetCommand{{URL: delayServer.URL}},
		Timeout:  intPtr(1000),
		Expected: stringPtr("*"),
		Rounds:   intPtr(1),
		Unified:  boolPtr(false),
	}})
	if err != nil {
		t.Fatal(err)
	}
	if delayTracker.peakValue() != 2 || len(delayAll.Results) != 3 {
		t.Fatalf("delay fallback peak=%d results=%+v", delayTracker.peakValue(), delayAll.Results)
	}
	for i, want := range []string{"first", "second", "third"} {
		if delayAll.Results[i].Digest != want {
			t.Fatalf("delay result order = %+v", delayAll.Results)
		}
	}

	downloadTracker := &concurrencyTracker{}
	downloadServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer downloadTracker.enter()()
		time.Sleep(25 * time.Millisecond)
		_, _ = w.Write([]byte("abcd"))
	}))
	defer downloadServer.Close()
	downloadAll, err := rt.DownloadAll(context.Background(), DownloadCollectionCommand{
		DownloadCommand: DownloadCommand{
			URLs:     []DownloadTargetCommand{{URL: downloadServer.URL}},
			Timeout:  intPtr(1000),
			Rounds:   intPtr(1),
			MaxBytes: intPtr(4),
		},
		Concurrency: intPtr(3),
	})
	if err != nil {
		t.Fatal(err)
	}
	if downloadTracker.peakValue() != 3 || len(downloadAll.Results) != 3 {
		t.Fatalf("download request peak=%d results=%+v", downloadTracker.peakValue(), downloadAll.Results)
	}

	if _, err := rt.DelayAll(context.Background(), DelayCollectionCommand{Concurrency: intPtr(-1)}); err == nil {
		t.Fatal("negative delay concurrency returned nil error")
	}
	if _, err := rt.DownloadAll(context.Background(), DownloadCollectionCommand{Concurrency: intPtr(0)}); err == nil {
		t.Fatal("zero download concurrency returned nil error")
	}
}

func TestRuntimeSingleTestsDoNotWaitForCollectionConcurrency(t *testing.T) {
	cfg := config.Default()
	cfg.Delay.Concurrency = 1
	cfg.Download.Concurrency = 1
	rt := makeRuntime(t, cfg, directRecords("node"))

	delayStarted := make(chan struct{})
	delayRelease := make(chan struct{})
	delayOnce := sync.Once{}
	delayServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/block":
			delayOnce.Do(func() { close(delayStarted) })
			<-delayRelease
			w.WriteHeader(http.StatusNoContent)
		default:
			time.Sleep(2 * time.Millisecond)
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer delayServer.Close()
	collectionDelayDone := make(chan error, 1)
	go func() {
		_, err := rt.DelayAll(context.Background(), DelayCollectionCommand{
			DelayCommand: DelayCommand{
				URLs:     []DelayTargetCommand{{URL: delayServer.URL + "/block"}},
				Timeout:  intPtr(1000),
				Expected: stringPtr("*"),
				Rounds:   intPtr(1),
				Unified:  boolPtr(false),
			},
			Concurrency: intPtr(1),
		})
		collectionDelayDone <- err
	}()
	waitForClose(t, delayStarted, "delay collection start")
	singleDelayDone := make(chan error, 1)
	go func() {
		_, err := rt.Delay(context.Background(), "node", DelayCommand{
			URLs:     []DelayTargetCommand{{URL: delayServer.URL + "/fast"}},
			Timeout:  intPtr(1000),
			Expected: stringPtr("*"),
			Rounds:   intPtr(1),
			Unified:  boolPtr(false),
		})
		singleDelayDone <- err
	}()
	requireCompletesBeforeRelease(t, singleDelayDone, delayRelease, collectionDelayDone, "single delay")

	downloadStarted := make(chan struct{})
	downloadRelease := make(chan struct{})
	downloadOnce := sync.Once{}
	downloadServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/block":
			downloadOnce.Do(func() { close(downloadStarted) })
			<-downloadRelease
		default:
			time.Sleep(2 * time.Millisecond)
		}
		_, _ = w.Write([]byte("abcd"))
	}))
	defer downloadServer.Close()
	collectionDownloadDone := make(chan error, 1)
	go func() {
		_, err := rt.DownloadAll(context.Background(), DownloadCollectionCommand{
			DownloadCommand: DownloadCommand{
				URLs:     []DownloadTargetCommand{{URL: downloadServer.URL + "/block"}},
				Timeout:  intPtr(1000),
				Rounds:   intPtr(1),
				MaxBytes: intPtr(4),
			},
			Concurrency: intPtr(1),
		})
		collectionDownloadDone <- err
	}()
	waitForClose(t, downloadStarted, "download collection start")
	singleDownloadDone := make(chan error, 1)
	go func() {
		_, err := rt.Download(context.Background(), "node", DownloadCommand{
			URLs:     []DownloadTargetCommand{{URL: downloadServer.URL + "/fast"}},
			Timeout:  intPtr(1000),
			Rounds:   intPtr(1),
			MaxBytes: intPtr(4),
		})
		singleDownloadDone <- err
	}()
	requireCompletesBeforeRelease(t, singleDownloadDone, downloadRelease, collectionDownloadDone, "single download")
}

func TestRuntimeProxyHTTPStreamsUpstreamAndMapsPreResponseErrors(t *testing.T) {
	var seenMethod string
	var seenBody string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenMethod = r.Method
		buf, _ := io.ReadAll(r.Body)
		seenBody = string(buf)
		if r.Header.Get("X-Test") != "one" {
			t.Fatalf("proxy request header = %q", r.Header.Get("X-Test"))
		}
		w.Header().Set("X-Upstream", "yes")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte("proxied"))
	}))
	defer upstream.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, upstream.URL, http.StatusFound)
	}))
	defer redirect.Close()
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
		_, _ = w.Write([]byte("late"))
	}))
	defer slow.Close()

	rt := makeRuntime(t, config.Default(), directRecords("node"))
	body := "hello"
	method := http.MethodPost
	resp, err := rt.ProxyHTTP(context.Background(), "node", ProxyRequestCommand{
		URL:     upstream.URL,
		Method:  &method,
		Headers: map[string]string{"X-Test": "one"},
		Body:    &body,
	})
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if err := resp.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusAccepted ||
		resp.Header.Get("X-Upstream") != "yes" ||
		string(data) != "proxied" ||
		seenMethod != http.MethodPost ||
		seenBody != "hello" {
		t.Fatalf("proxied response status=%d header=%q body=%q method=%s seenBody=%q", resp.StatusCode, resp.Header.Get("X-Upstream"), data, seenMethod, seenBody)
	}

	resp, err = rt.ProxyHTTP(context.Background(), "node", ProxyRequestCommand{URL: redirect.URL, FollowRedirect: boolPtr(false)})
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("disabled redirect status = %d", resp.StatusCode)
	}

	if _, err := rt.ProxyHTTP(context.Background(), "node", ProxyRequestCommand{URL: slow.URL, Timeout: intPtr(1)}); !errors.Is(err, ErrProxyRequestTimeout) {
		t.Fatalf("timeout error = %v", err)
	}
	if _, err := rt.ProxyHTTP(context.Background(), "missing", ProxyRequestCommand{URL: upstream.URL}); !errors.Is(err, ErrProxyNotFound) {
		t.Fatalf("missing proxy error = %v", err)
	}
	if _, err := rt.ProxyHTTP(context.Background(), "node", ProxyRequestCommand{URL: "ftp://example.test"}); err == nil {
		t.Fatal("invalid proxy request URL returned nil error")
	}
	badMethod := "BAD METHOD"
	if _, err := rt.ProxyHTTP(context.Background(), "node", ProxyRequestCommand{URL: upstream.URL, Method: &badMethod}); err == nil {
		t.Fatal("invalid proxy request method returned nil error")
	}
	if _, err := rt.ProxyHTTP(context.Background(), "node", ProxyRequestCommand{URL: upstream.URL, Timeout: intPtr(0)}); err == nil {
		t.Fatal("invalid proxy request timeout returned nil error")
	}
}

func makeRuntime(t *testing.T, cfg config.Config, records []*proxyconfig.Record) *Runtime {
	t.Helper()
	rt, err := New(cfg, records)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(rt.Close)
	return rt
}

func directRecords(digests ...string) []*proxyconfig.Record {
	records := make([]*proxyconfig.Record, 0, len(digests))
	for _, digest := range digests {
		records = append(records, &proxyconfig.Record{
			Digest: digest,
			Raw:    map[string]any{"name": digest, "type": "direct", "server": "direct", "port": 0},
			Proxy:  appDirectProxy{},
		})
	}
	return records
}

func proxyYAML(name, server string) string {
	return "proxies:\n" +
		"  - name: " + name + "\n" +
		"    type: ss\n" +
		"    server: " + server + "\n" +
		"    port: 8388\n" +
		"    cipher: aes-128-gcm\n" +
		"    password: password\n"
}

func testEndpoints(t *testing.T) (delayURL string, downloadURL string, cleanup func()) {
	t.Helper()
	delayServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Millisecond)
		w.WriteHeader(http.StatusNoContent)
	}))
	downloadServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Millisecond)
		_, _ = w.Write([]byte("abcd"))
	}))
	return delayServer.URL, downloadServer.URL, func() {
		delayServer.Close()
		downloadServer.Close()
	}
}

func intPtr(value int) *int { return &value }

func boolPtr(value bool) *bool { return &value }

func stringPtr(value string) *string { return &value }

type concurrencyTracker struct {
	active int32
	peak   int32
}

func (t *concurrencyTracker) enter() func() {
	current := atomic.AddInt32(&t.active, 1)
	for {
		peak := atomic.LoadInt32(&t.peak)
		if current <= peak || atomic.CompareAndSwapInt32(&t.peak, peak, current) {
			break
		}
	}
	return func() { atomic.AddInt32(&t.active, -1) }
}

func (t *concurrencyTracker) peakValue() int32 {
	return atomic.LoadInt32(&t.peak)
}

func waitForClose(t *testing.T, ch <-chan struct{}, name string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for %s", name)
	}
}

func requireCompletesBeforeRelease(t *testing.T, singleDone <-chan error, release chan struct{}, collectionDone <-chan error, name string) {
	t.Helper()
	select {
	case err := <-singleDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(150 * time.Millisecond):
		close(release)
		if err := <-collectionDone; err != nil {
			t.Fatal(err)
		}
		if err := <-singleDone; err != nil {
			t.Fatal(err)
		}
		t.Fatalf("%s waited for collection concurrency", name)
	}
	close(release)
	if err := <-collectionDone; err != nil {
		t.Fatal(err)
	}
}

type appDirectProxy struct{}

func (appDirectProxy) Name() string { return "direct" }
func (appDirectProxy) Type() C.AdapterType {
	return C.Direct
}
func (appDirectProxy) Addr() string { return "direct" }
func (appDirectProxy) SupportUDP() bool {
	return false
}
func (appDirectProxy) ProxyInfo() C.ProxyInfo { return C.ProxyInfo{} }
func (appDirectProxy) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]string{"name": "direct"})
}
func (p appDirectProxy) DialContext(ctx context.Context, metadata *C.Metadata) (C.Conn, error) {
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", metadata.RemoteAddress())
	if err != nil {
		return nil, err
	}
	return outbound.NewConn(conn, p), nil
}
func (appDirectProxy) ListenPacketContext(context.Context, *C.Metadata) (C.PacketConn, error) {
	return nil, C.ErrNotSupport
}
func (appDirectProxy) SupportUOT() bool { return false }
func (appDirectProxy) IsL3Protocol(*C.Metadata) bool {
	return false
}
func (appDirectProxy) Unwrap(*C.Metadata, bool) C.Proxy { return nil }
func (appDirectProxy) Close() error                     { return nil }
func (p appDirectProxy) Adapter() C.ProxyAdapter        { return p }
func (appDirectProxy) AliveForTestUrl(string) bool {
	return true
}
func (appDirectProxy) DelayHistory() []C.DelayHistory { return nil }
func (appDirectProxy) ExtraDelayHistories() map[string]C.ProxyState {
	return nil
}
func (appDirectProxy) LastDelayForTestUrl(string) uint16 { return 0 }
func (appDirectProxy) URLTest(context.Context, string, utils.IntRanges[uint16]) (uint16, error) {
	return 0, C.ErrNotSupport
}

var _ C.Proxy = appDirectProxy{}
