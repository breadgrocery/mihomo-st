package cli

import (
	"strings"
	"testing"
)

func TestParseOnlySupportsStartupSurface(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		wantListen  string
		wantVersion bool
	}{
		{name: "no flags uses built in listen", wantListen: DefaultListen},
		{name: "long flags", args: []string{"--listen=0.0.0.0:4000", "--version"}, wantListen: "0.0.0.0:4000", wantVersion: true},
		{name: "short flags", args: []string{"-l", "[::1]:4010", "-v"}, wantListen: "[::1]:4010", wantVersion: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Parse(tt.args)
			if err != nil {
				t.Fatalf("Parse(%v) error = %v", tt.args, err)
			}
			if got.Listen != tt.wantListen || got.ShowVersion != tt.wantVersion {
				t.Fatalf("Parse(%v) = %+v, want listen %q version %v", tt.args, got, tt.wantListen, tt.wantVersion)
			}
		})
	}
}

func TestParseRejectsRemovedAndMalformedFlags(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{name: "old config file flag", args: []string{"--config", "app.yaml"}},
		{name: "old short config flag", args: []string{"-c", "app.yaml"}},
		{name: "removed flat delay timeout", args: []string{"--delay-timeout", "1000"}},
		{name: "removed flat delay concurrency", args: []string{"--delay-concurrency", "2"}},
		{name: "removed flat download url", args: []string{"--download-url", "https://example.test/file"}},
		{name: "removed proxy server expansion flag", args: []string{"--proxy-server-expand"}},
		{name: "single dash long option", args: []string{"-config", "app.yaml"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Parse(tc.args); err == nil {
				t.Fatalf("Parse(%v) succeeded", tc.args)
			}
		})
	}
}

func TestSingleDashScreeningAllowsNumericArguments(t *testing.T) {
	if err := rejectSingleDashLongFlags([]string{"-1", "-65535"}); err != nil {
		t.Fatalf("numeric-looking args should pass pre-screening: %v", err)
	}
	if err := rejectSingleDashLongFlags([]string{"-listen"}); err == nil || !strings.Contains(err.Error(), "single-dash") {
		t.Fatalf("single dash long flag error = %v", err)
	}
}

func TestNormalizeListenAddrAcceptsExplicitHostPortOnly(t *testing.T) {
	valid := []string{
		"127.0.0.1:32198",
		"localhost:32198",
		"example.test:443",
		"0.0.0.0:1",
		"[::1]:32198",
		"[::]:65535",
	}

	for _, input := range valid {
		t.Run(input, func(t *testing.T) {
			got, err := NormalizeListenAddr(input)
			if err != nil {
				t.Fatalf("NormalizeListenAddr(%q) error = %v", input, err)
			}
			if got != input {
				t.Fatalf("NormalizeListenAddr(%q) = %q", input, got)
			}
		})
	}
}

func TestNormalizeListenAddrRejectsAmbiguousOrInvalidValues(t *testing.T) {
	invalid := []string{
		"",
		"   ",
		":32198",
		"32198",
		"127.0.0.1",
		"127.0.0.1:0x50",
		"127.0.0.1:65536",
		"http://127.0.0.1:32198",
		"::1:32198",
	}

	for _, input := range invalid {
		name := strings.ReplaceAll(input, " ", "_")
		if name == "" {
			name = "empty"
		}
		t.Run(name, func(t *testing.T) {
			if got, err := NormalizeListenAddr(input); err == nil {
				t.Fatalf("NormalizeListenAddr(%q) = %q, want error", input, got)
			}
		})
	}
}
