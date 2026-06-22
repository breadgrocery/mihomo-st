package server

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/metacubex/mihomo/adapter/outbound"
	"github.com/metacubex/mihomo/common/utils"
	C "github.com/metacubex/mihomo/constant"
	"mihomo-st/internal/app"
	"mihomo-st/internal/config"
	"mihomo-st/internal/digest"
	"mihomo-st/internal/proxyconfig"
	"mihomo-st/internal/version"
)

func TestRouterPublishesDocumentedVersionAndErrorShapes(t *testing.T) {
	api := newServerHarness(t)

	versionResponse := api.jsonObject(http.MethodGet, "/version", "", http.StatusOK)
	if versionResponse["name"] != version.Name || versionResponse["version"] != version.Version {
		t.Fatalf("version response = %+v", versionResponse)
	}
	for _, oldEnvelopeField := range []string{"code", "message", "data"} {
		if _, ok := versionResponse[oldEnvelopeField]; ok {
			t.Fatalf("version response contains old envelope field %q: %+v", oldEnvelopeField, versionResponse)
		}
	}

	api.errorObject(http.MethodGet, "/", "", http.StatusNotFound, "NOT_FOUND", "not found")
	api.errorObject(http.MethodPut, "/version", "", http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "method not allowed")
	api.errorObject(http.MethodGet, "/settings", "", http.StatusNotFound, "NOT_FOUND", "not found")
	api.errorObject(http.MethodPost, "/hash", `{}`, http.StatusNotFound, "NOT_FOUND", "not found")
	api.errorObject(http.MethodGet, "/proxies/not-a-route", "", http.StatusNotFound, "NOT_FOUND", "not found")
}

