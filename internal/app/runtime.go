package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/semaphore"
	"mihomo-st/internal/config"
	"mihomo-st/internal/httpclient"
	"mihomo-st/internal/proxyconfig"
	"mihomo-st/internal/store"
	"mihomo-st/internal/tester"
)

const (
	SourceText   = "text"
	SourceLocal  = "local"
	SourceRemote = "remote"

	ImportReplace = "replace"
	ImportAppend  = "append"
)

type Runtime struct {
	proxies       *store.Store
	config        *config.Store
	tester        *tester.Tester
	importMu      sync.Mutex
	expandCache   *proxyconfig.ServerExpandCache
	executableDir string
}

type ProxyImportResult struct {
	Version  int
	Proxies  []store.ProxyInfo
	Warnings []proxyconfig.Warning
}

type ProxyListResult struct {
	Version int
	Proxies []store.ProxyInfo
}

type DelayResult struct {
	Version int
	Digest  string
	Metrics tester.DelayMetrics
}

type DelayCollectionResult struct {
	Version int
	Results []DelayResult
}

type DownloadResult struct {
	Version int
	Digest  string
	Metrics tester.DownloadMetrics
}

type DownloadCollectionResult struct {
	Version int
	Results []DownloadResult
}

type closeProxyResponseBody struct {
	io.ReadCloser
	close func()
}

var expandServerDomains = proxyconfig.ExpandServerDomains

func New(initialConfig config.Config, initialProxies ...[]*proxyconfig.Record) (*Runtime, error) {
	configStore, err := config.NewStore(initialConfig)
	if err != nil {
		return nil, err
	}
	var records []*proxyconfig.Record
	if len(initialProxies) > 0 {
		records = initialProxies[0]
	}
	executableDir, err := defaultExecutableDir()
	if err != nil {
		return nil, err
	}
	return &Runtime{
		proxies:       store.New(records),
		config:        configStore,
		tester:        &tester.Tester{},
		expandCache:   &proxyconfig.ServerExpandCache{},
		executableDir: executableDir,
	}, nil
}

func defaultExecutableDir() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.Dir(exe), nil
}

func (r *Runtime) Close() {
	r.proxies.Close()
}

func (r *Runtime) Config() config.Config {
	return r.config.Snapshot()
}

func (r *Runtime) PatchConfig(values map[string]json.RawMessage) (config.Config, error) {
	return r.config.PatchAPI(values)
}

func (r *Runtime) ListProxies() (ProxyListResult, error) {
	ref := r.proxies.Current()
	defer ref.Release()
	return ProxyListResult{
		Version: ref.Version(),
		Proxies: ref.List(),
	}, nil
}

func (r *Runtime) ImportProxies(ctx context.Context, cmd ProxyImportCommand) (ProxyImportResult, error) {
	if cmd.Payload == "" {
		return ProxyImportResult{}, ErrProxySourceRequired
	}
	mode := cmd.Mode
	if mode == "" {
		mode = ImportReplace
	}
	if mode != ImportReplace && mode != ImportAppend {
		return ProxyImportResult{}, fmt.Errorf("mode must be replace or append")
	}
	proxyServer, err := r.importProxyServer(cmd.ProxyServer)
	if err != nil {
		return ProxyImportResult{}, err
	}

	r.importMu.Lock()
	defer r.importMu.Unlock()

	result, err := r.loadImportSource(ctx, cmd)
	if err != nil {
		closeRecords(result.Records)
		return ProxyImportResult{}, err
	}

	result = r.expandImport(ctx, result, proxyServer)

	var ref *store.SnapshotRef
	if mode == ImportAppend {
		ref = r.proxies.Append(result.Records)
	} else {
		ref = r.proxies.Publish(result.Records)
	}
	defer ref.Release()

	return ProxyImportResult{
		Version:  ref.Version(),
		Proxies:  ref.List(),
		Warnings: nonNilWarnings(result.Warnings),
	}, nil
}

func (r *Runtime) loadImportSource(ctx context.Context, cmd ProxyImportCommand) (proxyconfig.Result, error) {
	switch cmd.Type {
	case SourceText:
		return proxyconfig.LoadText(cmd.Payload)
	case SourceLocal:
		return proxyconfig.LoadLocal(r.resolveLocalPath(cmd.Payload))
	case SourceRemote:
		timeout := r.Config().DefaultTimeout
		if cmd.Timeout != nil && *cmd.Timeout <= 0 {
			return proxyconfig.Result{}, fmt.Errorf("timeout must be greater than 0")
		}
		if cmd.Timeout != nil {
			timeout = *cmd.Timeout
		}
		return proxyconfig.LoadRemote(ctx, cmd.Payload, proxyconfig.RemoteOptions{
			Timeout:        time.Duration(timeout) * time.Millisecond,
			Headers:        cmd.Headers,
			FollowRedirect: cmd.FollowRedirect,
		})
	default:
		return proxyconfig.Result{}, fmt.Errorf("type must be text, local, or remote")
	}
}

