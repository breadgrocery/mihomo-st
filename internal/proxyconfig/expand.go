package proxyconfig

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"sync"
	"time"

	"github.com/metacubex/mihomo/dns"
	"mihomo-st/internal/config"

	_ "github.com/metacubex/mihomo/config"
)

type ServerExpandOptions struct {
	Enabled     bool
	Nameservers []string
	Timeout     time.Duration
	Cache       *ServerExpandCache
}

type ipLookup func(context.Context, string) ([]netip.Addr, error)

type ServerExpandCache struct {
	mu            sync.Mutex
	nameserverKey string
	ips           map[string][]netip.Addr
}

type proxyServerLookupFactory func([]string) (ipLookup, error)

func ExpandServerDomains(ctx context.Context, result Result, opts ServerExpandOptions) Result {
	return expandServerDomainsWithLookupFactory(ctx, result, opts, newProxyServerLookup)
}

func expandServerDomainsWithLookupFactory(ctx context.Context, result Result, opts ServerExpandOptions, newLookup proxyServerLookupFactory) Result {
	if !opts.Enabled || len(result.Records) == 0 {
		return result
	}

	lookup, err := newLookup(opts.Nameservers)
	if err != nil {
		result.Warnings = append(result.Warnings, Warning{Index: -1, Message: err.Error()})
		return result
	}
	if opts.Cache != nil {
		lookup = opts.Cache.wrap(opts.Nameservers, lookup)
	}
	return expandServerDomains(ctx, result, opts.Timeout, lookup)
}

func (c *ServerExpandCache) wrap(nameservers []string, lookup ipLookup) ipLookup {
	key := strings.Join(nameservers, "\x00")
	return func(ctx context.Context, host string) ([]netip.Addr, error) {
		c.mu.Lock()
		if c.nameserverKey != key {
			c.nameserverKey = key
			c.ips = map[string][]netip.Addr{}
		}
		if ips, ok := c.ips[host]; ok {
			cached := append([]netip.Addr(nil), ips...)
			c.mu.Unlock()
			return cached, nil
		}
		c.mu.Unlock()

		ips, err := lookup(ctx, host)
		if err != nil {
			return nil, err
		}
		ips = uniqueIPs(ips)

		c.mu.Lock()
		if c.nameserverKey == key {
			c.ips[host] = append([]netip.Addr(nil), ips...)
		}
		c.mu.Unlock()
		return ips, nil
	}
}

func newProxyServerLookup(nameservers []string) (ipLookup, error) {
	parsed, err := parseProxyServerNameServers(nameservers)
	if err != nil {
		return nil, err
	}
	system, _ := parseProxyServerNameServers([]string{"system"})
	lookups := make([]ipLookup, 0, len(parsed))
	for _, nameserver := range parsed {
		lookups = append(lookups, newNameServerLookup(nameserver, system))
	}
	return lookupAll(lookups), nil
}

func parseProxyServerNameServers(nameservers []string) ([]dns.NameServer, error) {
	if dns.ParseNameServer == nil {
		return nil, fmt.Errorf("proxy-server-nameserver parser is not initialized")
	}
	return dns.ParseNameServer(nameservers)
}

func newNameServerLookup(nameserver dns.NameServer, defaultNameservers []dns.NameServer) ipLookup {
	resolvers := dns.NewResolver(dns.Config{
		Main:    []dns.NameServer{nameserver},
		Default: defaultNameservers,
		IPv6:    true,
	})
	return resolvers.Resolver.LookupIP
}

func lookupAll(lookups []ipLookup) ipLookup {
	return func(ctx context.Context, host string) ([]netip.Addr, error) {
		results := make([]lookupResult, len(lookups))
		var wg sync.WaitGroup
		for idx, lookup := range lookups {
			idx := idx
			lookup := lookup
			wg.Add(1)
			go func() {
				defer wg.Done()
				ips, err := lookup(ctx, host)
				results[idx] = lookupResult{ips: ips, err: err}
			}()
		}
		wg.Wait()

		ips := make([]netip.Addr, 0)
		errs := make([]error, 0)
		for _, result := range results {
			if result.err != nil {
				errs = append(errs, result.err)
				continue
			}
			ips = append(ips, result.ips...)
		}
		ips = uniqueIPs(ips)
		if len(ips) != 0 {
			return ips, nil
		}
		if len(errs) == 0 {
			return nil, fmt.Errorf("no IP records found")
		}
		return nil, fmt.Errorf("all proxy-server-nameserver lookups failed: %w", errors.Join(errs...))
	}
}