func TestDigestEndpointRequiresObjectAndUsesCanonicalDigest(t *testing.T) {
	api := newServerHarness(t)

	response := api.jsonObject(http.MethodPost, "/digest", `{
		"name": "ui-only",
		"type": "ss",
		"server": "node.example",
		"port": 443,
		"metadata": {"ignored": true},
		"_temporary": "ignored",
		"nested": {"name": "nested name is still data"}
	}`, http.StatusOK)

	want, err := digest.Sum(map[string]any{
		"type":   "ss",
		"server": "node.example",
		"port":   float64(443),
		"nested": map[string]any{"name": "nested name is still data"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if response["digest"] != want {
		t.Fatalf("digest = %v, want %s", response["digest"], want)
	}

	api.errorObject(http.MethodPost, "/digest", `null`, http.StatusBadRequest, "BAD_REQUEST", "cannot be null")
	api.errorObject(http.MethodPost, "/digest", `[]`, http.StatusBadRequest, "BAD_REQUEST", "cannot unmarshal array")
	api.errorObject(http.MethodPost, "/digest", `{}`, http.StatusOK, "", "")
}

func TestConfigEndpointDefaultsPatchSemanticsAndValidation(t *testing.T) {
	api := newServerHarness(t)

	defaults := api.jsonObject(http.MethodGet, "/config", "", http.StatusOK)
	delay := objectField(t, defaults, "delay")
	if delay["timeout"] != float64(config.DefaultDelayTimeout) ||
		delay["concurrency"] != float64(config.DefaultDelayConcurrency) ||
		delay["rounds"] != float64(config.DefaultDelayRounds) {
		t.Fatalf("delay defaults = %+v", delay)
	}
	download := objectField(t, defaults, "download")
	if download["concurrency"] != float64(config.DefaultDownloadConcurrency) {
		t.Fatalf("download defaults = %+v", download)
	}

	patched := api.jsonObject(http.MethodPatch, "/config", `{
		"delay": {
			"headers": {"X-Root": "yes"},
			"urls": [
				"https://one.example/generate_204",
				{"url": "https://two.example/generate_204", "rounds": 3}
			]
		},
		"download": {"concurrency": 2}
	}`, http.StatusOK)
	delay = objectField(t, patched, "delay")
	if delay["timeout"] != float64(config.DefaultDelayTimeout) {
		t.Fatalf("patch should deep-merge delay timeout, got %+v", delay)
	}
	if objectField(t, delay, "headers")["X-Root"] != "yes" {
		t.Fatalf("delay headers were not patched: %+v", delay)
	}
	urls, ok := delay["urls"].([]any)
	if !ok || len(urls) != 2 {
		t.Fatalf("delay urls were not replaced by array patch: %+v", delay["urls"])
	}
	secondURL := urls[1].(map[string]any)
	if secondURL["rounds"] != float64(3) {
		t.Fatalf("url item override missing: %+v", secondURL)
	}
	download = objectField(t, patched, "download")
	if download["concurrency"] != float64(2) {
		t.Fatalf("download concurrency patch = %+v", download)
	}

	api.errorObject(http.MethodPatch, "/config", `{"bogus": true}`, http.StatusBadRequest, "BAD_REQUEST", "invalid keys")
	afterRejected := api.jsonObject(http.MethodGet, "/config", "", http.StatusOK)
	if objectField(t, afterRejected, "download")["concurrency"] != float64(2) {
		t.Fatalf("failed patch changed config: %+v", afterRejected)
	}
	api.errorObject(http.MethodPatch, "/config", `null`, http.StatusBadRequest, "BAD_REQUEST", "cannot be null")
	api.errorObject(http.MethodPatch, "/config", `{"default-timeout":0}`, http.StatusBadRequest, "BAD_REQUEST", "default-timeout")
	api.errorObject(http.MethodPatch, "/config", `{"delay":{"urls":[{"url":"https://bad.example","timeout":0}]}}`, http.StatusBadRequest, "BAD_REQUEST", "greater than 0")
	api.errorObject(http.MethodPut, "/config", `{}`, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "method not allowed")
}

func TestProxyImportListAndRemoteHTTPOptions(t *testing.T) {
	api := newServerHarness(t)

	firstImport := api.jsonObject(http.MethodPost, "/proxies/import", `{
		"type": "text",
		"payload": `+jsonText(proxyYAML("alpha", "alpha.example"))+`,
		"headers": {"Authorization": "ignored"}
	}`, http.StatusOK)
	if firstImport["version"] != float64(1) {
		t.Fatalf("first import version = %+v", firstImport)
	}
	proxies := arrayField(t, firstImport, "proxies")
	if len(proxies) != 1 || objectField(t, proxies[0], "")["name"] != "alpha" || objectField(t, proxies[0], "")["digest"] == "" {
		t.Fatalf("first import proxies = %+v", proxies)
	}
	if warnings := arrayField(t, firstImport, "warnings"); len(warnings) != 0 {
		t.Fatalf("unexpected warnings = %+v", warnings)
	}

	secondImport := api.jsonObject(http.MethodPost, "/proxies/import", `{
		"type": "text",
		"mode": "append",
		"payload": `+jsonText(proxyYAML("beta", "beta.example"))+`
	}`, http.StatusOK)
	if secondImport["version"] != float64(2) || len(arrayField(t, secondImport, "proxies")) != 2 {
		t.Fatalf("append response = %+v", secondImport)
	}
	list := api.jsonObject(http.MethodGet, "/proxies", "", http.StatusOK)
	if list["version"] != float64(2) || len(arrayField(t, list, "proxies")) != 2 {
		t.Fatalf("list response = %+v", list)
	}

	warned := api.jsonObject(http.MethodPost, "/proxies/import", `{
		"type": "text",
		"payload": `+jsonText("proxies:\n  - name: incomplete\n    type: ss\n  - "+strings.TrimPrefix(proxyYAML("kept", "kept.example"), "proxies:\n  - "))+`
	}`, http.StatusOK)
	if len(arrayField(t, warned, "warnings")) != 1 || len(arrayField(t, warned, "proxies")) != 1 {
		t.Fatalf("partial import response = %+v", warned)
	}

	seenRemote := make(chan http.Header, 2)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenRemote <- r.Header.Clone()
		_, _ = w.Write([]byte(proxyYAML("remote", "remote.example")))
	}))
	defer origin.Close()
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, origin.URL, http.StatusFound)
	}))
	defer redirector.Close()

	remote := api.jsonObject(http.MethodPost, "/proxies/import", `{
		"type": "remote",
		"payload": `+jsonText(redirector.URL)+`,
		"headers": {"Authorization": "Bearer token", "User-Agent": "contract-test"},
		"timeout": 1000
	}`, http.StatusOK)
	if len(arrayField(t, remote, "proxies")) != 1 {
		t.Fatalf("remote import response = %+v", remote)
	}
	headers := <-seenRemote
	if headers.Get("Authorization") != "Bearer token" || headers.Get("User-Agent") != "contract-test" {
		t.Fatalf("remote headers = %+v", headers)
	}

	api.errorObject(http.MethodPost, "/proxies/import", `{
		"type": "remote",
		"payload": `+jsonText(redirector.URL)+`,
		"timeout": 1000,
		"follow-redirect": false
	}`, http.StatusBadGateway, "BAD_GATEWAY", "302")
	api.errorObject(http.MethodPost, "/proxies/import", `{"type":"remote","payload":"ftp://example.test/list.yaml"}`, http.StatusBadRequest, "BAD_REQUEST", "remote source URL")
	api.errorObject(http.MethodPost, "/proxies/import", `{"type":"text"}`, http.StatusBadRequest, "BAD_REQUEST", "payload is required")
	api.errorObject(http.MethodPost, "/proxies", `{}`, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "method not allowed")
	api.errorObject(http.MethodPut, "/proxies", `{}`, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "method not allowed")
}

