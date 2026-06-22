package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"mihomo-st/internal/app"
	"mihomo-st/internal/cli"
	"mihomo-st/internal/config"
	"mihomo-st/internal/server"
	"mihomo-st/internal/version"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	logFatalIfError(runWithServe(ctx, os.Args[1:], os.Stdout, nil))
}

type serveFunc func(*http.Server) error

var exit = os.Exit
var newRuntime = app.New
var newHandler = server.New
var listenTCP = net.Listen
var shutdownServer = func(srv *http.Server, ctx context.Context) error {
	return srv.Shutdown(ctx)
}

func logFatalIfError(err error) {
	if err == nil {
		return
	}
	log.Print(err)
	exit(1)
}

func runWithServe(ctx context.Context, args []string, stdout io.Writer, serve serveFunc) error {
	opts, err := cli.Parse(args)
	if err != nil {
		return err
	}

	if opts.ShowVersion {
		fmt.Fprintln(stdout, version.Version)
		return nil
	}

	runtime, err := newRuntime(config.Default())
	if err != nil {
		return fmt.Errorf("initialize runtime: %w", err)
	}
	defer runtime.Close()

	addr, err := cli.NormalizeListenAddr(opts.Listen)
	if err != nil {
		return fmt.Errorf("invalid listen address: %w", err)
	}

	srv := &http.Server{
		Addr:              addr,
		Handler:           newHandler(runtime),
		ReadHeaderTimeout: 5 * time.Second,
	}
	if serve == nil {
		serve = func(srv *http.Server) error {
			listener, err := listenTCP("tcp", srv.Addr)
			if err != nil {
				return err
			}
			return srv.Serve(listener)
		}
	}

	go func() {
		log.Printf("mihomo-st listening on http://%s", addr)
		if err := serve(srv); err != nil && err != http.ErrServerClosed {
			log.Printf("listen: %v", err)
		}
	}()

	<-ctx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := shutdownServer(srv, shutdownCtx); err != nil {
		log.Printf("shutdown: %v", err)
	}
	return nil
}
