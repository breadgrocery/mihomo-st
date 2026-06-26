package proxyconfig

import (
	"context"
	"errors"
	"net/netip"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/metacubex/mihomo/dns"
)

func TestExpandServerDomainsLeavesDisabledOrEmptyResultsAlone(t *testing.T) {
	input := Result{Warnings: []Warning{{Index: 9, Message: "existing"}}}
	if got := ExpandServerDomains(context.Background(), input, ServerExpandOptions{}); !reflect.DeepEqual(got, input) {
		t.Fatalf("disabled expansion = %+v", got)
	}
	if got := ExpandServerDomains(context.Background(), Result{}, ServerExpandOptions{Enabled: true}); len(got.Records) != 0 || len(got.Warnings) != 0 {
		t.Fatalf("empty expansion = %+v", got)
	}
}

func TestExpandServerDomainsUsesLookupFactoryAndCache(t *testing.T) {
	records := parseProxyMappings([]map[string]any{validProxy("node", map[string]any{"server": "node.example"})})
	defer closeRecords(records.Records)

	calls := 0
	factory := func(nameservers []string) (ipLookup, error) {
		if !reflect.DeepEqual(nameservers, []string{"system"}) {
			t.Fatalf("nameservers = %+v", nameservers)
		}
		return func(context.Context, string) ([]netip.Addr, error) {
			calls++
			return []netip.Addr{netip.MustParseAddr("203.0.113.10")}, nil
		}, nil
	}
	cache := &ServerExpandCache{}
	opts := ServerExpandOptions{Enabled: true, Nameservers: []string{"system"}, Timeout: time.Second, Cache: cache}

	first := expandServerDomainsWithLookupFactory(context.Background(), records, opts, factory)
	defer closeRecords(first.Records[len(records.Records):])
	second := expandServerDomainsWithLookupFactory(context.Background(), records, opts, factory)
	defer closeRecords(second.Records[len(records.Records):])

	if len(first.Warnings) != 0 || len(first.Records) != 2 {
		t.Fatalf("first expansion = %+v", first)
	}
	if len(second.Warnings) != 0 || len(second.Records) != 2 {
		t.Fatalf("second expansion = %+v", second)
	}
	if calls != 1 {
		t.Fatalf("cached lookup calls = %d, want 1", calls)
	}
}

func TestExpandServerDomainsAppendsUniqueIPRecordsAndKeepsOriginals(t *testing.T) {
	records := parseProxyMappings([]map[string]any{
		validProxy("domain", map[string]any{"server": "domain.example"}),
		validProxy("ip", map[string]any{"server": "198.51.100.1"}),
	})
	defer closeRecords(records.Records)

	expanded := expandServerDomains(context.Background(), records, time.Second, func(ctx context.Context, host string) ([]netip.Addr, error) {
		if host != "domain.example" {
			t.Fatalf("unexpected lookup host %q", host)
		}
		return []netip.Addr{
			netip.MustParseAddr("::ffff:203.0.113.1"),
			netip.MustParseAddr("203.0.113.2"),
			netip.MustParseAddr("203.0.113.1"),
		}, nil
	})
	defer closeRecords(expanded.Records[len(records.Records):])

	if len(expanded.Warnings) != 0 {
		t.Fatalf("warnings = %+v", expanded.Warnings)
	}
	servers := map[string]int{}
	for _, record := range expanded.Records {
		if record != nil {
			servers[record.Raw["server"].(string)]++
		}
	}
	for _, server := range []string{"domain.example", "198.51.100.1", "203.0.113.1", "203.0.113.2"} {
		if servers[server] != 1 {
			t.Fatalf("server %q count = %d in %+v", server, servers[server], servers)
		}
	}
}

func TestExpandServerDomainsWarnsWithoutDroppingOriginalRecords(t *testing.T) {
	records := parseProxyMappings([]map[string]any{validProxy("domain", map[string]any{"server": "domain.example"})})
	defer closeRecords(records.Records)
	expanded := expandServerDomains(context.Background(), records, time.Second, func(context.Context, string) ([]netip.Addr, error) {
		return nil, errors.New("lookup refused")
	})
	if len(expanded.Records) != len(records.Records) {
		t.Fatalf("records changed after failed lookup: %+v", expanded.Records)
	}
	if len(expanded.Warnings) != 1 || expanded.Warnings[0].Index != 0 || !strings.Contains(expanded.Warnings[0].Message, "lookup refused") {
		t.Fatalf("warnings = %+v", expanded.Warnings)
	}
}