func TestProxyExportReturnsCurrentSnapshotRawNodeData(t *testing.T) {
	api := newServerHarness(t)
	api.jsonObject(http.MethodPost, "/proxies/import", `{
		"type": "text",
		"payload": `+jsonText(proxyYAML("alpha", "alpha.example")+"    udp: true\n    custom-field: keep\n    metadata:\n      source: imported\n      digest: stale\n")+`
	}`, http.StatusOK)

	exported := api.jsonObject(http.MethodGet, "/proxies/export", "", http.StatusOK)
	if _, ok := exported["version"]; ok {
		t.Fatalf("export response must not include version: %+v", exported)
	}
	proxies := arrayField(t, exported, "proxies")
	if len(proxies) != 1 {
		t.Fatalf("exported proxies = %+v", proxies)
	}
	proxy := objectField(t, proxies[0], "")
	if _, ok := proxy["digest"]; ok {
		t.Fatalf("exported proxy must not include top-level digest: %+v", proxy)
	}
	metadata := objectField(t, proxy, "metadata")
	if metadata["digest"] == "" || metadata["digest"] == "stale" || metadata["source"] != "imported" {
		t.Fatalf("exported metadata = %+v", metadata)
	}
	if proxy["name"] != "alpha" ||
		proxy["type"] != "ss" ||
		proxy["server"] != "alpha.example" ||
		proxy["port"] != float64(8388) ||
		proxy["cipher"] != "aes-128-gcm" ||
		proxy["password"] != "secret" ||
		proxy["udp"] != true ||
		proxy["custom-field"] != "keep" {
		t.Fatalf("exported proxy = %+v", proxy)
	}
}

