package cli

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/spf13/pflag"
)

const DefaultListen = "127.0.0.1:32198"

type Options struct {
	Listen      string
	ShowVersion bool
}

func Parse(args []string) (Options, error) {
	if err := rejectSingleDashLongFlags(args); err != nil {
		return Options{}, err
	}

	opts := Options{
		Listen: DefaultListen,
	}

	flags := pflag.NewFlagSet("mihomo-st", pflag.ContinueOnError)
	flags.StringVarP(&opts.Listen, "listen", "l", opts.Listen, "REST server listen address")
	flags.BoolVarP(&opts.ShowVersion, "version", "v", false, "print version and exit")

	if err := flags.Parse(args); err != nil {
		return Options{}, err
	}

	return opts, nil
}

func rejectSingleDashLongFlags(args []string) error {
	for _, arg := range args {
		if !strings.HasPrefix(arg, "-") || strings.HasPrefix(arg, "--") || len(arg) <= 2 {
			continue
		}
		if arg[1] >= '0' && arg[1] <= '9' {
			continue
		}
		return fmt.Errorf("single-dash long flag %q is not supported", arg)
	}
	return nil
}

func NormalizeListenAddr(addr string) (string, error) {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return "", fmt.Errorf("listen address is empty")
	}

	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "", err
	}
	if host == "" {
		return "", fmt.Errorf("host is empty")
	}
	if _, err := strconv.ParseUint(port, 10, 16); err != nil {
		return "", fmt.Errorf("invalid port %q", port)
	}
	return addr, nil
}
