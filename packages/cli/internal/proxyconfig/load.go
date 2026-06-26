package proxyconfig

import (
	"github.com/metacubex/mihomo/adapter"
	C "github.com/metacubex/mihomo/constant"
	"gopkg.in/yaml.v3"
	"mihomo-st/internal/digest"
)

type rawConfig struct {
	Proxies []map[string]any `yaml:"proxies"`
}

type Record struct {
	Digest string
	Raw    map[string]any
	Proxy  C.Proxy
}

type Warning struct {
	Index   int    `json:"index,omitempty"`
	Message string `json:"message"`
}

type Result struct {
	Records  []*Record `json:"-"`
	Warnings []Warning `json:"warnings,omitempty"`
}

func Load(buf []byte) (Result, error) {
	var raw rawConfig
	if err := yaml.Unmarshal(buf, &raw); err != nil {
		return Result{}, err
	}
	return parseProxyMappings(raw.Proxies), nil
}

func parseProxyMappings(mappings []map[string]any) Result {
	records := make([]*Record, 0, len(mappings))
	warnings := make([]Warning, 0)
	byDigest := map[string]*Record{}
	for idx, mapping := range mappings {
		record, warning := parseProxyRecord(idx, mapping)
		if warning != nil {
			warnings = append(warnings, *warning)
			continue
		}
		if byDigest[record.Digest] != nil {
			closeRecord(record)
			continue
		}

		records = append(records, record)
		byDigest[record.Digest] = record
	}
	return Result{Records: records, Warnings: warnings}
}

func parseProxyRecord(idx int, mapping map[string]any) (*Record, *Warning) {
	sum, err := digest.Sum(mapping)
	if err != nil {
		return nil, &Warning{Index: idx, Message: err.Error()}
	}

	proxy, err := adapter.ParseProxy(cloneMap(mapping))
	if err != nil {
		return nil, &Warning{Index: idx, Message: err.Error()}
	}

	return &Record{
		Digest: sum,
		Raw:    mapping,
		Proxy:  proxy,
	}, nil
}

func cloneMap(mapping map[string]any) map[string]any {
	return deepClone(mapping).(map[string]any)
}

func deepClone(value any) any {
	switch v := value.(type) {
	case map[string]any:
		clone := make(map[string]any, len(v))
		for key, item := range v {
			clone[key] = deepClone(item)
		}
		return clone
	case []any:
		clone := make([]any, len(v))
		for i, item := range v {
			clone[i] = deepClone(item)
		}
		return clone
	default:
		return value
	}
}

func closeRecord(record *Record) {
	if record == nil || record.Proxy == nil {
		return
	}
	_ = record.Proxy.Close()
}
