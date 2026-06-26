package store

import (
	"sort"
	"sync"

	C "github.com/metacubex/mihomo/constant"
	"mihomo-st/internal/proxyconfig"
)

type Store struct {
	mu      sync.Mutex
	current *Snapshot
	closed  bool
}

type Snapshot struct {
	version  int
	records  []*proxyconfig.Record
	list     []ProxyInfo
	byDigest map[string]*proxyconfig.Record

	mu      sync.Mutex
	refs    int
	retired bool
	closed  bool
}

type SnapshotRef struct {
	snapshot *Snapshot
	once     sync.Once
}

type ProxyInfo struct {
	Type   string
	Name   string
	Server string
	Port   any
	Digest string
}

func New(records []*proxyconfig.Record) *Store {
	return &Store{current: newSnapshot(0, records)}
}

func (s *Store) Current() *SnapshotRef {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.current == nil {
		s.current = newSnapshot(0, nil)
	}
	return s.current.acquire()
}

func (s *Store) Publish(records []*proxyconfig.Record) *SnapshotRef {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		closeRecords(records)
		return newSnapshot(0, nil).acquire()
	}
	old := s.current
	nextVersion := 0
	if old != nil {
		nextVersion = old.version + 1
	}
	next := newSnapshot(nextVersion, records)
	s.current = next
	ref := next.acquire()
	if old != nil {
		old.retire()
	}
	return ref
}

func (s *Store) Append(records []*proxyconfig.Record) *SnapshotRef {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		closeRecords(records)
		return newSnapshot(0, nil).acquire()
	}
	old := s.current
	if old == nil {
		old = newSnapshot(0, nil)
	}

	merged := append([]*proxyconfig.Record(nil), old.records...)
	seen := make(map[string]struct{}, len(old.byDigest)+len(records))
	for digest := range old.byDigest {
		seen[digest] = struct{}{}
	}
	skipped := make([]*proxyconfig.Record, 0)
	for _, record := range records {
		if record == nil {
			continue
		}
		if _, ok := seen[record.Digest]; ok {
			skipped = append(skipped, record)
			continue
		}
		seen[record.Digest] = struct{}{}
		merged = append(merged, record)
	}

	next := newSnapshot(old.version+1, merged)
	s.current = next
	ref := next.acquire()
	old.retire()
	closeRecords(skipped)
	return ref
}

func (s *Store) Close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	current := s.current
	s.current = nil
	s.mu.Unlock()

	if current != nil {
		current.retire()
	}
}

func (s *Snapshot) Version() int {
	if s == nil {
		return 0
	}
	return s.version
}

func (s *Snapshot) List() []ProxyInfo {
	if s == nil {
		return nil
	}
	items := make([]ProxyInfo, len(s.list))
	copy(items, s.list)
	return items
}

func (s *Snapshot) Records() []*proxyconfig.Record {
	if s == nil {
		return nil
	}
	records := make([]*proxyconfig.Record, len(s.records))
	copy(records, s.records)
	return records
}

func (s *Snapshot) Get(digest string) (*proxyconfig.Record, bool) {
	if s == nil {
		return nil, false
	}
	record, ok := s.byDigest[digest]
	return record, ok
}

func (r *SnapshotRef) Snapshot() *Snapshot {
	if r == nil {
		return nil
	}
	return r.snapshot
}

func (r *SnapshotRef) Version() int {
	if r == nil || r.snapshot == nil {
		return 0
	}
	return r.snapshot.Version()
}

func (r *SnapshotRef) List() []ProxyInfo {
	if r == nil || r.snapshot == nil {
		return nil
	}
	return r.snapshot.List()
}

func (r *SnapshotRef) Records() []*proxyconfig.Record {
	if r == nil || r.snapshot == nil {
		return nil
	}
	return r.snapshot.Records()
}

func (r *SnapshotRef) Get(digest string) (*proxyconfig.Record, bool) {
	if r == nil || r.snapshot == nil {
		return nil, false
	}
	return r.snapshot.Get(digest)
}

func (r *SnapshotRef) Release() {
	if r == nil || r.snapshot == nil {
		return
	}
	r.once.Do(func() {
		r.snapshot.release()
	})
}

func newSnapshot(version int, records []*proxyconfig.Record) *Snapshot {
	snapshotRecords := make([]*proxyconfig.Record, 0, len(records))
	byDigest := make(map[string]*proxyconfig.Record, len(records))
	for _, record := range records {
		if record == nil {
			continue
		}
		if _, exists := byDigest[record.Digest]; exists {
			closeRecord(record)
			continue
		}
		snapshotRecords = append(snapshotRecords, record)
		byDigest[record.Digest] = record
	}

	items := make([]ProxyInfo, 0, len(snapshotRecords))
	for _, record := range snapshotRecords {
		items = append(items, newProxyInfo(record))
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].Digest < items[j].Digest
	})

	return &Snapshot{
		version:  version,
		records:  snapshotRecords,
		list:     items,
		byDigest: byDigest,
	}
}

func (s *Snapshot) acquire() *SnapshotRef {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.closed {
		s.refs++
	}
	return &SnapshotRef{snapshot: s}
}

func (s *Snapshot) release() {
	var records []*proxyconfig.Record
	s.mu.Lock()
	if s.refs > 0 {
		s.refs--
	}
	if s.retired && s.refs == 0 && !s.closed {
		s.closed = true
		records = s.records
	}
	s.mu.Unlock()
	closeRecords(records)
}

func (s *Snapshot) retire() {
	var records []*proxyconfig.Record
	s.mu.Lock()
	s.retired = true
	if s.refs == 0 && !s.closed {
		s.closed = true
		records = s.records
	}
	s.mu.Unlock()
	closeRecords(records)
}

func newProxyInfo(record *proxyconfig.Record) ProxyInfo {
	return ProxyInfo{
		Type:   stringFromRaw(record.Raw, "type"),
		Name:   stringFromRaw(record.Raw, "name"),
		Server: stringFromRaw(record.Raw, "server"),
		Port:   record.Raw["port"],
		Digest: record.Digest,
	}
}

func stringFromRaw(raw map[string]any, key string) string {
	if value, ok := raw[key].(string); ok {
		return value
	}
	return ""
}

func closeRecords(records []*proxyconfig.Record) {
	seen := map[C.Proxy]struct{}{}
	for _, record := range records {
		closeRecordWithSeen(record, seen)
	}
}

func closeRecord(record *proxyconfig.Record) {
	closeRecordWithSeen(record, map[C.Proxy]struct{}{})
}

func closeRecordWithSeen(record *proxyconfig.Record, seen map[C.Proxy]struct{}) {
	if record == nil || record.Proxy == nil {
		return
	}
	if _, ok := seen[record.Proxy]; ok {
		return
	}
	seen[record.Proxy] = struct{}{}
	_ = record.Proxy.Close()
}
