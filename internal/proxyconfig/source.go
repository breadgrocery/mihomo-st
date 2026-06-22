package proxyconfig

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"mihomo-st/internal/httpclient"
)

const MaxRemoteConfigBytes = 100 * 1024 * 1024

var maxRemoteConfigBytes = MaxRemoteConfigBytes

var ErrInvalidRemoteURL = errors.New("remote source URL must use http or https")
var ErrRemoteStatus = errors.New("remote source returned non-2xx status")
var ErrRemoteTimeout = errors.New("remote source timeout")
var ErrRemoteBodyTooLarge = errors.New("remote source body too large")

type RemoteStatusError struct {
	StatusCode int
}

type RemoteOptions struct {
	Timeout        time.Duration
	Headers        map[string]string
	FollowRedirect *bool
}

func (e *RemoteStatusError) Error() string {
	return fmt.Sprintf("%s: HTTP %d", ErrRemoteStatus, e.StatusCode)
}

func (e *RemoteStatusError) Is(target error) bool {
	return target == ErrRemoteStatus
}

func LoadText(text string) (Result, error) {
	return Load(maybeBase64Decode([]byte(text)))
}

func LoadLocal(path string) (Result, error) {
	buf, err := os.ReadFile(path)
	if err != nil {
		return Result{}, err
	}
	return Load(maybeBase64Decode(buf))
}

func LoadRemote(ctx context.Context, rawURL string, opts RemoteOptions) (Result, error) {
	if _, err := parseRemoteURL(rawURL); err != nil {
		return Result{}, err
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = time.Duration(5000) * time.Millisecond
	}
	followRedirect := true
	if opts.FollowRedirect != nil {
		followRedirect = *opts.FollowRedirect
	}
	client := httpclient.New(httpclient.Options{
		Timeout:          timeout,
		Headers:          opts.Headers,
		DisableRedirects: !followRedirect,
	})
	req, err := httpclient.NewRequest(ctx, http.MethodGet, rawURL, opts.Headers, nil)
	if err != nil {
		return Result{}, err
	}
	resp, err := client.Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || os.IsTimeout(err) {
			return Result{}, fmt.Errorf("%w: %v", ErrRemoteTimeout, err)
		}
		return Result{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Result{}, &RemoteStatusError{StatusCode: resp.StatusCode}
	}

	buf, err := io.ReadAll(io.LimitReader(resp.Body, int64(maxRemoteConfigBytes)+1))
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || os.IsTimeout(err) {
			return Result{}, fmt.Errorf("%w: %v", ErrRemoteTimeout, err)
		}
		return Result{}, err
	}
	if len(buf) > maxRemoteConfigBytes {
		return Result{}, fmt.Errorf("%w: limit is %d bytes", ErrRemoteBodyTooLarge, maxRemoteConfigBytes)
	}
	return Load(maybeBase64Decode(buf))
}

func LoadSourceContext(ctx context.Context, source string, timeout time.Duration) Result {
	var (
		result Result
		err    error
	)
	if isRemoteConfigURL(source) {
		result, err = LoadRemote(ctx, source, RemoteOptions{Timeout: timeout})
	} else {
		result, err = LoadLocal(source)
	}
	if err != nil {
		return Result{Warnings: []Warning{{Index: -1, Message: err.Error()}}}
	}
	return result
}

func isRemoteConfigURL(value string) bool {
	_, err := parseRemoteURL(value)
	return err == nil
}

func parseRemoteURL(value string) (*url.URL, error) {
	parsed, err := url.Parse(value)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidRemoteURL, err)
	}
	if parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, ErrInvalidRemoteURL
	}
	return parsed, nil
}

func maybeBase64Decode(buf []byte) []byte {
	trimmed := strings.TrimSpace(string(buf))
	if trimmed == "" {
		return buf
	}
	decoded, err := base64.StdEncoding.DecodeString(trimmed)
	if err != nil {
		return buf
	}
	return decoded
}
