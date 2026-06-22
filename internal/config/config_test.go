package config

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestDefaultConfigMatchesDocumentedRuntimeShape(t *testing.T) {
	cfg := Default()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("default config is invalid: %v", err)
	}

	if cfg.DefaultTimeout != 5000 {
		t.Fatalf("default-timeout = %d", cfg.DefaultTimeout)
	}
	if cfg.ProxyServer.Expand || cfg.ProxyServer.Timeout != 5000 || !reflect.DeepEqual(cfg.ProxyServer.Nameservers, []string{"system"}) {
		t.Fatalf("proxy-server defaults = %+v", cfg.ProxyServer)
	}
	if len(cfg.Delay.URLs) != 1 ||
		cfg.Delay.URLs[0].URL != "https://www.google.com/generate_204" ||
		cfg.Delay.Timeout != 10000 ||
		!cfg.Delay.FollowRedirect ||
		cfg.Delay.Expected != "200-299" ||
		cfg.Delay.Rounds != 2 ||
		cfg.Delay.Concurrency != 10 ||
		!cfg.Delay.Unified {
		t.Fatalf("delay defaults = %+v", cfg.Delay)
	}
	if len(cfg.Download.URLs) != 1 ||
		cfg.Download.URLs[0].URL != "https://cachefly.cachefly.net/50mb.test" ||
		cfg.Download.Timeout != 15000 ||
		!cfg.Download.FollowRedirect ||
		cfg.Download.Rounds != 1 ||
		cfg.Download.MaxBytes != 50*1024*1024 ||
		cfg.Download.Concurrency != 1 {
		t.Fatalf("download defaults = %+v", cfg.Download)
	}
}

func TestCloneDeepCopiesMutableConfigFields(t *testing.T) {
	follow := false
	unified := false
	cfg := Default()
	cfg.ProxyServer.Nameservers = []string{"system", "1.1.1.1"}
	cfg.Delay.Headers = map[string]string{"X-Delay": "root"}
	cfg.Delay.URLs[0].Headers = map[string]string{"X-Delay-URL": "item"}
	cfg.Delay.URLs[0].FollowRedirect = &follow
	cfg.Delay.URLs[0].Unified = &unified
	cfg.Download.Headers = map[string]string{"X-Download": "root"}
	cfg.Download.URLs[0].Headers = map[string]string{"X-Download-URL": "item"}
	cfg.Download.URLs[0].FollowRedirect = &follow

	clone := cfg.Clone()
	clone.ProxyServer.Nameservers[0] = "changed"
	clone.Delay.Headers["X-Delay"] = "changed"
	clone.Delay.URLs[0].Headers["X-Delay-URL"] = "changed"
	*clone.Delay.URLs[0].FollowRedirect = true
	*clone.Delay.URLs[0].Unified = true
	clone.Download.Headers["X-Download"] = "changed"
	clone.Download.URLs[0].Headers["X-Download-URL"] = "changed"
	*clone.Download.URLs[0].FollowRedirect = true

	if cfg.ProxyServer.Nameservers[0] != "system" ||
		cfg.Delay.Headers["X-Delay"] != "root" ||
		cfg.Delay.URLs[0].Headers["X-Delay-URL"] != "item" ||
		*cfg.Delay.URLs[0].FollowRedirect ||
		*cfg.Delay.URLs[0].Unified ||
		cfg.Download.Headers["X-Download"] != "root" ||
		cfg.Download.URLs[0].Headers["X-Download-URL"] != "item" ||
		*cfg.Download.URLs[0].FollowRedirect {
		t.Fatalf("clone mutation reached original config: %+v", cfg)
	}
}

func TestToAPIUsesKebabCaseAndPreservesFalseValues(t *testing.T) {
	cfg := Default()
	cfg.ProxyServer.Expand = false
	cfg.Delay.FollowRedirect = false
	cfg.Delay.Unified = false
	cfg.Download.FollowRedirect = false

	raw := marshalAPIToMap(t, cfg)
	for _, key := range []string{"default-timeout", "proxy-server", "delay", "download"} {
		if _, ok := raw[key]; !ok {
			t.Fatalf("missing API key %q in %+v", key, raw)
		}
	}
	if _, leaked := raw["delay-timeout"]; leaked {
		t.Fatalf("flat legacy config key leaked into API: %+v", raw)
	}

	proxyServer := raw["proxy-server"].(map[string]any)
	delay := raw["delay"].(map[string]any)
	download := raw["download"].(map[string]any)
	if proxyServer["expand"] != false ||
		delay["follow-redirect"] != false ||
		delay["unified"] != false ||
		download["follow-redirect"] != false {
		t.Fatalf("false API values omitted or changed: %+v", raw)
	}
	if _, ok := delay["headers"]; ok {
		t.Fatalf("empty optional headers should be omitted: %+v", delay)
	}
}

