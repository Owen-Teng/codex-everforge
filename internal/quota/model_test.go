package quota

import (
	"context"
	"encoding/json"
	"math"
	"sync/atomic"
	"testing"
	"time"
)

func ptr[T any](v T) *T { return &v }
func bucket(used float64) *RawBucket {
	return &RawBucket{Primary: &RawWindow{ptr(used), ptr(int64(10080)), ptr(int64(2000000000))}}
}
func TestBoundaries(t *testing.T) {
	for _, c := range []struct {
		used      float64
		remaining int
		tone      string
	}{{0, 100, "green"}, {40, 60, "green"}, {41, 59, "yellow"}, {80, 20, "yellow"}, {81, 19, "red"}, {100, 0, "red"}, {40.1, 59, "yellow"}} {
		s, e := Normalize(Response{Buckets: map[string]*RawBucket{"codex": bucket(c.used)}}, "")
		if e != nil || s.Remaining == nil || *s.Remaining != c.remaining || s.Color != c.tone {
			t.Fatalf("used %v => %+v / %v", c.used, s, e)
		}
	}
}
func TestInvalidValuesNeverBecomeQuota(t *testing.T) {
	for _, v := range []float64{-1, 101, math.NaN(), math.Inf(1)} {
		s, e := Normalize(Response{Buckets: map[string]*RawBucket{"codex": bucket(v)}}, "")
		if e == nil || s.Remaining != nil {
			t.Fatalf("accepted %v", v)
		}
	}
	b := bucket(10)
	b.Primary.UsedPercent = nil
	s, e := Normalize(Response{Buckets: map[string]*RawBucket{"codex": b}}, "")
	if e == nil || s.Remaining != nil {
		t.Fatal("accepted missing percent")
	}
}
func TestBucketsAndWindows(t *testing.T) {
	b := bucket(25)
	b.Secondary = &RawWindow{ptr(79.0), ptr(int64(300)), nil}
	raw := Response{Buckets: map[string]*RawBucket{"codex": b, "other": bucket(99)}}
	s, e := Normalize(raw, "")
	if e != nil || *s.Remaining != 21 || len(s.Windows) != 2 || !s.Partial {
		t.Fatalf("wrong scoped window: %+v %v", s, e)
	}
	s, e = Normalize(raw, "other")
	if e != nil || *s.Remaining != 1 {
		t.Fatal("selection ignored")
	}
	_, e = Normalize(Response{Buckets: map[string]*RawBucket{"other": b}}, "")
	if e == nil {
		t.Fatal("unknown bucket was selected silently")
	}
	_, e = Normalize(raw, "removed")
	if e == nil {
		t.Fatal("missing selection silently changed")
	}
}
func TestNullNotHundredAndInvalidSecondary(t *testing.T) {
	s, e := Normalize(Response{Buckets: map[string]*RawBucket{"codex": {}}}, "")
	if e == nil || s.Remaining != nil {
		t.Fatal("null treated as quota")
	}
	b := bucket(0)
	b.Secondary = &RawWindow{UsedPercent: ptr(101.0)}
	s, e = Normalize(Response{Buckets: map[string]*RawBucket{"codex": b}}, "")
	if e == nil || s.Remaining != nil {
		t.Fatal("invalid secondary hidden by valid primary")
	}
}
func TestLegacyAndMetadata(t *testing.T) {
	b := bucket(50)
	b.ID = ptr("codex")
	s, e := Normalize(Response{Legacy: b}, "")
	if e != nil || *s.Remaining != 50 {
		t.Fatal("legacy rejected")
	}
	b.Primary.Duration = nil
	b.Primary.Reset = nil
	s, e = Normalize(Response{Legacy: b}, "")
	if e != nil || !s.Partial {
		t.Fatal("missing metadata not identified")
	}
	b.Primary.Duration = ptr(int64(-1))
	if _, e = Normalize(Response{Legacy: b}, ""); e == nil {
		t.Fatal("invalid duration")
	}
}
func TestIdentityNeverSerialized(t *testing.T) {
	s := Snapshot{AccountKey: "private-account-hash"}
	b, e := json.Marshal(s)
	if e != nil {
		t.Fatal(e)
	}
	var fields map[string]any
	_ = json.Unmarshal(b, &fields)
	if _, ok := fields["AccountKey"]; ok {
		t.Fatal("identity exposed")
	}
	if string(b) == "" {
		t.Fatal("empty output")
	}
}
func TestBackoffAndReset(t *testing.T) {
	now := time.Unix(1900000000, 0)
	if d := NextDelay(120, 4, Snapshot{}, Fail("network"), now); d != 15*time.Minute {
		t.Fatalf("backoff %v", d)
	}
	if d := NextDelay(120, 1, Snapshot{}, &Failure{Kind: "rate_limited", RetryAfter: time.Hour}, now); d != time.Hour {
		t.Fatalf("retry ignored %v", d)
	}
	s := Snapshot{Windows: []Window{{Reset: ptr(now.Add(20 * time.Second).Unix())}}}
	if d := NextDelay(120, 0, s, nil, now); d != 21*time.Second {
		t.Fatal(d)
	}
	s.Windows[0].Reset = ptr(now.Add(-time.Second).Unix())
	if d := NextDelay(120, 0, s, nil, now); d != 120*time.Second {
		t.Fatal("past reset makes polling storm", d)
	}
}
func TestStoreValidationAndRoundtrip(t *testing.T) {
	root := t.TempDir()
	s := OpenStore(root)
	s.Set(func(c *Settings) { c.Interval = 300; c.Bucket = "codex" })
	loaded := OpenStore(root).Get()
	if loaded.Interval != 300 || loaded.Bucket != "codex" {
		t.Fatal(loaded)
	}
	s.Set(func(c *Settings) { c.Interval = 1 })
	if s.Get().Interval != 60 {
		t.Fatal("unbounded interval")
	}
}

