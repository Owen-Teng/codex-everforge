package quota

import (
	"context"
	"sync"
	"time"
)

type Engine struct {
	reader    Reader
	store     *Store
	trigger   chan struct{}
	mu        sync.Mutex
	last      *Snapshot
	current   Snapshot
	notBefore time.Time
	failures  int
	demo      bool
}

func NewEngine(reader Reader, store *Store, demo bool) *Engine {
	return &Engine{reader: reader, store: store, trigger: make(chan struct{}, 1), demo: demo}
}
func (e *Engine) Refresh() {
	select {
	case e.trigger <- struct{}{}:
	default:
	}
}
func (e *Engine) Current() Snapshot { e.mu.Lock(); defer e.mu.Unlock(); return e.current }
func (e *Engine) publish(s Snapshot, emit func(Snapshot)) {
	s.Demo = e.demo
	s.Interval = e.store.Get().Interval
	s.ConfigWarning = e.store.GetWarning()
	e.mu.Lock()
	e.current = s
	e.mu.Unlock()
	emit(s)
}
func (e *Engine) Run(ctx context.Context, emit func(Snapshot)) {
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		case <-e.trigger:
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		if wait := time.Until(e.notBefore); wait > 0 {
			timer.Reset(wait)
			continue
		}
		// Clear the main value before every read. A previous result never looks live during a stalled refresh.
		loading := Snapshot{State: "loading", Color: "gray", Message: "正在读取额度…"}
		if e.last != nil {
			loading.LastSuccess = e.last.LastSuccess
			loading.Windows = e.last.Windows
			loading.Stale = true
			loading.Choices = e.last.Choices
			loading.Bucket = e.last.Bucket
			loading.BucketName = e.last.BucketName
		}
		e.publish(loading, emit)
		settings := e.store.Get()
		s, err := e.reader.Read(ctx, settings)
		if ctx.Err() != nil {
			return
		}
		latest := e.store.Get()
		if latest.Bucket != settings.Bucket || latest.Executable != settings.Executable {
			e.last = nil
			timer.Reset(0)
			continue
		}
		now := time.Now()
		if err == nil {
			e.failures = 0
			s.UpdatedAt = now.Unix()
			s.LastSuccess = s.UpdatedAt
			s.Stale = false
			copy := s
			e.last = &copy
		} else {
			e.failures++
			s.State = "error"
			s.Remaining = nil
			s.Color = "gray"
			s.Message = Reason(err)
			invalidate := false
			if f, ok := err.(*Failure); ok {
				switch f.Kind {
				case "auth", "not_logged_in", "unsupported_account", "account_changed", "select_bucket", "bucket_missing":
					invalidate = true
				}
			}
			if e.last != nil && !invalidate && s.AccountKey != "" && s.AccountKey == e.last.AccountKey && s.Bucket == e.last.Bucket {
				s.Windows = e.last.Windows
				s.LastSuccess = e.last.LastSuccess
				s.Stale = true
			} else {
				e.last = nil
			}
		}
		delay := NextDelay(e.store.Get().Interval, e.failures, s, err, now)
		if f, ok := err.(*Failure); ok && (f.Kind == "rate_limited" || f.RetryAfter > 0) {
			e.notBefore = now.Add(delay)
		} else {
			e.notBefore = time.Time{}
		}
		s.NextRefresh = now.Add(delay).Unix()
		e.publish(s, emit)
		// Requests made during the in-flight read are coalesced into that read.
		select {
		case <-e.trigger:
		default:
		}
		timer.Reset(delay)
	}
}
func NextDelay(interval, failures int, s Snapshot, err error, now time.Time) time.Duration {
	delay := time.Duration(interval) * time.Second
	if failures > 0 {
		shift := failures - 1
		if shift > 4 {
			shift = 4
		}
		delay *= time.Duration(1 << shift)
		if delay > 15*time.Minute {
			delay = 15 * time.Minute
		}
	}
	if f, ok := err.(*Failure); ok && f.RetryAfter > delay {
		delay = f.RetryAfter
	}
	if err == nil {
		for _, w := range s.Windows {
			if w.Reset != nil {
				d := time.Unix(*w.Reset, 0).Add(time.Second).Sub(now)
				if d > 0 && d < delay {
					delay = d
				}
			}
		}
	}
	if delay < 5*time.Second {
		delay = 5 * time.Second
	}
	return delay
}

type DemoReader struct {
	mu       sync.Mutex
	Scenario string
}

func (d *DemoReader) Set(value string) { d.mu.Lock(); d.Scenario = value; d.mu.Unlock() }
func (d *DemoReader) Read(ctx context.Context, settings Settings) (Snapshot, error) {
	select {
	case <-ctx.Done():
		return Snapshot{}, Fail("cancelled")
	case <-time.After(300 * time.Millisecond):
	}
	d.mu.Lock()
	value := d.Scenario
	d.mu.Unlock()
	if value == "error" {
		return Snapshot{AccountKey: "demo", Bucket: "codex"}, Fail("network")
	}
	if value == "login" {
		return Snapshot{}, Fail("not_logged_in")
	}
	p := 75.0
	switch value {
	case "100":
		p = 100
	case "60":
		p = 60
	case "59":
		p = 59
	case "20":
		p = 20
	case "19":
		p = 19
	case "0":
		p = 0
	}
	used := 100 - p
	duration := int64(10080)
	reset := time.Now().Add(6 * 24 * time.Hour).Unix()
	b := &RawBucket{Primary: &RawWindow{&used, &duration, &reset}}
	if value == "multi" {
		other := 79.0
		mins := int64(300)
		r := time.Now().Add(2 * time.Hour).Unix()
		b.Secondary = &RawWindow{&other, &mins, &r}
	}
	s, err := Normalize(Response{Buckets: map[string]*RawBucket{"codex": b}}, "codex")
	s.AccountKey = "demo"
	return s, err
}
