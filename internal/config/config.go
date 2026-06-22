package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"reflect"
	"strconv"
	"sync/atomic"

	"github.com/go-playground/validator/v10"
	"github.com/go-viper/mapstructure/v2"
)

const (
	DefaultTimeout               = 5000
	DefaultProxyServerTimeout    = 5000
	DefaultProxyServerNameserver = "system"
	DefaultDelayURL              = "https://www.google.com/generate_204"
	DefaultDelayTimeout          = 10000
	DefaultDelayExpected         = "200-299"
	DefaultDelayRounds           = 2
	DefaultDelayConcurrency      = 100
	DefaultDelayUnified          = true
	DefaultDownloadURL           = "https://cachefly.cachefly.net/50mb.test"
	DefaultDownloadTimeout       = 10000
	DefaultDownloadRounds        = 1
	DefaultDownloadMaxBytes      = 100 * 1024 * 1024
	DefaultDownloadConcurrency   = 1
)

type Config struct {
	DefaultTimeout int         `config:"default-timeout" validate:"gt=0"`
	ProxyServer    ProxyServer `config:"proxy-server"`
	Delay          Delay       `config:"delay"`
	Download       Download    `config:"download"`
}

type ProxyServer struct {
	Expand      bool     `config:"expand"`
	Nameservers []string `config:"nameservers" validate:"min=1,dive,required"`
	Timeout     int      `config:"timeout" validate:"gt=0"`
}

type Delay struct {
	URLs           []DelayURL        `config:"urls" validate:"min=1,dive"`
	Timeout        int               `config:"timeout" validate:"gt=0"`
	Headers        map[string]string `config:"headers"`
	FollowRedirect bool              `config:"follow-redirect"`
	Expected       string            `config:"expected" validate:"required"`
	Rounds         int               `config:"rounds" validate:"gt=0"`
	Concurrency    int               `config:"concurrency" validate:"gt=0"`
	Unified        bool              `config:"unified"`
}

type DelayURL struct {
	URL            string            `config:"url" validate:"required,http_url"`
	Timeout        int               `config:"timeout" validate:"gte=0"`
	Headers        map[string]string `config:"headers"`
	FollowRedirect *bool             `config:"follow-redirect"`
	Expected       string            `config:"expected"`
	Rounds         int               `config:"rounds" validate:"gte=0"`
	Unified        *bool             `config:"unified"`
}

type Download struct {
	URLs           []DownloadURL     `config:"urls" validate:"min=1,dive"`
	Timeout        int               `config:"timeout" validate:"gt=0"`
	Headers        map[string]string `config:"headers"`
	FollowRedirect bool              `config:"follow-redirect"`
	Rounds         int               `config:"rounds" validate:"gt=0"`
	MaxBytes       int               `config:"max-bytes" validate:"gt=0"`
	Concurrency    int               `config:"concurrency" validate:"gt=0"`
}

type DownloadURL struct {
	URL            string            `config:"url" validate:"required,http_url"`
	Timeout        int               `config:"timeout" validate:"gte=0"`
	Headers        map[string]string `config:"headers"`
	FollowRedirect *bool             `config:"follow-redirect"`
	Rounds         int               `config:"rounds" validate:"gte=0"`
	MaxBytes       int               `config:"max-bytes" validate:"gte=0"`
}

type Store struct {
	value atomic.Value
}

var validate = newValidator()

func Default() Config {
	return Config{
		DefaultTimeout: DefaultTimeout,
		ProxyServer: ProxyServer{
			Nameservers: []string{DefaultProxyServerNameserver},
			Timeout:     DefaultProxyServerTimeout,
		},
		Delay: Delay{
			URLs:           []DelayURL{{URL: DefaultDelayURL}},
			Timeout:        DefaultDelayTimeout,
			FollowRedirect: true,
			Expected:       DefaultDelayExpected,
			Rounds:         DefaultDelayRounds,
			Concurrency:    DefaultDelayConcurrency,
			Unified:        DefaultDelayUnified,
		},
		Download: Download{
			URLs:           []DownloadURL{{URL: DefaultDownloadURL}},
			Timeout:        DefaultDownloadTimeout,
			FollowRedirect: true,
			Rounds:         DefaultDownloadRounds,
			MaxBytes:       DefaultDownloadMaxBytes,
			Concurrency:    DefaultDownloadConcurrency,
		},
	}
}