func TestProxyHTTPRequestEndpointStreamsRawUpstreamResponses(t *testing.T) {
	api := newServerHarness(t, directRecord("direct"))

	observed := make(chan struct {
		method string
		body   string
		header string
	}, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		observed <- struct {
			method string
			body   string
			header string
		}{method: r.Method, body: string(body), header: r.Header.Get("X-Test")}
		w.Header().Set("X-Upstream", "copied")
		w.Header().Set("Connection", "X-Hop, close")
		w.Header().Set("X-Hop", "blocked")
		w.Header().Set("Keep-Alive", "timeout=5")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write([]byte("raw body"))
	}))
	defer upstream.Close()

	rec := api.do(http.MethodPost, "/proxies/direct/proxy", `{
		"url": `+jsonText(upstream.URL)+`,
		"headers": {"X-Test": "yes"},
		"body": "payload"
	}`)
	if rec.Code != http.StatusPartialContent {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusPartialContent, rec.Body.String())
	}
	if rec.Body.String() != "raw body" {
		t.Fatalf("upstream body = %q", rec.Body.String())
	}
	if rec.Header().Get("X-Upstream") != "copied" {
		t.Fatalf("expected upstream header to be copied, got %+v", rec.Header())
	}
	for _, hopHeader := range []string{"Connection", "Keep-Alive"} {
		if rec.Header().Get(hopHeader) != "" {
			t.Fatalf("hop-by-hop header %s leaked: %+v", hopHeader, rec.Header())
		}
	}
	gotRequest := <-observed
	if gotRequest.method != http.MethodGet || gotRequest.body != "payload" || gotRequest.header != "yes" {
		t.Fatalf("proxied request = %+v", gotRequest)
	}

	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, upstream.URL, http.StatusFound)
	}))
	defer redirect.Close()
	rec = api.do(http.MethodPost, "/proxies/direct/proxy", `{"url":`+jsonText(redirect.URL)+`,"follow-redirect":false}`)
	if rec.Code != http.StatusFound {
		t.Fatalf("redirect was followed or remapped: status=%d body=%s", rec.Code, rec.Body.String())
	}

	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad upstream", http.StatusServiceUnavailable)
	}))
	defer failing.Close()
	rec = api.do(http.MethodPost, "/proxies/direct/proxy", `{"url":`+jsonText(failing.URL)+`}`)
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "bad upstream") {
		t.Fatalf("upstream error was not streamed: status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestProxyHTTPRequestEndpointMapsPreResponseFailuresToJSONErrors(t *testing.T) {
	api := newServerHarness(t, directRecord("direct"))
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(50 * time.Millisecond):
			_, _ = w.Write([]byte("late"))
		}
	}))
	defer slow.Close()

	api.errorObject(http.MethodPost, "/proxies/missing/proxy", `{"url":`+jsonText(slow.URL)+`}`, http.StatusNotFound, "NOT_FOUND", "proxy not found")
	api.errorObject(http.MethodPost, "/proxies/direct/proxy", `{"url":"ftp://example.test/file"}`, http.StatusBadRequest, "BAD_REQUEST", "http or https")
	api.errorObject(http.MethodPost, "/proxies/direct/proxy", `{"url":`+jsonText(slow.URL)+`,"timeout":1}`, http.StatusGatewayTimeout, "GATEWAY_TIMEOUT", "proxy request timeout")
	api.errorObject(http.MethodPost, "/proxies/direct/proxy", `{"url":`+jsonText(slow.URL)+`,"method":""}`, http.StatusBadRequest, "BAD_REQUEST", "method cannot be empty")
	api.errorObject(http.MethodPost, "/proxies/direct/proxy", `{"url":`+jsonText(slow.URL)+`,"timeout":0}`, http.StatusBadRequest, "BAD_REQUEST", "greater than 0")
}

