package proxyconfig

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"mihomo-st/internal/httpclient"
)

func TestLoadTextParsesPlainBase64AndNonBase64YAML(t *testing.T) {
	inputs := map[string]string{
		"plain":      configYAML("plain-node"),
		"base64":     base64.StdEncoding.EncodeToString([]byte(configYAML("encoded-node"))),
		"plain-trim": "\n" + configYAML("trimmed-node") + "\n",
	}
	for name, input := range inputs {
		t.Run(name, func(t *testing.T) {
			result, err := LoadText(input)
			if err != nil {
				t.Fatal(err)
			}
			defer closeRecords(result.Records)
			if len(result.Records) != 1 || len(result.Warnings) != 0 {
				t.Fatalf("LoadText result = %+v", result)
			}
		})
	}
}

func TestLoadTextPropagatesYAMLErrors(t *testing.T) {
	if _, err := LoadText("proxies:\n  - name: ["); err == nil {
		t.Fatal("invalid YAML text returned nil error")
	}
}

func TestLoadLocalReadsFilesystemConfigAndReportsReadErrors(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nodes.yaml")
	if err := os.WriteFile(path, []byte(configYAML("local-node")), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := LoadLocal(path)
	if err != nil {
		t.Fatal(err)
	}
	defer closeRecords(result.Records)
	if len(result.Records) != 1 || result.Records[0].Raw["name"] != "local-node" {
		t.Fatalf("LoadLocal result = %+v", result)
	}

	if _, err := LoadLocal(filepath.Join(dir, "missing.yaml")); err == nil {
		t.Fatal("missing local config returned nil error")
	}
}

func TestLoadRemoteUsesHeadersDefaultUserAgentTimeoutAndYAMLParsing(t *testing.T) {
	seen := make(chan http.Header, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen <- r.Header.Clone()
		_, _ = w.Write([]byte(configYAML("remote-node")))
	}))
	defer server.Close()

	result, err := LoadRemote(context.Background(), server.URL, RemoteOptions{
		Timeout: time.Second,
		Headers: map[string]string{
			"Authorization": "Bearer token",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer closeRecords(result.Records)
	if len(result.Records) != 1 || result.Records[0].Raw["name"] != "remote-node" {
		t.Fatalf("remote result = %+v", result)
	}
	headers := <-seen
	if headers.Get("Authorization") != "Bearer token" || headers.Get("User-Agent") != httpclient.DefaultUserAgent {
		t.Fatalf("remote request headers = %+v", headers)
	}
}

func TestLoadRemoteCustomUserAgentAndRedirectPolicy(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") != "custom-agent" {
			t.Fatalf("user-agent = %q", r.Header.Get("User-Agent"))
		}
		_, _ = w.Write([]byte(configYAML("redirected")))
	}))
	defer target.Close()
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer redirector.Close()

	result, err := LoadRemote(context.Background(), redirector.URL, RemoteOptions{
		Timeout: time.Second,
		Headers: map[string]string{"User-Agent": "custom-agent"},
	})
	if err != nil {
		t.Fatal(err)
	}
	closeRecords(result.Records)

	noRedirect := false
	_, err = LoadRemote(context.Background(), redirector.URL, RemoteOptions{
		Timeout:        time.Second,
		FollowRedirect: &noRedirect,
	})
	var status *RemoteStatusError
	if !errors.As(err, &status) || status.StatusCode != http.StatusFound {
		t.Fatalf("disabled redirect error = %v", err)
	}
}

func TestLoadRemoteRejectsInvalidURLsAndNonSuccessStatuses(t *testing.T) {
	invalid := []string{"", "ftp://example.test/config.yaml", "https:///missing-host", "http://%zz"}
	for _, rawURL := range invalid {
		if _, err := LoadRemote(context.Background(), rawURL, RemoteOptions{Timeout: time.Second}); !errors.Is(err, ErrInvalidRemoteURL) {
			t.Fatalf("LoadRemote(%q) error = %v", rawURL, err)
		}
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad", http.StatusBadGateway)
	}))
	defer server.Close()
	_, err := LoadRemote(context.Background(), server.URL, RemoteOptions{Timeout: time.Second})
	var status *RemoteStatusError
	if !errors.As(err, &status) || status.StatusCode != http.StatusBadGateway || !errors.Is(err, ErrRemoteStatus) {
		t.Fatalf("status error = %v", err)
	}
}

func TestLoadRemoteMapsTimeoutsAndBodyLimit(t *testing.T) {
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
		_, _ = w.Write([]byte(configYAML("late")))
	}))
	defer slow.Close()
	if _, err := LoadRemote(context.Background(), slow.URL, RemoteOptions{Timeout: time.Nanosecond}); !errors.Is(err, ErrRemoteTimeout) {
		t.Fatalf("timeout error = %v", err)
	}

	oldLimit := maxRemoteConfigBytes
	maxRemoteConfigBytes = 5
	defer func() { maxRemoteConfigBytes = oldLimit }()
	large := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("123456"))
	}))
	defer large.Close()
	if _, err := LoadRemote(context.Background(), large.URL, RemoteOptions{Timeout: time.Second}); !errors.Is(err, ErrRemoteBodyTooLarge) {
		t.Fatalf("body limit error = %v", err)
	}
}

func TestLoadSourceContextChoosesRemoteOrLocalAndConvertsFailuresToWarnings(t *testing.T) {
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(configYAML("source-remote")))
	}))
	defer remote.Close()
	remoteResult := LoadSourceContext(context.Background(), remote.URL, time.Second)
	defer closeRecords(remoteResult.Records)
	if len(remoteResult.Records) != 1 || len(remoteResult.Warnings) != 0 {
		t.Fatalf("remote source result = %+v", remoteResult)
	}

	path := filepath.Join(t.TempDir(), "local.yaml")
	if err := os.WriteFile(path, []byte(configYAML("source-local")), 0o600); err != nil {
		t.Fatal(err)
	}
	localResult := LoadSourceContext(context.Background(), path, time.Second)
	defer closeRecords(localResult.Records)
	if len(localResult.Records) != 1 || len(localResult.Warnings) != 0 {
		t.Fatalf("local source result = %+v", localResult)
	}

	missing := LoadSourceContext(context.Background(), filepath.Join(t.TempDir(), "missing.yaml"), time.Second)
	if len(missing.Records) != 0 || len(missing.Warnings) != 1 || missing.Warnings[0].Index != -1 {
		t.Fatalf("missing source result = %+v", missing)
	}
}

func TestRemoteURLClassifierAcceptsOnlyHTTPWithHost(t *testing.T) {
	cases := map[string]bool{
		"http://example.test/config.yaml":  true,
		"https://example.test/config.yaml": true,
		"ftp://example.test/config.yaml":   false,
		"https:///config.yaml":             false,
		"C:\\nodes.yaml":                   false,
		"./nodes.yaml":                     false,
		"":                                 false,
	}
	for input, want := range cases {
		if got := isRemoteConfigURL(input); got != want {
			t.Fatalf("isRemoteConfigURL(%q) = %v, want %v", input, got, want)
		}
	}
}
