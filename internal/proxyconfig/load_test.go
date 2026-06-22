package proxyconfig

import (
	"encoding/json"
	"strings"
	"testing"

	"mihomo-st/internal/digest"
)

func TestLoadReadsOnlyTopLevelProxies(t *testing.T) {
	cases := []string{
		"mixed-port: 7890\nrules:\n  - MATCH,DIRECT\n",
		"proxies: []\nproxy-groups:\n  - name: ignored\n",
	}
	for _, input := range cases {
		result, err := Load([]byte(input))
		if err != nil {
			t.Fatalf("Load(%q) error = %v", input, err)
		}
		if len(result.Records) != 0 || len(result.Warnings) != 0 {
			t.Fatalf("Load(%q) = %+v", input, result)
		}
	}
}

func TestLoadReturnsWholeYAMLParseError(t *testing.T) {
	result, err := Load([]byte("proxies:\n  - name: ["))
	if err == nil {
		t.Fatal("malformed YAML returned nil error")
	}
	if len(result.Records) != 0 || len(result.Warnings) != 0 {
		t.Fatalf("error result should be empty: %+v", result)
	}
}

func TestParseProxyMappingsKeepsValidRecordsAndWarnsForInvalidItems(t *testing.T) {
	result := parseProxyMappings([]map[string]any{
		validProxy("first", nil),
		validProxy("same-digest-different-name", nil),
		{"name": "bad-type", "type": "not-a-real-proxy"},
		validProxy("second", map[string]any{"server": "second.example"}),
	})
	defer closeRecords(result.Records)

	if len(result.Records) != 2 {
		t.Fatalf("records = %d, want 2: %+v", len(result.Records), result)
	}
	if result.Records[0].Raw["name"] != "first" {
		t.Fatalf("first duplicate was not retained: %+v", result.Records[0].Raw)
	}
	if len(result.Warnings) != 1 || result.Warnings[0].Index != 2 || result.Warnings[0].Message == "" {
		t.Fatalf("warnings = %+v", result.Warnings)
	}
	if result.Records[0].Digest == result.Records[1].Digest {
		t.Fatalf("distinct endpoints produced duplicate digest: %q", result.Records[0].Digest)
	}
}

func TestParseProxyMappingsReportsDigestFailuresBeforeProxyConstruction(t *testing.T) {
	result := parseProxyMappings([]map[string]any{{
		"name": "bad-canonical",
		"type": "direct",
		"bad":  func() {},
	}})
	if len(result.Records) != 0 || len(result.Warnings) != 1 || result.Warnings[0].Index != 0 {
		t.Fatalf("digest failure result = %+v", result)
	}
}

func TestParseProxyMappingsDoesNotMutateRawMapping(t *testing.T) {
	raw := validProxy("node", map[string]any{
		"metadata": map[string]any{"tag": "kept"},
		"_local":   "kept",
		"nested":   []any{map[string]any{"k": "v"}},
	})
	before := mustJSON(t, raw)

	result := parseProxyMappings([]map[string]any{raw})
	defer closeRecords(result.Records)
	if len(result.Warnings) != 0 || len(result.Records) != 1 {
		t.Fatalf("parse result = %+v", result)
	}
	if after := mustJSON(t, raw); after != before {
		t.Fatalf("raw proxy mapping mutated: before=%s after=%s", before, after)
	}
	if result.Records[0].Raw["metadata"] == nil || result.Records[0].Raw["_local"] == nil {
		t.Fatalf("record raw lost display/reserved fields: %+v", result.Records[0].Raw)
	}
}

func TestRecordDigestDelegatesToDigestPackage(t *testing.T) {
	raw := validProxy("digest-node", map[string]any{"server": "digest.example"})
	result := parseProxyMappings([]map[string]any{raw})
	defer closeRecords(result.Records)
	if len(result.Records) != 1 || len(result.Warnings) != 0 {
		t.Fatalf("parse result = %+v", result)
	}
	want, err := digest.Sum(raw)
	if err != nil {
		t.Fatal(err)
	}
	if result.Records[0].Digest != want {
		t.Fatalf("record digest = %q, want %q", result.Records[0].Digest, want)
	}
}

func TestCloneMapRecursivelyCopiesMapsAndSlices(t *testing.T) {
	original := map[string]any{
		"level": map[string]any{"name": "root"},
		"items": []any{map[string]any{"name": "child"}},
	}
	clone := cloneMap(original)
	clone["level"].(map[string]any)["name"] = "changed"
	clone["items"].([]any)[0].(map[string]any)["name"] = "changed"
	if original["level"].(map[string]any)["name"] != "root" ||
		original["items"].([]any)[0].(map[string]any)["name"] != "child" {
		t.Fatalf("clone mutation reached original: %+v", original)
	}
	if empty := cloneMap(nil); len(empty) != 0 {
		t.Fatalf("cloneMap(nil) = %+v", empty)
	}
}

func TestRecordJSONUsesDigestTerminology(t *testing.T) {
	result := parseProxyMappings([]map[string]any{validProxy("json-node", nil)})
	defer closeRecords(result.Records)
	buf, err := json.Marshal(result.Records[0])
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ToLower(string(buf)), "hash") {
		t.Fatalf("record JSON contains old hash terminology: %s", buf)
	}
}

func TestCloseRecordAcceptsNilInputs(t *testing.T) {
	closeRecord(nil)
	closeRecord(&Record{})
}

func validProxy(name string, extra map[string]any) map[string]any {
	mapping := map[string]any{
		"name":     name,
		"type":     "ss",
		"server":   "example.com",
		"port":     8388,
		"cipher":   "aes-128-gcm",
		"password": "password",
	}
	for key, value := range extra {
		mapping[key] = value
	}
	return mapping
}

func configYAML(name string) string {
	return "proxies:\n" +
		"  - name: " + name + "\n" +
		"    type: ss\n" +
		"    server: example.com\n" +
		"    port: 8388\n" +
		"    cipher: aes-128-gcm\n" +
		"    password: password\n"
}

func closeRecords(records []*Record) {
	for _, record := range records {
		closeRecord(record)
	}
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	buf, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(buf)
}