type lookupResult struct {
	ips []netip.Addr
	err error
}

type expandJob struct {
	index  int
	record *Record
	server string
}

type expandResult struct {
	records  []*Record
	warnings []Warning
}

func expandServerDomains(ctx context.Context, result Result, timeout time.Duration, lookup ipLookup) Result {
	if timeout <= 0 {
		timeout = time.Duration(config.DefaultProxyServerTimeout) * time.Millisecond
	}
	expanded := Result{
		Records:  append([]*Record(nil), result.Records...),
		Warnings: append([]Warning(nil), result.Warnings...),
	}
	seen := make(map[string]struct{}, len(expanded.Records))
	for _, record := range expanded.Records {
		if record != nil {
			seen[record.Digest] = struct{}{}
		}
	}

	jobs := make([]expandJob, 0, len(result.Records))
	for idx, record := range result.Records {
		if record == nil {
			continue
		}
		server, ok := record.Raw["server"].(string)
		if !ok || !shouldExpandServer(server) {
			continue
		}
		jobs = append(jobs, expandJob{index: idx, record: record, server: server})
	}

	results := make([]expandResult, len(jobs))
	var wg sync.WaitGroup
	for idx, job := range jobs {
		idx := idx
		job := job
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[idx] = expandProxyServer(ctx, job, timeout, lookup)
		}()
	}
	wg.Wait()

	for _, result := range results {
		expanded.Warnings = append(expanded.Warnings, result.warnings...)
		for _, record := range result.records {
			if _, ok := seen[record.Digest]; ok {
				closeRecord(record)
				continue
			}
			expanded.Records = append(expanded.Records, record)
			seen[record.Digest] = struct{}{}
		}
	}
	return expanded
}

func expandProxyServer(ctx context.Context, job expandJob, timeout time.Duration, lookup ipLookup) expandResult {
	ips, err := lookupWithTimeout(ctx, timeout, lookup, job.server)
	if err != nil {
		return expandResult{
			warnings: []Warning{{Index: job.index, Message: fmt.Sprintf("expand proxy server %q: %s", job.server, err)}},
		}
	}

	result := expandResult{records: make([]*Record, 0, len(ips))}
	for _, ip := range uniqueIPs(ips) {
		mapping := cloneMap(job.record.Raw)
		mapping["server"] = ip.String()
		record, warning := parseProxyRecord(job.index, mapping)
		if warning != nil {
			warning.Message = fmt.Sprintf("expand proxy server %q to %s: %s", job.server, ip, warning.Message)
			result.warnings = append(result.warnings, *warning)
			continue
		}
		result.records = append(result.records, record)
	}
	return result
}

func lookupWithTimeout(ctx context.Context, timeout time.Duration, lookup ipLookup, host string) ([]netip.Addr, error) {
	lookupCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return lookup(lookupCtx, host)
}

func shouldExpandServer(server string) bool {
	server = strings.TrimSpace(server)
	if server == "" {
		return false
	}
	if strings.HasPrefix(server, "[") && strings.HasSuffix(server, "]") {
		server = strings.TrimPrefix(strings.TrimSuffix(server, "]"), "[")
	}
	if _, err := netip.ParseAddr(server); err == nil {
		return false
	}
	return true
}

func uniqueIPs(ips []netip.Addr) []netip.Addr {
	seen := make(map[string]struct{}, len(ips))
	unique := make([]netip.Addr, 0, len(ips))
	for _, ip := range ips {
		ip = ip.Unmap()
		key := ip.String()
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		unique = append(unique, ip)
	}
	return unique
}
