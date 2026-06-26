package digest

import (
	"regexp"
	"strings"
	"testing"
)

var digestShape = regexp.MustCompile(`^[0-9a-f]{16}_[0-9a-f]{16}$`)

func TestSumProducesCanonicalAndEndpointParts(t *testing.T) {
	got, err := Sum(map[string]any{
		"type":   "ss",
		"server": "node.example",
		"port":   8388,
		"cipher": "aes-128-gcm",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !digestShape.MatchString(got) {
		t.Fatalf("digest %q does not match canonical_endpoint format", got)
	}
	if parts := strings.Split(got, "_"); len(parts) != 2 || parts[0] == parts[1] {
		t.Fatalf("digest parts = %v, want two independently derived parts", parts)
	}
}

func TestSumExcludesOnlyTopLevelDisplayAndReservedFields(t *testing.T) {
	base := map[string]any{
		"name":     "visible-a",
		"type":     "vmess",
		"server":   "edge.example",
		"port":     443,
		"metadata": map[string]any{"owner": "a"},
		"_local":   "a",
		"network": map[string]any{
			"name":     "nested-name",
			"metadata": "nested-metadata",
			"_local":   "nested-local",
		},
	}
	renamed := map[string]any{
		"name":     "visible-b",
		"type":     "vmess",
		"server":   "edge.example",
		"port":     443,
		"metadata": map[string]any{"owner": "b"},
		"_local":   "b",
		"network": map[string]any{
			"name":     "nested-name",
			"metadata": "nested-metadata",
			"_local":   "nested-local",
		},
	}

	first, err := Sum(base)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Sum(renamed)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("top-level name/metadata/_ fields changed digest: %q != %q", first, second)
	}

	withNestedChange := map[string]any{
		"type":   "vmess",
		"server": "edge.example",
		"port":   443,
		"network": map[string]any{
			"name":     "different",
			"metadata": "nested-metadata",
			"_local":   "nested-local",
		},
	}
	third, err := Sum(withNestedChange)
	if err != nil {
		t.Fatal(err)
	}
	if third == first {
		t.Fatalf("nested display/reserved fields must remain part of canonical digest: %q", third)
	}
}

func TestEndpointPartDependsOnlyOnTypeServerPortTuple(t *testing.T) {
	reference, err := Sum(map[string]any{
		"name":   "node-a",
		"type":   "trojan",
		"server": "proxy.example",
		"port":   443,
		"sni":    "one.example",
	})
	if err != nil {
		t.Fatal(err)
	}
	sameEndpoint, err := Sum(map[string]any{
		"name":   "node-b",
		"type":   "trojan",
		"server": "proxy.example",
		"port":   443,
		"sni":    "two.example",
	})
	if err != nil {
		t.Fatal(err)
	}
	changedEndpoint, err := Sum(map[string]any{
		"type":   "trojan",
		"server": "other.example",
		"port":   443,
	})
	if err != nil {
		t.Fatal(err)
	}

	refEndpoint := endpointPart(reference)
	if endpointPart(sameEndpoint) != refEndpoint {
		t.Fatalf("non-endpoint fields changed endpoint digest: %q vs %q", endpointPart(sameEndpoint), refEndpoint)
	}
	if endpointPart(changedEndpoint) == refEndpoint {
		t.Fatalf("server change did not affect endpoint digest: %q", changedEndpoint)
	}
}

func TestMissingEndpointValuesUseUndefinedLiteral(t *testing.T) {
	missingPort, err := Sum(map[string]any{"type": "direct", "server": "direct"})
	if err != nil {
		t.Fatal(err)
	}
	explicitUndefined, err := Sum(map[string]any{"type": "direct", "server": "direct", "port": "undefined"})
	if err != nil {
		t.Fatal(err)
	}
	if endpointPart(missingPort) != endpointPart(explicitUndefined) {
		t.Fatalf("missing endpoint value did not match explicit undefined literal")
	}
}

func TestMapNormalizationIsStableAndDoesNotMutateInput(t *testing.T) {
	nested := map[any]any{
		1:       "one",
		"two":   2,
		"child": []any{map[any]any{3: "three"}},
	}
	raw := map[string]any{"type": "custom", "nested": nested}

	got, err := Sum(raw)
	if err != nil {
		t.Fatal(err)
	}
	want, err := Sum(map[string]any{
		"type": "custom",
		"nested": map[string]any{
			"1":     "one",
			"two":   2,
			"child": []any{map[string]any{"3": "three"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("mixed-key map normalization mismatch: %q != %q", got, want)
	}
	if _, ok := raw["nested"].(map[any]any); !ok || len(nested) != 3 {
		t.Fatalf("Sum mutated original map: %#v", raw)
	}
}

func TestSumReturnsMarshalErrors(t *testing.T) {
	if _, err := Sum(map[string]any{"type": "custom", "bad": make(chan int)}); err == nil {
		t.Fatal("expected unsupported value to fail canonical marshaling")
	}
}

func endpointPart(value string) string {
	return strings.Split(value, "_")[1]
}
