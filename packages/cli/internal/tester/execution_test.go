package tester

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/metacubex/mihomo/adapter/outbound"
	"github.com/metacubex/mihomo/common/utils"
	C "github.com/metacubex/mihomo/constant"
	"mihomo-st/internal/httpclient"
)

func TestMetricAndPlanNumericFieldsAreInt(t *testing.T) {
	var delay DelayMetrics
	var download DownloadMetrics
	var downloadTarget DownloadTarget
	var _ int = delay.Min
	var _ int = delay.Cost
	var _ int = download.Score
	var _ int = downloadTarget.MaxBytes
}

func TestDelayExecutesHEADRequestsWithHeadersExpectedStatusAndUnifiedProbe(t *testing.T) {
	var requests int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		if r.Method != http.MethodHead {
			t.Fatalf("delay method = %s", r.Method)
		}
		if r.Header.Get("X-Test") != "delay" {
			t.Fatalf("delay header = %q", r.Header.Get("X-Test"))
		}
		time.Sleep(2 * time.Millisecond)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	metrics, err := (&Tester{}).Delay(context.Background(), tcpProxy{}, DelayPlan{Targets: []DelayTarget{{
		URL:            server.URL,
		Timeout:        1000,
		Headers:        map[string]string{"X-Test": "delay"},
		FollowRedirect: true,
		Expected:       "204",
		Rounds:         1,
		Unified:        true,
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if metrics.Success != 1 || metrics.Failed != 0 || metrics.Total != 1 || metrics.Min <= 0 || metrics.Cost <= 0 {
		t.Fatalf("delay metrics = %+v", metrics)
	}
	if atomic.LoadInt32(&requests) != 2 {
		t.Fatalf("unified delay requests = %d, want 2", requests)
	}
}

func TestDelayTargetsRunInParallelButRoundsStaySerial(t *testing.T) {
	var active int32
	var peak int32
	allActive := make(chan struct{})
	var closeOnce sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		current := atomic.AddInt32(&active, 1)
		rememberPeak(&peak, current)
		if current == 2 {
			closeOnce.Do(func() { close(allActive) })
		}
		select {
		case <-allActive:
		case <-time.After(150 * time.Millisecond):
		}
		time.Sleep(2 * time.Millisecond)
		w.WriteHeader(http.StatusNoContent)
		atomic.AddInt32(&active, -1)
	}))
	defer server.Close()

	metrics, err := (&Tester{}).Delay(context.Background(), tcpProxy{}, DelayPlan{Targets: []DelayTarget{
		{URL: server.URL + "/a", Timeout: 1000, Expected: "*", Rounds: 1},
		{URL: server.URL + "/b", Timeout: 1000, Expected: "*", Rounds: 1},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if metrics.Success != 2 || metrics.Total != 2 {
		t.Fatalf("parallel delay metrics = %+v", metrics)
	}
	if atomic.LoadInt32(&peak) != 2 {
		t.Fatalf("delay target peak = %d, want 2", peak)
	}

	atomic.StoreInt32(&active, 0)
	atomic.StoreInt32(&peak, 0)
	serial := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		current := atomic.AddInt32(&active, 1)
		rememberPeak(&peak, current)
		time.Sleep(2 * time.Millisecond)
		w.WriteHeader(http.StatusNoContent)
		atomic.AddInt32(&active, -1)
	}))
	defer serial.Close()
	metrics, err = (&Tester{}).Delay(context.Background(), tcpProxy{}, DelayPlan{Targets: []DelayTarget{
		{URL: serial.URL, Timeout: 1000, Expected: "*", Rounds: 3, Unified: true},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if metrics.Success != 3 || metrics.Total != 3 || atomic.LoadInt32(&peak) != 1 {
		t.Fatalf("serial delay rounds metrics=%+v peak=%d", metrics, peak)
	}
}

func TestDelayAggregatesFailuresAndReturnsInvalidExpectedAsRequestError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Millisecond)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	metrics, err := (&Tester{}).Delay(context.Background(), tcpProxy{}, DelayPlan{Targets: []DelayTarget{
		{URL: server.URL, Timeout: 1000, Expected: "204", Rounds: 2},
	}})
	if !errors.Is(err, ErrAllRoundsFailed) {
		t.Fatalf("delay failure error = %v", err)
	}
	if metrics.Min != -1 || metrics.Max != -1 || metrics.Avg != -1 || metrics.Cost != -1 ||
		metrics.Success != 0 || metrics.Failed != 2 || metrics.Total != 2 || metrics.Error != ErrAllRoundsFailed.Error() {
		t.Fatalf("all-failed delay metrics = %+v", metrics)
	}

	if _, err := (&Tester{}).Delay(context.Background(), tcpProxy{}, DelayPlan{Targets: []DelayTarget{
		{URL: server.URL, Timeout: 1000, Expected: "not-a-range", Rounds: 1},
	}}); err == nil {
		t.Fatal("invalid expected range returned nil error")
	}
}

func TestDelayTreatsRequestConstructionAndDialFailuresAsRoundFailures(t *testing.T) {
	metrics, err := (&Tester{}).Delay(context.Background(), tcpProxy{}, DelayPlan{Targets: []DelayTarget{
		{URL: "http://%zz", Timeout: 1000, Expected: "*", Rounds: 1},
	}})
	if !errors.Is(err, ErrAllRoundsFailed) || metrics.Failed != 1 || metrics.Total != 1 {
		t.Fatalf("bad URL delay metrics=%+v err=%v", metrics, err)
	}

	expected, err := utils.NewUnsignedRanges[uint16]("200")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := delayOnce(context.Background(), blockedProxy{}, "http://example.test", expected, false, nil, true); err == nil {
		t.Fatal("blocked proxy delay returned nil error")
	}
	if _, err := doDelayRequest(context.Background(), httpclient.New(httpclient.Options{
		Transport: fakeRoundTripper(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("round trip failed")
		}),
	}), "http://example.test", nil); err == nil {
		t.Fatal("delay request transport error returned nil")
	}
}

func TestDownloadExecutesGETRequestsWithHeadersMaxBytesAndMetrics(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("download method = %s", r.Method)
		}
		if r.Header.Get("X-Test") != "download" {
			t.Fatalf("download header = %q", r.Header.Get("X-Test"))
		}
		_, _ = w.Write([]byte("0123456789"))
	}))
	defer server.Close()

	metrics, err := (&Tester{}).Download(context.Background(), tcpProxy{}, DownloadPlan{Targets: []DownloadTarget{{
		URL:            server.URL,
		Timeout:        1000,
		Headers:        map[string]string{"X-Test": "download"},
		FollowRedirect: true,
		Rounds:         2,
		MaxBytes:       4,
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if metrics.Success != 2 || metrics.Failed != 0 || metrics.Total != 2 ||
		metrics.Min <= 0 || metrics.Max < metrics.Min || metrics.Avg <= 0 || metrics.Score <= 0 {
		t.Fatalf("download metrics = %+v", metrics)
	}
}

func TestDownloadTargetsAndRoundsRunSerially(t *testing.T) {
	var active int32
	var peak int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		current := atomic.AddInt32(&active, 1)
		rememberPeak(&peak, current)
		time.Sleep(10 * time.Millisecond)
		_, _ = w.Write([]byte("abcd"))
		atomic.AddInt32(&active, -1)
	}))
	defer server.Close()

	metrics, err := (&Tester{}).Download(context.Background(), tcpProxy{}, DownloadPlan{Targets: []DownloadTarget{
		{URL: server.URL + "/one", Timeout: 1000, Rounds: 2, MaxBytes: 4},
		{URL: server.URL + "/two", Timeout: 1000, Rounds: 2, MaxBytes: 4},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if metrics.Success != 4 || metrics.Total != 4 || atomic.LoadInt32(&peak) != 1 {
		t.Fatalf("serial download metrics=%+v peak=%d", metrics, peak)
	}
}

func TestDownloadCountsPartialBodyBeforeTimeoutAsSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(bytes.Repeat([]byte("x"), 1024))
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		time.Sleep(200 * time.Millisecond)
	}))
	defer server.Close()

	metrics, err := (&Tester{}).Download(context.Background(), tcpProxy{}, DownloadPlan{Targets: []DownloadTarget{
		{URL: server.URL, Timeout: 50, Rounds: 1, MaxBytes: 1024 * 1024},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if metrics.Success != 1 || metrics.Failed != 0 || metrics.Min <= 0 || metrics.Score <= 0 {
		t.Fatalf("partial timeout metrics = %+v", metrics)
	}
}

func TestDownloadReportsAllRoundFailuresForBadStatusNoBytesAndBadURL(t *testing.T) {
	badStatus := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer badStatus.Close()

	metrics, err := (&Tester{}).Download(context.Background(), tcpProxy{}, DownloadPlan{Targets: []DownloadTarget{
		{URL: badStatus.URL, Timeout: 1000, Rounds: 1, MaxBytes: 4},
		{URL: "http://%zz", Timeout: 1000, Rounds: 1, MaxBytes: 4},
	}})
	if !errors.Is(err, ErrAllRoundsFailed) {
		t.Fatalf("download failure error = %v", err)
	}
	if metrics.Min != -1 || metrics.Max != -1 || metrics.Avg != -1 || metrics.Score != -1 ||
		metrics.Success != 0 || metrics.Failed != 2 || metrics.Total != 2 || metrics.Error != ErrAllRoundsFailed.Error() {
		t.Fatalf("all-failed download metrics = %+v", metrics)
	}

	if _, err := downloadOnce(context.Background(), tcpProxy{}, "http://%zz", 1, nil, true); err == nil {
		t.Fatal("downloadOnce accepted malformed URL")
	}
}

func TestDelayAndDownloadHonorRedirectControls(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/start":
			http.Redirect(w, r, "/ok", http.StatusFound)
		case "/ok":
			time.Sleep(2 * time.Millisecond)
			_, _ = w.Write([]byte("abcd"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	delayMetrics, err := (&Tester{}).Delay(context.Background(), tcpProxy{}, DelayPlan{Targets: []DelayTarget{
		{URL: server.URL + "/start", Timeout: 1000, FollowRedirect: true, Expected: "200", Rounds: 1},
	}})
	if err != nil || delayMetrics.Success != 1 {
		t.Fatalf("follow redirect delay metrics=%+v err=%v", delayMetrics, err)
	}

	noFollowDelay, err := (&Tester{}).Delay(context.Background(), tcpProxy{}, DelayPlan{Targets: []DelayTarget{
		{URL: server.URL + "/start", Timeout: 1000, FollowRedirect: false, Expected: "200", Rounds: 1},
	}})
	if !errors.Is(err, ErrAllRoundsFailed) || noFollowDelay.Failed != 1 {
		t.Fatalf("no-follow delay metrics=%+v err=%v", noFollowDelay, err)
	}

	downloadMetrics, err := (&Tester{}).Download(context.Background(), tcpProxy{}, DownloadPlan{Targets: []DownloadTarget{
		{URL: server.URL + "/start", Timeout: 1000, FollowRedirect: false, Rounds: 1, MaxBytes: 4},
	}})
	if err != nil || downloadMetrics.Success != 1 {
		t.Fatalf("no-follow download metrics=%+v err=%v", downloadMetrics, err)
	}
}

func rememberPeak(peak *int32, value int32) {
	for {
		current := atomic.LoadInt32(peak)
		if value <= current || atomic.CompareAndSwapInt32(peak, current, value) {
			return
		}
	}
}

type fakeRoundTripper func(*http.Request) (*http.Response, error)

func (f fakeRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type blockedProxy struct {
	tcpProxy
}

func (blockedProxy) DialContext(context.Context, *C.Metadata) (C.Conn, error) {
	return nil, errors.New("dial blocked")
}

type tcpProxy struct{}

func (tcpProxy) Name() string { return "direct" }
func (tcpProxy) Type() C.AdapterType {
	return C.Direct
}
func (tcpProxy) Addr() string { return "direct" }
func (tcpProxy) SupportUDP() bool {
	return false
}
func (tcpProxy) ProxyInfo() C.ProxyInfo { return C.ProxyInfo{} }
func (tcpProxy) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]string{"name": "direct"})
}
func (p tcpProxy) DialContext(ctx context.Context, metadata *C.Metadata) (C.Conn, error) {
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", metadata.RemoteAddress())
	if err != nil {
		return nil, err
	}
	return outbound.NewConn(conn, p), nil
}
func (tcpProxy) ListenPacketContext(context.Context, *C.Metadata) (C.PacketConn, error) {
	return nil, C.ErrNotSupport
}
func (tcpProxy) SupportUOT() bool { return false }
func (tcpProxy) IsL3Protocol(*C.Metadata) bool {
	return false
}
func (tcpProxy) Unwrap(*C.Metadata, bool) C.Proxy { return nil }
func (tcpProxy) Close() error                     { return nil }
func (p tcpProxy) Adapter() C.ProxyAdapter        { return p }
func (tcpProxy) AliveForTestUrl(string) bool {
	return true
}
func (tcpProxy) DelayHistory() []C.DelayHistory { return nil }
func (tcpProxy) ExtraDelayHistories() map[string]C.ProxyState {
	return nil
}
func (tcpProxy) LastDelayForTestUrl(string) uint16 { return 0 }
func (tcpProxy) URLTest(context.Context, string, utils.IntRanges[uint16]) (uint16, error) {
	return 0, C.ErrNotSupport
}

var _ C.Proxy = tcpProxy{}
