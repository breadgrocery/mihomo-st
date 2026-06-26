package digest

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

func Sum(mapping map[string]any) (string, error) {
	canonical, err := canonicalMapping(mapping)
	if err != nil {
		return "", err
	}

	canonicalSum := sha256.Sum256(canonical)
	endpointSum := sha256.Sum256([]byte(fmt.Sprintf("%v_%v_%v",
		endpointValue(mapping, "type"),
		endpointValue(mapping, "server"),
		endpointValue(mapping, "port"),
	)))

	return fmt.Sprintf("%s_%s",
		hex.EncodeToString(canonicalSum[:])[:16],
		hex.EncodeToString(endpointSum[:])[:16],
	), nil
}

func endpointValue(mapping map[string]any, key string) any {
	value, ok := mapping[key]
	if !ok {
		return "undefined"
	}
	return value
}

func canonicalMapping(mapping map[string]any) ([]byte, error) {
	normalized := normalize(mapping, true)
	return json.Marshal(normalized)
}

func normalize(value any, topLevel bool) any {
	switch v := value.(type) {
	case map[string]any:
		normalized := make(map[string]any, len(v))
		for key, item := range v {
			if excludeKey(key, topLevel) {
				continue
			}
			normalized[key] = normalize(item, false)
		}
		return normalized
	case map[any]any:
		normalized := make(map[string]any, len(v))
		for key, item := range v {
			stringKey := fmt.Sprint(key)
			if excludeKey(stringKey, topLevel) {
				continue
			}
			normalized[stringKey] = normalize(item, false)
		}
		return normalized
	case []any:
		normalized := make([]any, 0, len(v))
		for _, item := range v {
			normalized = append(normalized, normalize(item, false))
		}
		return normalized
	default:
		return value
	}
}

func excludeKey(key string, topLevel bool) bool {
	if !topLevel {
		return false
	}
	return key == "name" || key == "metadata" || (key != "" && key[0] == '_')
}
