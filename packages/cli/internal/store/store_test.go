package store

import (
	"context"
	"encoding/json"
	"reflect"
	"sync/atomic"
	"testing"

	"github.com/metacubex/mihomo/common/utils"
	C "github.com/metacubex/mihomo/constant"
	"mihomo-st/internal/proxyconfig"
)

func TestCurrentSnapshotStartsEmptyAtVersionZero(t *testing.T) {
	ref := New(nil).Current()
	defer ref.Release()

	if ref.Version() != 0 {
		t.Fatalf("initial version = %d", ref.Version())
	}
	if records := ref.Records(); len(records) != 0 {
		t.Fatalf("initial records = %+v", records)
	}
	if list := ref.List(); len(list) != 0 {
		t.Fatalf("initial list = %+v", list)
	}
	if rec, ok := ref.Get("absent"); ok || rec != nil {
		t.Fatalf("missing lookup = (%+v, %v)", rec, ok)
	}
}

func TestProxyInfoIsNotAnHTTPDTO(t *testing.T) {
	infoType := reflect.TypeOf(ProxyInfo{})
	for i := 0; i < infoType.NumField(); i++ {
		field := infoType.Field(i)
		if field.Tag.Get("json") != "" {
			t.Fatalf("store.ProxyInfo.%s must not define JSON tags", field.Name)
		}
	}
}

func TestSnapshotListIsSortedAndDetachedFromCallers(t *testing.T) {
	ref := New([]*proxyconfig.Record{
		recordForStore("z-digest", map[string]any{"name": "z", "type": "ss", "server": "z.example", "port": 9000}),
		recordForStore("a-digest", map[string]any{"name": "a", "type": "http", "server": "a.example", "port": "8080"}),
	}).Current()
	defer ref.Release()

	list := ref.List()
	want := []ProxyInfo{
		{Digest: "a-digest", Name: "a", Type: "http", Server: "a.example", Port: "8080"},
		{Digest: "z-digest", Name: "z", Type: "ss", Server: "z.example", Port: 9000},
	}
	if !reflect.DeepEqual(list, want) {
		t.Fatalf("snapshot list = %#v, want %#v", list, want)
	}

	list[0].Name = "changed-by-test"
	if fresh := ref.List(); fresh[0].Name != "a" {
		t.Fatalf("List returned mutable backing storage: %+v", fresh)
	}
}

func TestSnapshotRecordsReturnsCopyButPreservesRecordIdentity(t *testing.T) {
	first := recordForStore("first", map[string]any{"name": "first"})
	second := recordForStore("second", map[string]any{"name": "second"})
	ref := New([]*proxyconfig.Record{first, second}).Current()
	defer ref.Release()

	records := ref.Records()
	if len(records) != 2 || records[0] != first || records[1] != second {
		t.Fatalf("records = %+v", records)
	}
	records[0] = nil
	if again := ref.Records(); again[0] != first {
		t.Fatalf("Records returned mutable slice backing storage: %+v", again)
	}
}

func TestPublishSwapsCurrentSnapshotAndRetiresOldAfterLastReference(t *testing.T) {
	var oldClosed int32
	oldRef := New([]*proxyconfig.Record{
		recordWithCloseCounter("old", &oldClosed),
	}).Current()
	store := New(nil)
	oldStoreRecord := recordWithCloseCounter("kept-old", &oldClosed)
	store = New([]*proxyconfig.Record{oldStoreRecord})
	heldOld := store.Current()

	var newClosed int32
	newRef := store.Publish([]*proxyconfig.Record{recordWithCloseCounter("new", &newClosed)})
	defer newRef.Release()

	oldRef.Release()
	if newRef.Version() != 1 {
		t.Fatalf("published version = %d", newRef.Version())
	}
	if _, ok := newRef.Get("kept-old"); ok {
		t.Fatal("new snapshot still exposes replaced digest")
	}
	if _, ok := heldOld.Get("kept-old"); !ok {
		t.Fatal("held old snapshot lost its records")
	}
	if atomic.LoadInt32(&oldClosed) != 0 {
		t.Fatalf("old proxy closed before reference release: %d", oldClosed)
	}

	heldOld.Release()
	if atomic.LoadInt32(&oldClosed) != 1 {
		t.Fatalf("old proxy close count = %d, want 1", oldClosed)
	}
	if atomic.LoadInt32(&newClosed) != 0 {
		t.Fatalf("current proxy closed unexpectedly: %d", newClosed)
	}
}

func TestAppendUsesLatestSnapshotAndClosesSkippedDuplicates(t *testing.T) {
	store := New([]*proxyconfig.Record{recordForStore("one", map[string]any{"name": "one"})})
	published := store.Publish([]*proxyconfig.Record{recordForStore("two", map[string]any{"name": "two"})})
	published.Release()

	var duplicateClosed int32
	ref := store.Append([]*proxyconfig.Record{
		recordForStore("three", map[string]any{"name": "three"}),
		recordWithCloseCounter("two", &duplicateClosed),
		nil,
	})
	defer ref.Release()

	if ref.Version() != 2 {
		t.Fatalf("append version = %d, want 2", ref.Version())
	}
	for _, digest := range []string{"two", "three"} {
		if _, ok := ref.Get(digest); !ok {
			t.Fatalf("digest %q missing after append", digest)
		}
	}
	if _, ok := ref.Get("one"); ok {
		t.Fatal("append did not use latest snapshot after prior publish")
	}
	if atomic.LoadInt32(&duplicateClosed) != 1 {
		t.Fatalf("duplicate record close count = %d", duplicateClosed)
	}
}