func NewStore(initial Config) (*Store, error) {
	if err := initial.Validate(); err != nil {
		return nil, err
	}
	store := &Store{}
	store.value.Store(initial.Clone())
	return store, nil
}

func (s *Store) Snapshot() Config {
	value, ok := s.value.Load().(Config)
	if !ok {
		return Default()
	}
	return value.Clone()
}

func (c Config) Clone() Config {
	c.ProxyServer.Nameservers = cloneStrings(c.ProxyServer.Nameservers)
	c.Delay.Headers = cloneStringMap(c.Delay.Headers)
	c.Delay.URLs = cloneDelayURLs(c.Delay.URLs)
	c.Download.Headers = cloneStringMap(c.Download.Headers)
	c.Download.URLs = cloneDownloadURLs(c.Download.URLs)
	return c
}

func (c Config) Validate() error {
	if err := validate.Struct(c); err != nil {
		return formatValidationError(err)
	}
	return nil
}

func (p ProxyServer) Validate() error {
	if err := validate.Struct(p); err != nil {
		return formatValidationError(err)
	}
	return nil
}

func mergeDecode(base Config, patch map[string]any) (Config, error) {
	baseMap, err := configToMap(base)
	if err != nil {
		return Config{}, err
	}
	merged := deepMerge(baseMap, patch).(map[string]any)
	next, err := decodeMap(merged)
	if err != nil {
		return Config{}, err
	}
	if err := next.Validate(); err != nil {
		return Config{}, err
	}
	return next.Clone(), nil
}

func decodeMap(values map[string]any) (Config, error) {
	var cfg Config
	decoder, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{
		DecodeHook: mapstructure.ComposeDecodeHookFunc(
			stringToDelayURLHook(),
			stringToDownloadURLHook(),
			jsonNumberHook(),
		),
		ErrorUnused: true,
		Result:      &cfg,
		TagName:     "config",
	})
	if err != nil {
		return Config{}, err
	}
	if err := decoder.Decode(values); err != nil {
		return Config{}, fmt.Errorf("cannot decode config: %w", err)
	}
	return cfg, nil
}

func configToMap(cfg Config) (map[string]any, error) {
	buf, err := json.Marshal(ToAPI(cfg))
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(buf))
	decoder.UseNumber()
	var values map[string]any
	if err := decoder.Decode(&values); err != nil {
		return nil, err
	}
	return values, nil
}

func decodePatchValues(values map[string]json.RawMessage) (map[string]any, error) {
	decoded := make(map[string]any, len(values))
	for key, raw := range values {
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		var value any
		if err := decoder.Decode(&value); err != nil {
			return nil, fmt.Errorf("%s: %w", key, err)
		}
		if decoder.More() {
			return nil, fmt.Errorf("%s: request body must contain only one JSON value", key)
		}
		if err := rejectNulls(value, key); err != nil {
			return nil, err
		}
		decoded[key] = value
	}
	return decoded, nil
}

func rejectNulls(value any, path string) error {
	if value == nil {
		return fmt.Errorf("%s cannot be null", path)
	}
	switch v := value.(type) {
	case map[string]any:
		for key, item := range v {
			if err := rejectNulls(item, joinPath(path, key)); err != nil {
				return err
			}
		}
	case []any:
		for i, item := range v {
			if err := rejectNulls(item, fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}
		}
	}
	return nil
}

func joinPath(prefix, key string) string {
	if prefix == "" {
		return key
	}
	return prefix + "." + key
}

func deepMerge(base, patch any) any {
	baseMap, baseOK := base.(map[string]any)
	patchMap, patchOK := patch.(map[string]any)
	if baseOK && patchOK {
		merged := make(map[string]any, len(baseMap)+len(patchMap))
		for key, value := range baseMap {
			merged[key] = cloneAny(value)
		}
		for key, value := range patchMap {
			if existing, ok := merged[key]; ok {
				merged[key] = deepMerge(existing, value)
				continue
			}
			merged[key] = cloneAny(value)
		}
		return merged
	}
	return cloneAny(patch)
}