func TestExpandServerDomainsAddsLookupDeadlinesAndRunsRecordsConcurrently(t *testing.T) {
	records := parseProxyMappings([]map[string]any{
		validProxy("a", map[string]any{"server": "a.example"}),
		validProxy("b", map[string]any{"server": "b.example"}),
	})
	defer closeRecords(records.Records)

	started := make(chan string, 2)
	release := make(chan struct{})
	done := make(chan Result, 1)
	go func() {
		done <- expandServerDomains(context.Background(), records, 0, func(ctx context.Context, host string) ([]netip.Addr, error) {
			if _, ok := ctx.Deadline(); !ok {
				return nil, errors.New("missing deadline")
			}
			started <- host
			<-release
			if host == "a.example" {
				return []netip.Addr{netip.MustParseAddr("203.0.113.1")}, nil
			}
			return []netip.Addr{netip.MustParseAddr("203.0.113.2")}, nil
		})
	}()

	<-started
	select {
	case <-started:
	case <-time.After(150 * time.Millisecond):
		close(release)
		t.Fatal("second domain lookup did not start before first was released")
	}
	close(release)
	expanded := <-done
	defer closeRecords(expanded.Records[len(records.Records):])
	if len(expanded.Warnings) != 0 || len(expanded.Records) != 4 {
		t.Fatalf("concurrent expansion = %+v", expanded)
	}
}

func TestExpandServerDomainsSkipsNilBlankAndLiteralIPServers(t *testing.T) {
	result := Result{Records: []*Record{
		nil,
		{Digest: "missing", Raw: map[string]any{}},
		{Digest: "blank", Raw: map[string]any{"server": " "}},
		{Digest: "ipv4", Raw: map[string]any{"server": "203.0.113.1"}},
		{Digest: "ipv6", Raw: map[string]any{"server": "[2001:db8::1]"}},
	}}
	expanded := expandServerDomains(context.Background(), result, time.Second, func(context.Context, string) ([]netip.Addr, error) {
		t.Fatal("literal and missing servers should not be expanded")
		return nil, nil
	})
	if len(expanded.Records) != len(result.Records) || len(expanded.Warnings) != 0 {
		t.Fatalf("non-expandable result = %+v", expanded)
	}
}

func TestExpandServerDomainsSkipsDuplicateExpandedDigests(t *testing.T) {
	result := Result{Records: []*Record{
		{Digest: "a", Raw: validProxy("a", map[string]any{"server": "same.example"})},
		{Digest: "b", Raw: validProxy("b", map[string]any{"server": "same.example"})},
	}}
	expanded := expandServerDomains(context.Background(), result, time.Second, func(context.Context, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("203.0.113.9")}, nil
	})
	defer closeRecords(expanded.Records[len(result.Records):])
	if len(expanded.Records) != 3 {
		t.Fatalf("duplicate expanded digest was not skipped: %+v", expanded.Records)
	}
}

func TestExpandProxyServerReportsParseFailuresForGeneratedRecords(t *testing.T) {
	out := expandProxyServer(context.Background(), expandJob{
		index:  7,
		server: "bad.example",
		record: &Record{Raw: map[string]any{
			"name":   "bad",
			"type":   "unknown-type",
			"server": "bad.example",
		}},
	}, time.Second, func(context.Context, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("203.0.113.1")}, nil
	})
	if len(out.records) != 0 || len(out.warnings) != 1 || out.warnings[0].Index != 7 {
		t.Fatalf("expanded bad record result = %+v", out)
	}
}

func TestExpandServerDomainsReportsNameserverFactoryErrors(t *testing.T) {
	records := parseProxyMappings([]map[string]any{validProxy("node", map[string]any{"server": "node.example"})})
	defer closeRecords(records.Records)

	expanded := expandServerDomainsWithLookupFactory(context.Background(), records, ServerExpandOptions{Enabled: true}, func([]string) (ipLookup, error) {
		return nil, errors.New("factory failed")
	})
	if len(expanded.Records) != len(records.Records) || len(expanded.Warnings) != 1 || !strings.Contains(expanded.Warnings[0].Message, "factory failed") {
		t.Fatalf("factory error expansion = %+v", expanded)
	}
}

