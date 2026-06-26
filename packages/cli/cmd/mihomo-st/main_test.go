package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"mihomo-st/internal/app"
	"mihomo-st/internal/config"
	"mihomo-st/internal/proxyconfig"
	"mihomo-st/internal/version"
)

func TestRunWithServeVersionPrintsOnlyVersionAndDoesNotStartRuntime(t *testing.T) {
	restore := replaceProcessHooks(t)
	defer restore()

	newRuntime = func(config.Config, ...[]*proxyconfig.Record) (*app.Runtime, error) {
		t.Fatal("version command initialized runtime")
		return nil, nil
	}

	var stdout bytes.Buffer
	if err := runWithServe(context.Background(), []string{"--version"}, &stdout, nil); err != nil {
		t.Fatal(err)
	}
	if stdout.String() != version.Version+"\n" {
		t.Fatalf("version output = %q", stdout.String())
	}
}

func TestRunWithServeRejectsRemovedFlagsAndInvalidListenAddress(t *testing.T) {
	cases := []struct {
		name     string
		args     []string
		contains string
	}{
		{name: "single dash long flag", args: []string{"-config", "proxies.yaml"}, contains: "single-dash"},
		{name: "removed config flag", args: []string{"--config", "proxies.yaml"}, contains: "unknown flag"},
		{name: "removed delay timeout flag", args: []string{"--delay-timeout", "1000"}, contains: "unknown flag"},
		{name: "port without host", args: []string{"--listen", ":32198"}, contains: "invalid listen address"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			err := runWithServe(context.Background(), tc.args, io.Discard, nil)
			if err == nil || !strings.Contains(err.Error(), tc.contains) {
				t.Fatalf("error = %v, want substring %q", err, tc.contains)
			}
		})
	}
}