func TestProxyTestEndpointsAcceptOnlyDocumentedRequestShapes(t *testing.T) {
	emptyAPI := newServerHarness(t)
	emptyDelay := emptyAPI.jsonObject(http.MethodPost, "/proxies/delay", "", http.StatusOK)
	if emptyDelay["version"] != float64(0) || len(arrayField(t, emptyDelay, "results")) != 0 {
		t.Fatalf("empty delay response = %+v", emptyDelay)
	}
	emptyDownload := emptyAPI.jsonObject(http.MethodPost, "/proxies/download", `{"concurrency":1}`, http.StatusOK)
	if emptyDownload["version"] != float64(0) || len(arrayField(t, emptyDownload, "results")) != 0 {
		t.Fatalf("empty download response = %+v", emptyDownload)
	}
	emptyAPI.errorObject(http.MethodPost, "/proxies/missing/delay", "", http.StatusNotFound, "NOT_FOUND", "proxy not found")
	emptyAPI.errorObject(http.MethodPost, "/proxies/missing/download", "", http.StatusNotFound, "NOT_FOUND", "proxy not found")
	emptyAPI.errorObject(http.MethodPost, "/proxies/delay", `{"concurrency":0}`, http.StatusBadRequest, "BAD_REQUEST", "greater than 0")
	emptyAPI.errorObject(http.MethodPost, "/proxies/download", `{"concurrency":-2}`, http.StatusBadRequest, "BAD_REQUEST", "greater than 0")
	emptyAPI.errorObject(http.MethodPost, "/proxies/missing/delay", `{"concurrency":1}`, http.StatusBadRequest, "BAD_REQUEST", "unknown field")
	emptyAPI.errorObject(http.MethodPost, "/proxies/missing/download", `{"concurrency":1}`, http.StatusBadRequest, "BAD_REQUEST", "unknown field")

	var delayHeader, downloadHeader string
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodHead:
			delayHeader = r.Header.Get("X-Test")
			w.WriteHeader(http.StatusNoContent)
		case http.MethodGet:
			downloadHeader = r.Header.Get("X-Test")
			_, _ = w.Write([]byte(strings.Repeat("x", 256)))
		default:
			t.Errorf("unexpected method %s", r.Method)
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer target.Close()

	api := newServerHarness(t, directRecord("direct"))
	delayBody := `{
		"headers": {"X-Test": "delay"},
		"timeout": 1000,
		"follow-redirect": false,
		"expected": "200-299",
		"rounds": 1,
		"unified": false,
		"urls": [` + jsonText(target.URL) + `]
	}`
	singleDelay := api.jsonObject(http.MethodPost, "/proxies/direct/delay", delayBody, http.StatusOK)
	requireMetricSet(t, singleDelay, "delay")
	if singleDelay["version"] != float64(0) || singleDelay["digest"] != "direct" || delayHeader != "delay" {
		t.Fatalf("single delay response/header = %+v header=%q", singleDelay, delayHeader)
	}

	collectionDelay := api.jsonObject(http.MethodPost, "/proxies/delay", `{"concurrency":1,`+strings.TrimPrefix(strings.TrimSpace(delayBody), "{"), http.StatusOK)
	delayResults := arrayField(t, collectionDelay, "results")
	if len(delayResults) != 1 {
		t.Fatalf("delay collection = %+v", collectionDelay)
	}
	delayResult := objectField(t, delayResults[0], "")
	if _, leaked := delayResult["version"]; leaked {
		t.Fatalf("collection delay item includes version: %+v", delayResult)
	}
	requireMetricSet(t, delayResult, "delay")

	downloadBody := `{
		"headers": {"X-Test": "download"},
		"timeout": 1000,
		"follow-redirect": false,
		"rounds": 1,
		"max-bytes": 64,
		"urls": [` + jsonText(target.URL) + `]
	}`
	singleDownload := api.jsonObject(http.MethodPost, "/proxies/direct/download", downloadBody, http.StatusOK)
	requireMetricSet(t, singleDownload, "download")
	if singleDownload["version"] != float64(0) || singleDownload["digest"] != "direct" || downloadHeader != "download" {
		t.Fatalf("single download response/header = %+v header=%q", singleDownload, downloadHeader)
	}

	collectionDownload := api.jsonObject(http.MethodPost, "/proxies/download", `{"concurrency":1,`+strings.TrimPrefix(strings.TrimSpace(downloadBody), "{"), http.StatusOK)
	downloadResults := arrayField(t, collectionDownload, "results")
	if len(downloadResults) != 1 {
		t.Fatalf("download collection = %+v", collectionDownload)
	}
	downloadResult := objectField(t, downloadResults[0], "")
	if _, leaked := downloadResult["version"]; leaked {
		t.Fatalf("collection download item includes version: %+v", downloadResult)
	}
	requireMetricSet(t, downloadResult, "download")
}

func TestStrictJSONDecoderRejectsUnknownNullMalformedAndExtraValues(t *testing.T) {
	api := newServerHarness(t, directRecord("direct"))

	tests := []struct {
		name     string
		method   string
		path     string
		body     string
		contains string
	}{
		{name: "unknown import field", method: http.MethodPost, path: "/proxies/import", body: `{"type":"text","payload":"proxies: []","legacy":true}`, contains: "unknown field"},
		{name: "nested null", method: http.MethodPost, path: "/proxies/direct/download", body: `{"urls":[{"url":"https://example.test/file","max-bytes":null}]}`, contains: "cannot be null"},
		{name: "malformed json", method: http.MethodPost, path: "/proxies/direct/proxy", body: `{"url":`, contains: "unexpected EOF"},
		{name: "extra json value", method: http.MethodPost, path: "/digest", body: `{"type":"direct"} {}`, contains: "only one JSON value"},
		{name: "single delay disallows concurrency", method: http.MethodPost, path: "/proxies/direct/delay", body: `{"concurrency":1}`, contains: "unknown field"},
		{name: "single download disallows concurrency", method: http.MethodPost, path: "/proxies/direct/download", body: `{"concurrency":1}`, contains: "unknown field"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			api.errorObject(tt.method, tt.path, tt.body, http.StatusBadRequest, "BAD_REQUEST", tt.contains)
		})
	}
}