func TestParseProxyServerNameServersFailsWhenParserIsUnavailable(t *testing.T) {
	previous := dns.ParseNameServer
	dns.ParseNameServer = nil
	defer func() { dns.ParseNameServer = previous }()

	if _, err := parseProxyServerNameServers([]string{"system"}); err == nil || !strings.Contains(err.Error(), "not initialized") {
		t.Fatalf("missing parser error = %v", err)
	}
}

func TestProxyServerLookupConstructorsReturnLookupFunctions(t *testing.T) {
	lookup, err := newProxyServerLookup([]string{"system"})
	if err != nil {
		t.Fatal(err)
	}
	if lookup == nil {
		t.Fatal("newProxyServerLookup returned nil lookup")
	}
	if single := newNameServerLookup(dns.NameServer{Net: "system"}, nil); single == nil {
		t.Fatal("newNameServerLookup returned nil lookup")
	}
}

func TestLookupAllRunsLookupsConcurrentlyAndDeduplicatesIPs(t *testing.T) {
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	lookup := lookupAll([]ipLookup{
		func(context.Context, string) ([]netip.Addr, error) {
			started <- struct{}{}
			<-release
			return []netip.Addr{netip.MustParseAddr("203.0.113.1")}, nil
		},
		func(context.Context, string) ([]netip.Addr, error) {
			started <- struct{}{}
			<-release
			return []netip.Addr{netip.MustParseAddr("::ffff:203.0.113.1"), netip.MustParseAddr("203.0.113.2")}, nil
		},
	})

	done := make(chan []netip.Addr, 1)
	go func() {
		ips, err := lookup(context.Background(), "node.example")
		if err != nil {
			t.Errorf("lookupAll error = %v", err)
		}
		done <- ips
	}()
	<-started
	<-started
	close(release)

	got := <-done
	want := []netip.Addr{netip.MustParseAddr("203.0.113.1"), netip.MustParseAddr("203.0.113.2")}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("lookupAll IPs = %+v, want %+v", got, want)
	}
}

func TestLookupAllReturnsUsefulErrorsWhenNoLookupSucceeds(t *testing.T) {
	if _, err := lookupAll(nil)(context.Background(), "node.example"); err == nil || !strings.Contains(err.Error(), "no IP records") {
		t.Fatalf("empty lookupAll error = %v", err)
	}
	lookup := lookupAll([]ipLookup{
		func(context.Context, string) ([]netip.Addr, error) { return nil, errors.New("first") },
		func(context.Context, string) ([]netip.Addr, error) { return nil, errors.New("second") },
	})
	if _, err := lookup(context.Background(), "node.example"); err == nil || !strings.Contains(err.Error(), "all proxy-server-nameserver lookups failed") {
		t.Fatalf("joined lookup error = %v", err)
	}
}

func TestServerExpandCacheKeysByNameserverAndDoesNotCacheFailures(t *testing.T) {
	cache := &ServerExpandCache{}
	calls := 0
	success := func(context.Context, string) ([]netip.Addr, error) {
		calls++
		return []netip.Addr{netip.MustParseAddr("203.0.113.1")}, nil
	}
	cachedSystem := cache.wrap([]string{"system"}, success)
	if _, err := cachedSystem(context.Background(), "node.example"); err != nil {
		t.Fatal(err)
	}
	if _, err := cachedSystem(context.Background(), "node.example"); err != nil {
		t.Fatal(err)
	}
	cachedOther := cache.wrap([]string{"1.1.1.1"}, success)
	if _, err := cachedOther(context.Background(), "node.example"); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("cache lookup calls = %d, want 2", calls)
	}

	failCalls := 0
	failing := cache.wrap([]string{"system"}, func(context.Context, string) ([]netip.Addr, error) {
		failCalls++
		return nil, errors.New("temporary")
	})
	_, _ = failing(context.Background(), "bad.example")
	_, _ = failing(context.Background(), "bad.example")
	if failCalls != 2 {
		t.Fatalf("failed lookups were cached: %d", failCalls)
	}
}

func TestUniqueIPsUnmapsAndPreservesFirstSeenOrder(t *testing.T) {
	got := uniqueIPs([]netip.Addr{
		netip.MustParseAddr("::ffff:192.0.2.1"),
		netip.MustParseAddr("192.0.2.1"),
		netip.MustParseAddr("192.0.2.2"),
	})
	want := []netip.Addr{netip.MustParseAddr("192.0.2.1"), netip.MustParseAddr("192.0.2.2")}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("uniqueIPs = %+v, want %+v", got, want)
	}
}