func (r *Runtime) resolveLocalPath(path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(r.executableDir, path)
}

func (r *Runtime) importProxyServer(override *ProxyServerOverride) (config.ProxyServer, error) {
	if override == nil {
		return r.Config().ProxyServer, nil
	}
	proxyServer := config.Default().ProxyServer
	if override.Expand != nil {
		proxyServer.Expand = *override.Expand
	}
	if override.Nameservers != nil {
		proxyServer.Nameservers = append([]string(nil), override.Nameservers...)
	}
	if override.Timeout != nil {
		if *override.Timeout <= 0 {
			return config.ProxyServer{}, fmt.Errorf("proxy-server.timeout must be greater than 0")
		}
		proxyServer.Timeout = *override.Timeout
	}
	if err := proxyServer.Validate(); err != nil {
		return config.ProxyServer{}, fmt.Errorf("proxy-server: %w", err)
	}
	return proxyServer, nil
}

func (r *Runtime) expandImport(ctx context.Context, result proxyconfig.Result, proxyServer config.ProxyServer) proxyconfig.Result {
	if !proxyServer.Expand {
		return result
	}
	return expandServerDomains(ctx, result, proxyconfig.ServerExpandOptions{
		Enabled:     proxyServer.Expand,
		Nameservers: proxyServer.Nameservers,
		Timeout:     time.Duration(proxyServer.Timeout) * time.Millisecond,
		Cache:       r.expandCache,
	})
}

func (r *Runtime) Delay(ctx context.Context, digest string, cmd DelayCommand) (DelayResult, error) {
	ref := r.proxies.Current()
	defer ref.Release()

	record, ok := ref.Get(digest)
	if !ok {
		return DelayResult{}, ErrProxyNotFound
	}
	plan, err := NormalizeDelayCommand(cmd, r.Config())
	if err != nil {
		return DelayResult{}, err
	}
	metrics, err := r.tester.Delay(ctx, record.Proxy, plan)
	if err != nil && !errors.Is(err, tester.ErrAllRoundsFailed) {
		return DelayResult{}, err
	}
	return DelayResult{Version: ref.Version(), Digest: digest, Metrics: metrics}, nil
}

func (r *Runtime) DelayAll(ctx context.Context, cmd DelayCollectionCommand) (DelayCollectionResult, error) {
	ref := r.proxies.Current()
	defer ref.Release()

	records := ref.Records()
	plan, concurrency, err := NormalizeDelayCollectionCommand(cmd, r.Config())
	if err != nil {
		return DelayCollectionResult{}, err
	}
	if len(records) == 0 {
		return DelayCollectionResult{Version: ref.Version(), Results: []DelayResult{}}, nil
	}

	resp := DelayCollectionResult{
		Version: ref.Version(),
		Results: make([]DelayResult, len(records)),
	}
	group, groupCtx := errgroup.WithContext(ctx)
	limit := semaphore.NewWeighted(int64(concurrency))
	for idx, record := range records {
		if err := limit.Acquire(groupCtx, 1); err != nil {
			if waitErr := group.Wait(); waitErr != nil {
				return DelayCollectionResult{}, waitErr
			}
			return DelayCollectionResult{}, err
		}
		idx, record := idx, record
		group.Go(func() error {
			defer limit.Release(1)
			metrics, err := r.tester.Delay(groupCtx, record.Proxy, plan)
			if err != nil && !errors.Is(err, tester.ErrAllRoundsFailed) {
				return err
			}
			resp.Results[idx] = DelayResult{Version: ref.Version(), Digest: record.Digest, Metrics: metrics}
			return nil
		})
	}
	if err := group.Wait(); err != nil {
		return DelayCollectionResult{}, err
	}
	return resp, nil
}

func (r *Runtime) Download(ctx context.Context, digest string, cmd DownloadCommand) (DownloadResult, error) {
	ref := r.proxies.Current()
	defer ref.Release()

	record, ok := ref.Get(digest)
	if !ok {
		return DownloadResult{}, ErrProxyNotFound
	}
	plan, err := NormalizeDownloadCommand(cmd, r.Config())
	if err != nil {
		return DownloadResult{}, err
	}
	metrics, err := r.tester.Download(ctx, record.Proxy, plan)
	if err != nil && !errors.Is(err, tester.ErrAllRoundsFailed) {
		return DownloadResult{}, err
	}
	return DownloadResult{Version: ref.Version(), Digest: digest, Metrics: metrics}, nil
}