func TestAPIMappingCopiesNestedURLPointersAndMaps(t *testing.T) {
	follow := false
	unified := false
	cfg := Default()
	cfg.Delay.URLs = []DelayURL{{
		URL:            "https://delay.example/generate_204",
		Timeout:        1234,
		Headers:        map[string]string{"X-Delay": "item"},
		FollowRedirect: &follow,
		Expected:       "204",
		Rounds:         3,
		Unified:        &unified,
	}}
	api := ToAPI(cfg)
	api.Delay.URLs[0].Headers["X-Delay"] = "changed"
	*api.Delay.URLs[0].FollowRedirect = true
	*api.Delay.URLs[0].Unified = true

	if cfg.Delay.URLs[0].Headers["X-Delay"] != "item" ||
		*cfg.Delay.URLs[0].FollowRedirect ||
		*cfg.Delay.URLs[0].Unified {
		t.Fatalf("API projection shares nested mutable state with config: %+v", cfg.Delay.URLs[0])
	}
}

func TestPatchAPIDeepMergesObjectsReplacesArraysAndStoresOnlyValidConfig(t *testing.T) {
	store := newDefaultStore(t)
	first, err := store.PatchAPI(rawPatch(map[string]string{
		"delay":           `{"headers":{"X-Root":"delay"},"urls":["https://delay.example/one"],"rounds":3}`,
		"download":        `{"headers":{"X-Root":"download"},"concurrency":2}`,
		"proxy-server":    `{"expand":true}`,
		"default-timeout": `7000`,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if first.DefaultTimeout != 7000 ||
		!first.ProxyServer.Expand ||
		first.ProxyServer.Timeout != DefaultProxyServerTimeout ||
		first.Delay.Headers["X-Root"] != "delay" ||
		first.Delay.Rounds != 3 ||
		first.Delay.Timeout != DefaultDelayTimeout ||
		len(first.Delay.URLs) != 1 ||
		first.Delay.URLs[0].URL != "https://delay.example/one" ||
		first.Download.Headers["X-Root"] != "download" ||
		first.Download.Concurrency != 2 {
		t.Fatalf("patched config = %+v", first)
	}

	if _, err := store.PatchAPI(rawPatch(map[string]string{"download": `{"concurrency":0}`})); err == nil {
		t.Fatal("invalid patch succeeded")
	}
	afterFailure := store.Snapshot()
	if afterFailure.Download.Concurrency != 2 || afterFailure.DefaultTimeout != 7000 {
		t.Fatalf("failed patch mutated stored config: %+v", afterFailure)
	}
}

func TestPatchAPIPreservesExplicitFalseAndRejectsExplicitZeroPositiveFields(t *testing.T) {
	store := newDefaultStore(t)
	next, err := store.PatchAPI(rawPatch(map[string]string{
		"delay":        `{"follow-redirect":false,"unified":false}`,
		"download":     `{"follow-redirect":false}`,
		"proxy-server": `{"expand":false}`,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if next.Delay.FollowRedirect || next.Delay.Unified || next.Download.FollowRedirect || next.ProxyServer.Expand {
		t.Fatalf("explicit false values were not preserved: %+v", next)
	}

	invalid := []map[string]string{
		{"default-timeout": `0`},
		{"proxy-server": `{"timeout":0}`},
		{"delay": `{"timeout":0}`},
		{"delay": `{"rounds":0}`},
		{"delay": `{"concurrency":0}`},
		{"download": `{"timeout":0}`},
		{"download": `{"rounds":0}`},
		{"download": `{"max-bytes":0}`},
		{"download": `{"concurrency":0}`},
	}
	for _, patch := range invalid {
		if _, err := store.PatchAPI(rawPatch(patch)); err == nil {
			t.Fatalf("PatchAPI(%v) succeeded", patch)
		}
	}
}

func TestPatchAPIRejectsZeroURLItemOverridesButAllowsOmittedFallbackSentinels(t *testing.T) {
	store := newDefaultStore(t)
	invalid := []map[string]string{
		{"delay": `{"urls":[{"url":"https://delay.example","timeout":0}]}`},
		{"delay": `{"urls":[{"url":"https://delay.example","rounds":0}]}`},
		{"download": `{"urls":[{"url":"https://download.example/file","timeout":0}]}`},
		{"download": `{"urls":[{"url":"https://download.example/file","rounds":0}]}`},
		{"download": `{"urls":[{"url":"https://download.example/file","max-bytes":0}]}`},
	}
	for _, patch := range invalid {
		if _, err := store.PatchAPI(rawPatch(patch)); err == nil {
			t.Fatalf("zero URL item override patch succeeded: %v", patch)
		}
	}

	next, err := store.PatchAPI(rawPatch(map[string]string{
		"delay":    `{"urls":[{"url":"https://delay.example"}]}`,
		"download": `{"urls":[{"url":"https://download.example/file"}]}`,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if next.Delay.URLs[0].Timeout != 0 || next.Delay.URLs[0].Rounds != 0 {
		t.Fatalf("delay URL fallback sentinels changed: %+v", next.Delay.URLs[0])
	}
	if next.Download.URLs[0].Timeout != 0 || next.Download.URLs[0].Rounds != 0 || next.Download.URLs[0].MaxBytes != 0 {
		t.Fatalf("download URL fallback sentinels changed: %+v", next.Download.URLs[0])
	}
}

func TestPatchAPIRejectsUnknownNullTypeMismatchAndInvalidURLs(t *testing.T) {
	store := newDefaultStore(t)
	cases := []struct {
		name    string
		patch   map[string]string
		message string
	}{
		{name: "unknown root", patch: map[string]string{"unexpected": `1`}, message: "unexpected"},
		{name: "null section", patch: map[string]string{"delay": `null`}, message: "null"},
		{name: "nested null", patch: map[string]string{"delay": `{"urls":[null]}`}, message: "null"},
		{name: "type mismatch", patch: map[string]string{"delay": `{"rounds":"2"}`}, message: "cannot decode"},
		{name: "invalid delay url", patch: map[string]string{"delay": `{"urls":["ftp://delay.example"]}`}, message: "http_url"},
		{name: "empty nameserver", patch: map[string]string{"proxy-server": `{"nameservers":[]}`}, message: "min"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := store.PatchAPI(rawPatch(tc.patch)); err == nil || !strings.Contains(err.Error(), tc.message) {
				t.Fatalf("PatchAPI error = %v, want containing %q", err, tc.message)
			}
		})
	}
}

func TestPatchAPIAcceptsStringAndObjectURLItems(t *testing.T) {
	store := newDefaultStore(t)
	followFalse := false
	unifiedFalse := false
	next, err := store.PatchAPI(rawPatch(map[string]string{
		"delay":    `{"urls":["https://one.example",{"url":"https://two.example","headers":{"X-Test":"delay"},"timeout":500,"follow-redirect":false,"expected":"204","rounds":4,"unified":false}]}`,
		"download": `{"urls":["https://one.example/file",{"url":"https://two.example/file","headers":{"X-Test":"download"},"follow-redirect":false,"rounds":2,"max-bytes":1024}]}`,
	}))
	if err != nil {
		t.Fatal(err)
	}
	wantDelay := []DelayURL{
		{URL: "https://one.example"},
		{
			URL:            "https://two.example",
			Headers:        map[string]string{"X-Test": "delay"},
			Timeout:        500,
			FollowRedirect: &followFalse,
			Expected:       "204",
			Rounds:         4,
			Unified:        &unifiedFalse,
		},
	}
	if !reflect.DeepEqual(next.Delay.URLs, wantDelay) {
		t.Fatalf("delay URLs = %#v, want %#v", next.Delay.URLs, wantDelay)
	}
	if len(next.Download.URLs) != 2 ||
		next.Download.URLs[0].URL != "https://one.example/file" ||
		next.Download.URLs[1].Headers["X-Test"] != "download" ||
		next.Download.URLs[1].FollowRedirect == nil ||
		*next.Download.URLs[1].FollowRedirect ||
		next.Download.URLs[1].Rounds != 2 ||
		next.Download.URLs[1].MaxBytes != 1024 {
		t.Fatalf("download URLs = %+v", next.Download.URLs)
	}
}

func TestRuntimeURLItemTypesDoNotOwnJSONCompatibility(t *testing.T) {
	var delayURLs []DelayURL
	if err := json.Unmarshal([]byte(`["https://example.test"]`), &delayURLs); err == nil {
		t.Fatal("DelayURL direct JSON decoding accepted string shorthand")
	}
	var downloadURLs []DownloadURL
	if err := json.Unmarshal([]byte(`["https://example.test/file"]`), &downloadURLs); err == nil {
		t.Fatal("DownloadURL direct JSON decoding accepted string shorthand")
	}
}

func TestConfigUsesIntForByteAndTimeoutQuantities(t *testing.T) {
	cfg := Default()
	var _ int = cfg.DefaultTimeout
	var _ int = cfg.Delay.Timeout
	var _ int = cfg.Download.MaxBytes
	var _ int = cfg.Download.URLs[0].MaxBytes

	api := ToAPI(cfg)
	var _ int = api.Delay.Timeout
	var _ int = api.Download.MaxBytes
}

func newDefaultStore(t *testing.T) *Store {
	t.Helper()
	store, err := NewStore(Default())
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func rawPatch(values map[string]string) map[string]json.RawMessage {
	out := make(map[string]json.RawMessage, len(values))
	for key, value := range values {
		out[key] = json.RawMessage(value)
	}
	return out
}

func marshalAPIToMap(t *testing.T, cfg Config) map[string]any {
	t.Helper()
	buf, err := json.Marshal(ToAPI(cfg))
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(buf, &out); err != nil {
		t.Fatal(err)
	}
	return out
}