func cloneAny(value any) any {
	switch v := value.(type) {
	case map[string]any:
		clone := make(map[string]any, len(v))
		for key, item := range v {
			clone[key] = cloneAny(item)
		}
		return clone
	case []any:
		clone := make([]any, len(v))
		for i, item := range v {
			clone[i] = cloneAny(item)
		}
		return clone
	default:
		return value
	}
}

func stringToDelayURLHook() mapstructure.DecodeHookFuncType {
	targetType := reflect.TypeOf(DelayURL{})
	return func(from reflect.Type, to reflect.Type, value any) (any, error) {
		if from.Kind() == reflect.String && to == targetType {
			return DelayURL{URL: value.(string)}, nil
		}
		return value, nil
	}
}

func stringToDownloadURLHook() mapstructure.DecodeHookFuncType {
	targetType := reflect.TypeOf(DownloadURL{})
	return func(from reflect.Type, to reflect.Type, value any) (any, error) {
		if from.Kind() == reflect.String && to == targetType {
			return DownloadURL{URL: value.(string)}, nil
		}
		return value, nil
	}
}

func jsonNumberHook() mapstructure.DecodeHookFuncType {
	numberType := reflect.TypeOf(json.Number(""))
	return func(from reflect.Type, to reflect.Type, value any) (any, error) {
		if from != numberType {
			return value, nil
		}
		number := value.(json.Number)
		switch to.Kind() {
		case reflect.Int:
			parsed, err := strconv.Atoi(number.String())
			if err != nil {
				return nil, err
			}
			return parsed, nil
		default:
			return value, nil
		}
	}
}

func cloneDelayURLs(urls []DelayURL) []DelayURL {
	if urls == nil {
		return nil
	}
	clone := make([]DelayURL, len(urls))
	for i, target := range urls {
		clone[i] = target
		clone[i].Headers = cloneStringMap(target.Headers)
		if target.FollowRedirect != nil {
			followRedirect := *target.FollowRedirect
			clone[i].FollowRedirect = &followRedirect
		}
		if target.Unified != nil {
			unified := *target.Unified
			clone[i].Unified = &unified
		}
	}
	return clone
}

func cloneDownloadURLs(urls []DownloadURL) []DownloadURL {
	if urls == nil {
		return nil
	}
	clone := make([]DownloadURL, len(urls))
	for i, target := range urls {
		clone[i] = target
		clone[i].Headers = cloneStringMap(target.Headers)
		if target.FollowRedirect != nil {
			followRedirect := *target.FollowRedirect
			clone[i].FollowRedirect = &followRedirect
		}
	}
	return clone
}

func cloneStringMap(values map[string]string) map[string]string {
	if values == nil {
		return nil
	}
	clone := make(map[string]string, len(values))
	for key, value := range values {
		clone[key] = value
	}
	return clone
}

func cloneStrings(values []string) []string {
	if values == nil {
		return nil
	}
	clone := make([]string, len(values))
	copy(clone, values)
	return clone
}

func newValidator() *validator.Validate {
	v := validator.New()
	_ = v.RegisterValidation("http_url", func(fl validator.FieldLevel) bool {
		parsed, err := url.Parse(fl.Field().String())
		return err == nil && parsed.Host != "" && (parsed.Scheme == "http" || parsed.Scheme == "https")
	})
	v.RegisterTagNameFunc(func(field reflect.StructField) string {
		if name := field.Tag.Get("config"); name != "" {
			return name
		}
		return field.Name
	})
	return v
}

func formatValidationError(err error) error {
	validationErrors, ok := err.(validator.ValidationErrors)
	if !ok || len(validationErrors) == 0 {
		return err
	}
	validationError := validationErrors[0]
	return fmt.Errorf("%s failed validation: %s", validationError.Namespace(), validationError.Tag())
}
