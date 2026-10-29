package live

import (
	"codex-quota-monitor/internal/messageinbox"
	"context"
	"strings"
	"sync"
	"time"
)

const PublicResetSourcesID = "public-reset-sources/v1"

type PublicResetSources struct {
	mu             sync.Mutex
	monitor        Source
	internal       Source
	activeInternal bool
	health         messageinbox.SourceState
}

func LoadPublicResetSources(root string) (Source, error) {
	monitor, err := loadMonitor(root)
	if err != nil {
		return nil, err
	}
	internal, err := LoadInternalPublicSource(root)
	if err != nil {
		if monitor != nil {
			return &PublicResetSources{monitor: monitor}, err
		}
		return nil, err
	}
	if monitor == nil && internal == nil {
		return nil, nil
	}
	return &PublicResetSources{monitor: monitor, internal: internal}, nil
}
func (s *PublicResetSources) SourceDescriptor() (string, string) {
	return "public_aggregator", "公开重置消息"
}
func (s *PublicResetSources) PersistentSourceID() string  { return PublicResetSourcesID }
func (s *PublicResetSources) PollInterval() time.Duration { return 300 * time.Second }
func (s *PublicResetSources) FetchTimeout() time.Duration {
	budget := time.Duration(0)
	for _, source := range []Source{s.monitor, s.internal} {
		if source != nil {
			budget = max(budget, sourceFetchBudget(source))
		}
	}
	return budget + 2*time.Second
}
func (s *PublicResetSources) ReadWithoutAlerts() bool { return s.monitor != nil }
func (s *PublicResetSources) Fetch(ctx context.Context) (SourceBatch, error) {
	return s.fetch(ctx, true)
}
func (s *PublicResetSources) FetchWithoutAlerts(ctx context.Context) (SourceBatch, error) {
	return s.fetch(ctx, false)
}
func (s *PublicResetSources) fetch(ctx context.Context, alerts bool) (SourceBatch, error) {
	s.mu.Lock()
	s.activeInternal = alerts
	s.mu.Unlock()
	states := []SourceBatch{}
	available := 0
	total := 0
	latestAttempt, latestSuccess := (*int64)(nil), (*int64)(nil)
	var earliestPoll *int64
	type result struct {
		batch SourceBatch
		err   error
	}
	active := []Source{}
	for _, src := range []Source{s.monitor, s.internal} {
		if src == nil || !alerts && src == s.internal {
			continue
		}
		active = append(active, src)
	}
	results := make([]result, len(active))
	var pending sync.WaitGroup
	for i, src := range active {
		pending.Add(1)
		go func(i int, src Source) {
			defer pending.Done()
			child, cancel := context.WithTimeout(ctx, sourceFetchBudget(src))
			defer cancel()
			results[i].batch, results[i].err = src.Fetch(child)
		}(i, src)
	}
	pending.Wait()
	for i, src := range active {
		total++
		b, err := results[i].batch, results[i].err
		if err != nil {
			if h, ok := src.(interface {
				HealthSnapshot() messageinbox.SourceState
			}); ok {
				v := h.HealthSnapshot()
				v.State = "offline"
				if !strings.Contains(v.Reason, "_retry") {
					v.Reason = err.Error()
				}
				b.Sync = &v
			}
			b.SourceKind = "public_aggregator"
		}
		if b.Sync != nil {
			if at := b.Sync.NextPollAt; at != nil && *at > 0 && (earliestPoll == nil || *at < *earliestPoll) {
				earliestPoll = at
			}
			states = append(states, b)
			if b.Sync.LastAttemptAt != nil && (latestAttempt == nil || *b.Sync.LastAttemptAt > *latestAttempt) {
				latestAttempt = b.Sync.LastAttemptAt
			}
			if b.Sync.LastSuccessAt != nil && (latestSuccess == nil || *b.Sync.LastSuccessAt > *latestSuccess) {
				latestSuccess = b.Sync.LastSuccessAt
			}
		}
		if err == nil && b.PublicFeedAvailable {
			available++
		}
	}
	state, reason := "available", ""
	if available == 0 {
		state = "offline"
		reason = "public_sources_unavailable"
	} else if available < total {
		state = "partial"
		reason = "public_sources_partial"
	}
	health := messageinbox.SourceState{ID: PublicResetSourcesID, Label: "公开重置消息", State: state, Reason: reason, LastAttemptAt: latestAttempt, LastSuccessAt: latestSuccess, NextPollAt: earliestPoll, Origin: "public_source_summary"}
	s.mu.Lock()
	s.health = health
	s.mu.Unlock()
	return SourceBatch{SourceID: PublicResetSourcesID, SourceKind: "public_aggregator", SourceLabel: health.Label, PublicFeedAvailable: available > 0, Sync: &health, CheckedAt: latestAttempt, UnavailableReason: reason, Segments: states, ResetProjection: true, RecordOnly: !alerts}, nil
}
func (s *PublicResetSources) NextPollTime() time.Time {
	s.mu.Lock()
	activeInternal := s.activeInternal
	s.mu.Unlock()
	var next time.Time
	for _, src := range []Source{s.monitor, s.internal} {
		if src == nil || src == s.internal && !activeInternal {
			continue
		}
		if n, ok := src.(interface{ NextPollTime() time.Time }); ok {
			t := n.NextPollTime()
			if next.IsZero() || t.Before(next) {
				next = t
			}
		}
	}
	return next
}
func (s *PublicResetSources) HealthSnapshot() messageinbox.SourceState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.health
}
