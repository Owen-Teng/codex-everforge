package live

import (
	"codex-quota-monitor/internal/messageinbox"
	"time"
)

// Age only a returned view. This neither reads source caches nor writes state,
// and an HTTP success never becomes a fabricated upstream collection time.
func (m *Manager) inboxSnapshotLockedAt(now time.Time) messageinbox.Snapshot {
	view := m.inbox.Snapshot()
	agePublicSource := func(source *messageinbox.SourceState) {
		ttl := int64(0)
		switch source.ID {
		case CodexResetsSourceID:
			ttl = 1200
		case AIHOTSourceID:
			ttl = 1800
		}
		if ttl == 0 || source.State != "available" {
			return
		}
		last := source.LastSuccessAt
		if last == nil || *last <= 0 || *last > now.Unix()+30 || now.Unix()-*last > ttl {
			source.State = "stale"
			source.Reason = "public_source_cache_stale"
			if source.ID == CodexResetsSourceID {
				source.Reason = "monitor_unavailable_or_stale"
			}
		}
	}
	agePublicSource(&view.Source)
	for i := range view.Sources {
		agePublicSource(&view.Sources[i])
	}
	if view.Source.ID != PublicResetSourcesID {
		return view
	}
	provider, ok := m.source.(*PublicResetSources)
	if !ok {
		return view
	}
	active := map[string]bool{CodexResetsSourceID: provider.monitor != nil, AIHOTSourceID: provider.internal != nil && m.state.Preferences.ResetAlertsEnabled}
	available, total := 0, 0
	var earliestPoll *int64
	for _, source := range view.Sources {
		if !active[source.ID] {
			continue
		}
		total++
		if at := source.NextPollAt; at != nil && *at > 0 && (earliestPoll == nil || *at < *earliestPoll) {
			earliestPoll = at
		}
		if source.State == "available" {
			available++
		}
	}
	if total == 0 {
		return view
	}
	view.Source.NextPollAt = earliestPoll
	switch {
	case available == 0:
		view.Source.State, view.Source.Reason = "offline", "public_sources_unavailable"
	case available < total:
		view.Source.State, view.Source.Reason = "partial", "public_sources_partial"
	default:
		view.Source.State, view.Source.Reason = "available", ""
	}
	return view
}