func TestDTOAndValidationHelpersKeepZeroMetricsAndCloneInputs(t *testing.T) {
	delayJSON, err := json.Marshal(delayResponseDTO{})
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{`"delay-min":0`, `"delay-max":0`, `"delay-avg":0`, `"delay-cost":0`, `"success":0`, `"failed":0`, `"total":0`} {
		if !strings.Contains(string(delayJSON), required) {
			t.Fatalf("delay zero field %s missing from %s", required, delayJSON)
		}
	}

	downloadJSON, err := json.Marshal(downloadResultResponseDTO{})
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{`"speed-min":0`, `"speed-max":0`, `"speed-avg":0`, `"speed-score":0`, `"success":0`, `"failed":0`, `"total":0`} {
		if !strings.Contains(string(downloadJSON), required) {
			t.Fatalf("download zero field %s missing from %s", required, downloadJSON)
		}
	}

	text := "GET"
	if got, err := requiredString("method", &text); err != nil || got != text {
		t.Fatalf("requiredString = %q, %v", got, err)
	}
	empty := ""
	if _, err := requiredString("method", &empty); err == nil || !strings.Contains(err.Error(), "required") {
		t.Fatalf("requiredString empty error = %v", err)
	}
	positive := 15
	gotPositive, err := optionalPositiveInt("timeout", &positive)
	if err != nil || gotPositive == nil || *gotPositive != 15 {
		t.Fatalf("optionalPositiveInt = %v, %v", gotPositive, err)
	}
	positive = 99
	if *gotPositive != 15 {
		t.Fatalf("optionalPositiveInt returned caller-owned pointer")
	}
	zero := 0
	if _, err := optionalPositiveInt("timeout", &zero); err == nil || !strings.Contains(err.Error(), "greater than 0") {
		t.Fatalf("optionalPositiveInt zero error = %v", err)
	}
	method := "POST"
	gotMethod, err := optionalNonEmptyString("method", &method)
	if err != nil || gotMethod == nil || *gotMethod != "POST" {
		t.Fatalf("optionalNonEmptyString = %v, %v", gotMethod, err)
	}
	method = "PATCH"
	if *gotMethod != "POST" {
		t.Fatalf("optionalNonEmptyString returned caller-owned pointer")
	}
	headers := map[string]string{"X-Test": "one"}
	clone := cloneHeaders(headers)
	clone["X-Test"] = "two"
	if headers["X-Test"] != "one" {
		t.Fatalf("cloneHeaders mutated source: %+v", headers)
	}
}

func TestCopyUpstreamHeadersDropsStandardAndConnectionNamedHopHeaders(t *testing.T) {
	dst := http.Header{}
	copyUpstreamHeaders(dst, http.Header{
		"Connection":        []string{"X-Internal, close"},
		"X-Internal":        []string{"secret"},
		"Transfer-Encoding": []string{"chunked"},
		"Upgrade":           []string{"websocket"},
		"X-End-To-End":      []string{"kept"},
	})
	if dst.Get("X-End-To-End") != "kept" {
		t.Fatalf("end-to-end header missing: %+v", dst)
	}
	for _, blocked := range []string{"Connection", "X-Internal", "Transfer-Encoding", "Upgrade"} {
		if dst.Get(blocked) != "" {
			t.Fatalf("blocked header %s copied: %+v", blocked, dst)
		}
	}
}

type serverHarness struct {
	t       *testing.T
	handler http.Handler
}

func newServerHarness(t *testing.T, records ...*proxyconfig.Record) serverHarness {
	t.Helper()
	runtime, err := app.New(config.Default(), records)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(runtime.Close)
	return serverHarness{t: t, handler: New(runtime)}
}

func (h serverHarness) do(method, path, body string) *httptest.ResponseRecorder {
	h.t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.handler.ServeHTTP(rec, req)
	return rec
}

func (h serverHarness) jsonObject(method, path, body string, status int) map[string]any {
	h.t.Helper()
	rec := h.do(method, path, body)
	requireStatus(h.t, rec, status)
	var response map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		h.t.Fatalf("decode %s %s response: %v; body=%s", method, path, err, rec.Body.String())
	}
	return response
}

