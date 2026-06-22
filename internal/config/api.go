package config

import (
	"encoding/json"
	"fmt"
	"strconv"
)

type APIConfig struct {
	DefaultTimeout int            `json:"default-timeout"`
	ProxyServer    APIProxyServer `json:"proxy-server"`
	Delay          APIDelay       `json:"delay"`
	Download       APIDownload    `json:"download"`
}

type APIProxyServer struct {
	Expand      bool     `json:"expand"`
	Nameservers []string `json:"nameservers"`
	Timeout     int      `json:"timeout"`
}

type APIDelay struct {
	URLs           []APIDelayURL     `json:"urls"`
	Timeout        int               `json:"timeout"`
	Headers        map[string]string `json:"headers,omitempty"`
	FollowRedirect bool              `json:"follow-redirect"`
	Expected       string            `json:"expected"`
	Rounds         int               `json:"rounds"`
	Concurrency    int               `json:"concurrency"`
	Unified        bool              `json:"unified"`
}

type APIDelayURL struct {
	URL            string            `json:"url"`
	Timeout        int               `json:"timeout,omitempty"`
	Headers        map[string]string `json:"headers,omitempty"`
	FollowRedirect *bool             `json:"follow-redirect,omitempty"`
	Expected       string            `json:"expected,omitempty"`
	Rounds         int               `json:"rounds,omitempty"`
	Unified        *bool             `json:"unified,omitempty"`
}

type APIDownload struct {
	URLs           []APIDownloadURL  `json:"urls"`
	Timeout        int               `json:"timeout"`
	Headers        map[string]string `json:"headers,omitempty"`
	FollowRedirect bool              `json:"follow-redirect"`
	Rounds         int               `json:"rounds"`
	MaxBytes       int               `json:"max-bytes"`
	Concurrency    int               `json:"concurrency"`
}

type APIDownloadURL struct {
	URL            string            `json:"url"`
	Timeout        int               `json:"timeout,omitempty"`
	Headers        map[string]string `json:"headers,omitempty"`
	FollowRedirect *bool             `json:"follow-redirect,omitempty"`
	Rounds         int               `json:"rounds,omitempty"`
	MaxBytes       int               `json:"max-bytes,omitempty"`
}

func ToAPI(cfg Config) APIConfig {
	cfg = cfg.Clone()
	return APIConfig{
		DefaultTimeout: cfg.DefaultTimeout,
		ProxyServer: APIProxyServer{
			Expand:      cfg.ProxyServer.Expand,
			Nameservers: cloneStrings(cfg.ProxyServer.Nameservers),
			Timeout:     cfg.ProxyServer.Timeout,
		},
		Delay: APIDelay{
			URLs:           delayURLsToAPI(cfg.Delay.URLs),
			Timeout:        cfg.Delay.Timeout,
			Headers:        cloneStringMap(cfg.Delay.Headers),
			FollowRedirect: cfg.Delay.FollowRedirect,
			Expected:       cfg.Delay.Expected,
			Rounds:         cfg.Delay.Rounds,
			Concurrency:    cfg.Delay.Concurrency,
			Unified:        cfg.Delay.Unified,
		},
		Download: APIDownload{
			URLs:           downloadURLsToAPI(cfg.Download.URLs),
			Timeout:        cfg.Download.Timeout,
			Headers:        cloneStringMap(cfg.Download.Headers),
			FollowRedirect: cfg.Download.FollowRedirect,
			Rounds:         cfg.Download.Rounds,
			MaxBytes:       cfg.Download.MaxBytes,
			Concurrency:    cfg.Download.Concurrency,
		},
	}
}

func (s *Store) PatchAPI(values map[string]json.RawMessage) (Config, error) {
	decoded, err := decodePatchValues(values)
	if err != nil {
		return Config{}, err
	}
	if err := rejectAPIURLItemZeroOverrides(decoded); err != nil {
		return Config{}, err
	}
	next, err := mergeDecode(s.Snapshot(), decoded)
	if err != nil {
		return Config{}, err
	}
	s.value.Store(next.Clone())
	return next.Clone(), nil
}

func delayURLsToAPI(urls []DelayURL) []APIDelayURL {
	if urls == nil {
		return nil
	}
	out := make([]APIDelayURL, len(urls))
	for i, target := range urls {
		out[i] = APIDelayURL{
			URL:            target.URL,
			Timeout:        target.Timeout,
			Headers:        cloneStringMap(target.Headers),
			FollowRedirect: cloneBoolPtr(target.FollowRedirect),
			Expected:       target.Expected,
			Rounds:         target.Rounds,
			Unified:        cloneBoolPtr(target.Unified),
		}
	}
	return out
}

func downloadURLsToAPI(urls []DownloadURL) []APIDownloadURL {
	if urls == nil {
		return nil
	}
	out := make([]APIDownloadURL, len(urls))
	for i, target := range urls {
		out[i] = APIDownloadURL{
			URL:            target.URL,
			Timeout:        target.Timeout,
			Headers:        cloneStringMap(target.Headers),
			FollowRedirect: cloneBoolPtr(target.FollowRedirect),
			Rounds:         target.Rounds,
			MaxBytes:       target.MaxBytes,
		}
	}
	return out
}

func cloneBoolPtr(value *bool) *bool {
	if value == nil {
		return nil
	}
	clone := *value
	return &clone
}

func rejectAPIURLItemZeroOverrides(values map[string]any) error {
	if err := rejectAPIURLItemZeroFields(values, "delay", []string{"timeout", "rounds"}); err != nil {
		return err
	}
	return rejectAPIURLItemZeroFields(values, "download", []string{"timeout", "rounds", "max-bytes"})
}

func rejectAPIURLItemZeroFields(values map[string]any, section string, fields []string) error {
	rawSection, ok := values[section]
	if !ok {
		return nil
	}
	sectionMap, ok := rawSection.(map[string]any)
	if !ok {
		return nil
	}
	rawURLs, ok := sectionMap["urls"]
	if !ok {
		return nil
	}
	urls, ok := rawURLs.([]any)
	if !ok {
		return nil
	}
	for idx, item := range urls {
		itemMap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		for _, field := range fields {
			if isJSONNumberZero(itemMap[field]) {
				return fmt.Errorf("%s.urls[%d].%s must be greater than 0", section, idx, field)
			}
		}
	}
	return nil
}

func isJSONNumberZero(value any) bool {
	number, ok := value.(json.Number)
	if !ok {
		return false
	}
	if parsed, err := strconv.Atoi(number.String()); err == nil {
		return parsed == 0
	}
	parsed, err := strconv.ParseFloat(number.String(), 64)
	return err == nil && parsed == 0
}