func TestRunWithServeCreatesRuntimeFromBuiltInDefaultsAndNoStartupProxies(t *testing.T) {
	restore := replaceProcessHooks(t)
	defer restore()

	var captured config.Config
	var variadicProxyArgs int
	realNewRuntime := newRuntime
	newRuntime = func(cfg config.Config, records ...[]*proxyconfig.Record) (*app.Runtime, error) {
		captured = cfg.Clone()
		variadicProxyArgs = len(records)
		return realNewRuntime(cfg, records...)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	err := runWithServe(ctx, nil, io.Discard, func(srv *http.Server) error {
		if srv.Addr != "127.0.0.1:32198" {
			t.Fatalf("server address = %q", srv.Addr)
		}
		if srv.Handler == nil {
			t.Fatal("server handler was nil")
		}
		if srv.ReadHeaderTimeout != 5*time.Second {
			t.Fatalf("ReadHeaderTimeout = %s", srv.ReadHeaderTimeout)
		}
		cancel()
		return http.ErrServerClosed
	})
	if err != nil {
		t.Fatal(err)
	}
	if captured.Delay.Timeout != config.DefaultDelayTimeout ||
		captured.Delay.Concurrency != config.DefaultDelayConcurrency ||
		captured.Download.Concurrency != config.DefaultDownloadConcurrency {
		t.Fatalf("runtime config = %+v", captured)
	}
	if variadicProxyArgs != 0 {
		t.Fatalf("runWithServe passed startup proxy records: %d", variadicProxyArgs)
	}
}

func TestRunWithServeReturnsRuntimeInitializationErrors(t *testing.T) {
	restore := replaceProcessHooks(t)
	defer restore()

	newRuntime = func(config.Config, ...[]*proxyconfig.Record) (*app.Runtime, error) {
		return nil, errors.New("store rejected defaults")
	}

	err := runWithServe(context.Background(), nil, io.Discard, func(*http.Server) error {
		t.Fatal("serve function should not run after runtime init failure")
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "initialize runtime") || !strings.Contains(err.Error(), "store rejected defaults") {
		t.Fatalf("runtime init error = %v", err)
	}
}

func TestRunWithServeWaitsForContextThenCallsShutdown(t *testing.T) {
	restore := replaceProcessHooks(t)
	defer restore()

	var served bool
	var shutdownSeen bool
	shutdownServer = func(srv *http.Server, ctx context.Context) error {
		shutdownSeen = true
		if srv == nil {
			t.Fatal("shutdown received nil server")
		}
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("shutdown context has no deadline")
		}
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	err := runWithServe(ctx, []string{"--listen", "127.0.0.1:0"}, io.Discard, func(srv *http.Server) error {
		served = true
		cancel()
		return http.ErrServerClosed
	})
	if err != nil {
		t.Fatal(err)
	}
	if !served || !shutdownSeen {
		t.Fatalf("served=%v shutdown=%v", served, shutdownSeen)
	}
}

func TestRunWithServeLogsServeAndShutdownErrorsWithoutReturningThem(t *testing.T) {
	restore := replaceProcessHooks(t)
	defer restore()

	shutdownServer = func(*http.Server, context.Context) error {
		return errors.New("shutdown failed")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	err := runWithServe(ctx, []string{"-l", "127.0.0.1:0"}, io.Discard, func(*http.Server) error {
		cancel()
		return errors.New("listen failed")
	})
	if err != nil {
		t.Fatalf("runWithServe returned logged serve/shutdown error: %v", err)
	}
}

func TestDefaultServePathUsesNativeTCPListenerAndExitsOnContextCancel(t *testing.T) {
	restore := replaceProcessHooks(t)
	defer restore()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	listenTCP = func(network, address string) (net.Listener, error) {
		if network != "tcp" || address != "127.0.0.1:0" {
			t.Fatalf("listen arguments = %s %s", network, address)
		}
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		cancel()
		return listener, nil
	}

	if err := runWithServe(ctx, []string{"--listen", "127.0.0.1:0"}, io.Discard, nil); err != nil {
		t.Fatal(err)
	}
}

func TestDefaultServePathTreatsListenFailureAsLoggedRuntimeState(t *testing.T) {
	restore := replaceProcessHooks(t)
	defer restore()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	listenTCP = func(network, address string) (net.Listener, error) {
		cancel()
		return nil, errors.New("bind failed")
	}

	if err := runWithServe(ctx, []string{"--listen", "127.0.0.1:0"}, io.Discard, nil); err != nil {
		t.Fatalf("listen failure was returned instead of logged: %v", err)
	}
}

func TestLogFatalIfErrorOnlyExitsForNonNilErrors(t *testing.T) {
	restore := replaceProcessHooks(t)
	defer restore()

	exitCalled := false
	exit = func(code int) {
		exitCalled = true
		panic(code)
	}

	logFatalIfError(nil)
	if exitCalled {
		t.Fatal("nil error triggered exit")
	}

	defer func() {
		recovered := recover()
		if recovered != 1 {
			t.Fatalf("exit panic = %v, want code 1", recovered)
		}
	}()
	logFatalIfError(errors.New("fatal startup error"))
}

func TestMainVersionPathUsesProcessArguments(t *testing.T) {
	restore := replaceProcessHooks(t)
	defer restore()

	newRuntime = func(config.Config, ...[]*proxyconfig.Record) (*app.Runtime, error) {
		t.Fatal("main --version initialized runtime")
		return nil, nil
	}

	oldArgs := os.Args
	oldStdout := os.Stdout
	defer func() {
		os.Args = oldArgs
		os.Stdout = oldStdout
	}()

	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Args = []string{"mihomo-st", "-v"}
	os.Stdout = writer

	main()

	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	output, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if string(output) != version.Version+"\n" {
		t.Fatalf("main version output = %q", output)
	}
}

func replaceProcessHooks(t *testing.T) func() {
	t.Helper()
	oldExit := exit
	oldNewRuntime := newRuntime
	oldNewHandler := newHandler
	oldListenTCP := listenTCP
	oldShutdownServer := shutdownServer
	return func() {
		exit = oldExit
		newRuntime = oldNewRuntime
		newHandler = oldNewHandler
		listenTCP = oldListenTCP
		shutdownServer = oldShutdownServer
	}
}