func TestCloseRetiresCurrentSnapshotAndFuturePublishesCloseInputs(t *testing.T) {
	var currentClosed int32
	store := New([]*proxyconfig.Record{recordWithCloseCounter("current", &currentClosed)})
	current := store.Current()

	store.Close()
	if atomic.LoadInt32(&currentClosed) != 0 {
		t.Fatalf("current record closed while reference is held: %d", currentClosed)
	}
	current.Release()
	if atomic.LoadInt32(&currentClosed) != 1 {
		t.Fatalf("current record close count = %d", currentClosed)
	}
	store.Close()
	if atomic.LoadInt32(&currentClosed) != 1 {
		t.Fatalf("second Close changed close count: %d", currentClosed)
	}

	var lateClosed int32
	ref := store.Publish([]*proxyconfig.Record{recordWithCloseCounter("late", &lateClosed)})
	defer ref.Release()
	if ref.Version() != 0 || len(ref.Records()) != 0 {
		t.Fatalf("publish on closed store returned version %d records %+v", ref.Version(), ref.Records())
	}
	if atomic.LoadInt32(&lateClosed) != 1 {
		t.Fatalf("closed store did not close publish input: %d", lateClosed)
	}
}

func TestProxyInfoUsesEmptyStringsForNonStringRawFields(t *testing.T) {
	info := newProxyInfo(recordForStore("digest", map[string]any{
		"type":   []string{"bad"},
		"name":   12,
		"server": nil,
		"port":   443,
	}))
	if info != (ProxyInfo{Digest: "digest", Port: 443}) {
		t.Fatalf("proxy info = %+v", info)
	}
}

func TestCloseRecordsHandlesNilAndSharedProxyInstances(t *testing.T) {
	var closes int32
	shared := &storeTestProxy{id: "shared", closes: &closes}
	closeRecords([]*proxyconfig.Record{
		nil,
		{Digest: "nil-proxy"},
		{Digest: "one", Proxy: shared},
		{Digest: "two", Proxy: shared},
	})
	if atomic.LoadInt32(&closes) != 1 {
		t.Fatalf("shared proxy close count = %d", closes)
	}
}

func recordForStore(digest string, raw map[string]any) *proxyconfig.Record {
	return &proxyconfig.Record{Digest: digest, Raw: raw, Proxy: &storeTestProxy{id: digest}}
}

func recordWithCloseCounter(digest string, closes *int32) *proxyconfig.Record {
	return &proxyconfig.Record{
		Digest: digest,
		Raw:    map[string]any{"name": digest, "type": "direct", "server": digest, "port": 0},
		Proxy:  &storeTestProxy{id: digest, closes: closes},
	}
}

type storeTestProxy struct {
	id     string
	closes *int32
}

func (p *storeTestProxy) Name() string { return p.id }
func (p *storeTestProxy) Type() C.AdapterType {
	return C.Direct
}
func (p *storeTestProxy) Addr() string { return "127.0.0.1:0" }
func (p *storeTestProxy) SupportUDP() bool {
	return false
}
func (p *storeTestProxy) ProxyInfo() C.ProxyInfo { return C.ProxyInfo{} }
func (p *storeTestProxy) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]string{"id": p.id})
}
func (p *storeTestProxy) DialContext(context.Context, *C.Metadata) (C.Conn, error) {
	return nil, C.ErrNotSupport
}
func (p *storeTestProxy) ListenPacketContext(context.Context, *C.Metadata) (C.PacketConn, error) {
	return nil, C.ErrNotSupport
}
func (p *storeTestProxy) SupportUOT() bool { return false }
func (p *storeTestProxy) IsL3Protocol(*C.Metadata) bool {
	return false
}
func (p *storeTestProxy) Unwrap(*C.Metadata, bool) C.Proxy { return nil }
func (p *storeTestProxy) Close() error {
	if p.closes != nil {
		atomic.AddInt32(p.closes, 1)
	}
	return nil
}
func (p *storeTestProxy) Adapter() C.ProxyAdapter { return p }
func (p *storeTestProxy) AliveForTestUrl(string) bool {
	return false
}
func (p *storeTestProxy) DelayHistory() []C.DelayHistory { return nil }
func (p *storeTestProxy) ExtraDelayHistories() map[string]C.ProxyState {
	return nil
}
func (p *storeTestProxy) LastDelayForTestUrl(string) uint16 { return 0 }
func (p *storeTestProxy) URLTest(context.Context, string, utils.IntRanges[uint16]) (uint16, error) {
	return 0, C.ErrNotSupport
}

var _ C.Proxy = (*storeTestProxy)(nil)