func (h serverHarness) errorObject(method, path, body string, status int, apiStatus, contains string) {
	h.t.Helper()
	rec := h.do(method, path, body)
	requireStatus(h.t, rec, status)
	if status == http.StatusOK {
		return
	}
	var response struct {
		Error struct {
			Code    int    `json:"code"`
			Status  string `json:"status"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		h.t.Fatalf("decode error response: %v; body=%s", err, rec.Body.String())
	}
	if response.Error.Code != status || response.Error.Status != apiStatus {
		h.t.Fatalf("error metadata = %+v, want code=%d status=%s", response.Error, status, apiStatus)
	}
	if contains != "" && !strings.Contains(response.Error.Message, contains) {
		h.t.Fatalf("error message = %q, want substring %q", response.Error.Message, contains)
	}
}

func requireStatus(t *testing.T, rec *httptest.ResponseRecorder, want int) {
	t.Helper()
	if rec.Code != want {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, want, rec.Body.String())
	}
	if contentType := rec.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "application/json") {
		t.Fatalf("content-type = %q, want JSON", contentType)
	}
}

func objectField(t *testing.T, value any, key string) map[string]any {
	t.Helper()
	var raw any
	if key == "" {
		raw = value
	} else {
		parent, ok := value.(map[string]any)
		if !ok {
			t.Fatalf("value is not object while looking for %q: %+v", key, value)
		}
		raw = parent[key]
	}
	object, ok := raw.(map[string]any)
	if !ok {
		t.Fatalf("field %q is not object: %+v", key, raw)
	}
	return object
}

func arrayField(t *testing.T, object map[string]any, key string) []any {
	t.Helper()
	values, ok := object[key].([]any)
	if !ok {
		t.Fatalf("field %q is not array: %+v", key, object[key])
	}
	return values
}

func requireMetricSet(t *testing.T, object map[string]any, kind string) {
	t.Helper()
	var fields []string
	switch kind {
	case "delay":
		fields = []string{"digest", "delay-min", "delay-max", "delay-avg", "delay-cost", "success", "failed", "total"}
	case "download":
		fields = []string{"digest", "speed-min", "speed-max", "speed-avg", "speed-score", "success", "failed", "total"}
	default:
		t.Fatalf("unknown metric kind %q", kind)
	}
	for _, field := range fields {
		if _, ok := object[field]; !ok {
			t.Fatalf("metric field %q missing from %+v", field, object)
		}
	}
	if object["total"] != float64(1) {
		t.Fatalf("total = %v, want 1 in %+v", object["total"], object)
	}
}

func jsonText(value string) string {
	buf, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return string(buf)
}

func proxyYAML(name, server string) string {
	return "proxies:\n" +
		"  - name: " + name + "\n" +
		"    type: ss\n" +
		"    server: " + server + "\n" +
		"    port: 8388\n" +
		"    cipher: aes-128-gcm\n" +
		"    password: secret\n"
}

func directRecord(id string) *proxyconfig.Record {
	return &proxyconfig.Record{
		Digest: id,
		Raw: map[string]any{
			"name":   id,
			"type":   "direct",
			"server": "direct",
			"port":   0,
		},
		Proxy: passthroughProxy{name: id},
	}
}

type passthroughProxy struct {
	name string
}

func (p passthroughProxy) Name() string { return p.name }
func (passthroughProxy) Type() C.AdapterType {
	return C.Direct
}
func (passthroughProxy) Addr() string           { return "direct" }
func (passthroughProxy) SupportUDP() bool       { return false }
func (passthroughProxy) SupportUOT() bool       { return false }
func (passthroughProxy) ProxyInfo() C.ProxyInfo { return C.ProxyInfo{} }
func (p passthroughProxy) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]string{"name": p.name, "type": "direct"})
}
func (p passthroughProxy) DialContext(ctx context.Context, metadata *C.Metadata) (C.Conn, error) {
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", metadata.RemoteAddress())
	if err != nil {
		return nil, err
	}
	return outbound.NewConn(conn, p), nil
}
func (passthroughProxy) ListenPacketContext(context.Context, *C.Metadata) (C.PacketConn, error) {
	return nil, C.ErrNotSupport
}
func (passthroughProxy) IsL3Protocol(*C.Metadata) bool { return false }
func (passthroughProxy) Unwrap(*C.Metadata, bool) C.Proxy {
	return nil
}
func (passthroughProxy) Close() error                   { return nil }
func (p passthroughProxy) Adapter() C.ProxyAdapter      { return p }
func (passthroughProxy) AliveForTestUrl(string) bool    { return true }
func (passthroughProxy) DelayHistory() []C.DelayHistory { return nil }
func (passthroughProxy) ExtraDelayHistories() map[string]C.ProxyState {
	return nil
}
func (passthroughProxy) LastDelayForTestUrl(string) uint16 { return 0 }
func (passthroughProxy) URLTest(context.Context, string, utils.IntRanges[uint16]) (uint16, error) {
	return 0, C.ErrNotSupport
}

var _ C.Proxy = passthroughProxy{}