func (r *Runtime) DownloadAll(ctx context.Context, cmd DownloadCollectionCommand) (DownloadCollectionResult, error) {
	ref := r.proxies.Current()
	defer ref.Release()

	records := ref.Records()
	plan, concurrency, err := NormalizeDownloadCollectionCommand(cmd, r.Config())
	if err != nil {
		return DownloadCollectionResult{}, err
	}
	if len(records) == 0 {
		return DownloadCollectionResult{Version: ref.Version(), Results: []DownloadResult{}}, nil
	}
	resp := DownloadCollectionResult{
		Version: ref.Version(),
		Results: make([]DownloadResult, len(records)),
	}
	group, groupCtx := errgroup.WithContext(ctx)
	limit := semaphore.NewWeighted(int64(concurrency))
	for idx, record := range records {
		if err := limit.Acquire(groupCtx, 1); err != nil {
			if waitErr := group.Wait(); waitErr != nil {
				return DownloadCollectionResult{}, waitErr
			}
			return DownloadCollectionResult{}, err
		}
		idx, record := idx, record
		group.Go(func() error {
			defer limit.Release(1)
			metrics, err := r.tester.Download(groupCtx, record.Proxy, plan)
			if err != nil && !errors.Is(err, tester.ErrAllRoundsFailed) {
				return err
			}
			resp.Results[idx] = DownloadResult{Version: ref.Version(), Digest: record.Digest, Metrics: metrics}
			return nil
		})
	}
	if err := group.Wait(); err != nil {
		return DownloadCollectionResult{}, err
	}
	return resp, nil
}

func (r *Runtime) ProxyHTTP(ctx context.Context, digest string, cmd ProxyRequestCommand) (*http.Response, error) {
	ref := r.proxies.Current()
	record, ok := ref.Get(digest)
	if !ok {
		ref.Release()
		return nil, ErrProxyNotFound
	}

	timeout, err := proxyRequestTimeout(cmd.Timeout, r.Config().DefaultTimeout)
	if err != nil {
		ref.Release()
		return nil, err
	}
	if err := validateProxyRequestURL(cmd.URL); err != nil {
		ref.Release()
		return nil, err
	}
	followRedirect := true
	if cmd.FollowRedirect != nil {
		followRedirect = *cmd.FollowRedirect
	}
	method := ""
	if cmd.Method != nil {
		method = *cmd.Method
	}

	client := httpclient.NewProxied(record.Proxy, httpclient.Options{
		Timeout:          timeout,
		DisableRedirects: !followRedirect,
	})
	request, err := httpclient.NewRequest(ctx, method, cmd.URL, cmd.Headers, cmd.Body)
	if err != nil {
		ref.Release()
		client.CloseIdleConnections()
		return nil, err
	}
	response, err := client.Do(request)
	if err != nil {
		ref.Release()
		client.CloseIdleConnections()
		if errors.Is(err, context.DeadlineExceeded) || os.IsTimeout(err) {
			return nil, fmt.Errorf("%w: %v", ErrProxyRequestTimeout, err)
		}
		return nil, err
	}
	if response.Body == nil {
		response.Body = http.NoBody
	}
	response.Body = closeProxyResponseBody{
		ReadCloser: response.Body,
		close: func() {
			client.CloseIdleConnections()
			ref.Release()
		},
	}
	return response, nil
}

func (b closeProxyResponseBody) Close() error {
	err := b.ReadCloser.Close()
	if b.close != nil {
		b.close()
	}
	return err
}

func proxyRequestTimeout(value *int, fallback int) (time.Duration, error) {
	if value != nil && *value <= 0 {
		return 0, fmt.Errorf("timeout must be greater than 0")
	}
	timeout := fallback
	if value != nil {
		timeout = *value
	}
	if timeout <= 0 {
		return 0, fmt.Errorf("timeout must be greater than 0")
	}
	return time.Duration(timeout) * time.Millisecond, nil
}

func validateProxyRequestURL(rawURL string) error {
	if rawURL == "" {
		return fmt.Errorf("url is required")
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return err
	}
	if parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("url must use http or https")
	}
	return nil
}

func nonNilWarnings(warnings []proxyconfig.Warning) []proxyconfig.Warning {
	if warnings == nil {
		return []proxyconfig.Warning{}
	}
	return warnings
}

func closeRecords(records []*proxyconfig.Record) {
	for _, record := range records {
		if record == nil || record.Proxy == nil {
			continue
		}
		_ = record.Proxy.Close()
	}
}