type fakeReader struct {
	calls   atomic.Int32
	active  atomic.Int32
	maximum atomic.Int32
	fail    bool
}

func (f *fakeReader) Read(ctx context.Context, c Settings) (Snapshot, error) {
	n := f.active.Add(1)
	defer f.active.Add(-1)
	for m := f.maximum.Load(); n > m && !f.maximum.CompareAndSwap(m, n); m = f.maximum.Load() {
	}
	call := f.calls.Add(1)
	select {
	case <-ctx.Done():
		return Snapshot{}, Fail("cancelled")
	case <-time.After(15 * time.Millisecond):
	}
	if f.fail && call > 1 {
		return Snapshot{AccountKey: "same", Bucket: "codex"}, Fail("network")
	}
	s, e := Normalize(Response{Buckets: map[string]*RawBucket{"codex": bucket(25)}}, "")
	s.AccountKey = "same"
	return s, e
}
func TestRefreshCoalescesAndCancellation(t *testing.T) {
	r := &fakeReader{}
	e := NewEngine(r, OpenStore(t.TempDir()), false)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	ready := make(chan Snapshot, 8)
	go func() { e.Run(ctx, func(s Snapshot) { ready <- s }); close(done) }()
	<-ready
	for i := 0; i < 50; i++ {
		e.Refresh()
	}
	<-ready
	time.Sleep(30 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("did not stop")
	}
	if r.maximum.Load() != 1 || r.calls.Load() != 1 {
		t.Fatalf("concurrency=%d reads=%d", r.maximum.Load(), r.calls.Load())
	}
}
func TestFailureKeepsOnlyMarkedStaleDetail(t *testing.T) {
	r := &fakeReader{fail: true}
	e := NewEngine(r, OpenStore(t.TempDir()), false)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	updates := make(chan Snapshot, 8)
	go e.Run(ctx, func(s Snapshot) { updates <- s })
	<-updates
	<-updates
	e.Refresh()
	<-updates
	failure := <-updates
	if failure.Remaining != nil || !failure.Stale || failure.LastSuccess == 0 || len(failure.Windows) != 1 || failure.Color != "gray" {
		t.Fatalf("stale state %+v", failure)
	}
}
