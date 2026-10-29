package live

import (
	"codex-quota-monitor/internal/reset"
	"errors"
	"time"
)

func (m *Manager) applyEventSegmentsLocked(batch *SourceBatch, now time.Time) error {
	if m.inbox == nil {
		return errors.New("inbox_unavailable")
	}
	segments := batch.Segments
	if segments == nil {
		segments = []SourceBatch{*batch}
	}
	for _, part := range segments {
		if part.Sync == nil {
			continue
		}
		if part.Posts != nil {
			if err := m.inbox.Ingest(part.Posts, *part.Sync, part.HistoryFrom, now); err != nil {
				return err
			}
		}
		if part.Evidence != nil {
			if err := m.inbox.IngestEvidence(part.Evidence, *part.Sync, now); err != nil {
				return err
			}
		} else {
			if err := m.inbox.UpdateSourceHealth(*part.Sync); err != nil {
				return err
			}
		}
	}
	if batch.Sync != nil {
		if err := m.inbox.UpdateSource(*batch.Sync); err != nil {
			return err
		}
	}
	batch.Events = []Event{}
	return nil
}
func (m *Manager) applyEventNotificationsLocked(batch SourceBatch, now time.Time) {
	if m.inbox == nil {
		return
	}
	events := m.inbox.ResetEvents()
	active := map[string]bool{}
	for _, event := range events {
		if event.Phase != "withdrawn" && !event.Conflict {
			active[event.NotificationID] = true
		}
	}
	kept := m.cloudQueue[:0]
	for _, pending := range m.cloudQueue {
		if active[pending.Notification.ID] {
			kept = append(kept, pending)
		} else {
			_ = m.inbox.EventDelivery(pending.Notification.ID, "withdrawn", "reset_evidence_withdrawn_or_changed", now)
		}
	}
	m.cloudQueue = kept
	if m.state.Notification != nil {
		if _, known := m.inbox.FindEventDelivery(m.state.Notification.ID); known && !active[m.state.Notification.ID] {
			m.state.Notification = nil
			m.state.Reset.Event = nil
		}
	}
	sourceFresh := map[string]bool{}
	segments := batch.Segments
	if segments == nil {
		segments = []SourceBatch{batch}
	}
	for _, part := range segments {
		if part.Sync != nil && part.PublicFeedAvailable {
			sourceFresh[part.Sync.ID] = true
		}
	}
	for _, event := range events {
		if event.Phase == "withdrawn" || event.Conflict {
			continue
		}
		record, known := m.inbox.FindEventDelivery(event.NotificationID)
		if known && record.State != "queued" {
			continue
		}
		// A classifier upgrade must not erase a historical per-post baseline or
		// a prior host submission. Add a silent stage checkpoint, never invent an
		// old emitted/submitted timestamp. Merely stored unrelated text may evolve.
		if !known {
			legacy := false
			for _, e := range event.Evidence {
				if phaseOfEvidence(e) != event.Phase || e.PostID == "" {
					continue
				}
				if post, ok := m.inbox.Message(e.PostID); ok {
					switch post.Delivery.State {
					case "baseline", "worker_emitted", "submitted", "failed", "unknown", "expired", "withdrawn":
						legacy = true
					}
				}
			}
			if legacy {
				if m.inbox.EventDelivery(event.NotificationID, "baseline", "preserved_prior_post_receipt", now) != nil {
					m.state.Reset.Reason = "event_delivery_save_failed"
				}
				continue
			}
		}
		ready, fresh := true, false
		var at *int64
		switch event.Phase {
		case "author_completed":
			at = event.ConfirmedAt
		case "externally_observed":
			at = event.ObservedAt
		default:
			at = event.AnnouncedAt
		}
		for _, e := range event.Evidence {
			if phaseOfEvidence(e) == event.Phase {
				if !m.inbox.Initialized("events:" + e.SourceID) {
					ready = false
				}
				if sourceFresh[e.SourceID] {
					fresh = true
				}
			}
		}
		baseline := !ready || !m.baseline || batch.RecordOnly || !m.state.Preferences.ResetAlertsEnabled
		if baseline {
			if m.inbox.EventDelivery(event.NotificationID, "baseline", "historical_or_disabled_baseline", now) != nil {
				m.state.Reset.Reason = "event_delivery_save_failed"
			}
			continue
		}
		if !fresh || at == nil || *at <= 0 || *at < m.enabledAt || *at > now.Unix()+300 || now.Unix()-*at > int64(EventTTL/time.Second) {
			continue
		}
		duplicate := false
		for _, pending := range m.cloudQueue {
			if pending.Notification.ID == event.NotificationID {
				duplicate = true
			}
		}
		if duplicate {
			continue
		}
		if m.inbox.EventDelivery(event.NotificationID, "queued", "new_attributed_reset_stage", now) != nil {
			m.state.Reset.Reason = "event_delivery_save_failed"
			continue
		}
		kind := "notice"
		title := event.Label
		if event.Phase == "author_completed" {
			kind = "announced"
			title = "Tibo 已确认重置，看看额度吧"
		} else if event.Phase == "externally_observed" {
			title = "有重置的好消息啦"
		} else if event.Phase == "announced" {
			title = "Tibo 又要重置啦，快点灯！"
		}
		if event.Kind == "banked_credit" {
			title = event.Label
		}
		body := event.Summary
		link := ""
		for _, e := range event.Evidence {
			if phaseOfEvidence(e) == event.Phase {
				link = e.URL
				body += " 来源：" + e.Attribution.Name
				if e.Type == "external_observation" {
					body += " · 观测记录"
				}
				break
			}
		}
		if link != "" {
			body += " " + link
		}
		notice := Notification{ID: event.NotificationID, Kind: kind, Title: title, Body: body, CreatedAt: now.Unix()}
		view := Event{ID: event.ID, Kind: kind, PublishedAt: *at, SourceURL: link, Summary: event.Summary, Verification: event.Phase}
		m.state.Notification = &notice
		m.state.Reset.Event = &view
		m.state.Reset.State = "notice"
		if kind == "announced" {
			m.state.Reset.State = "waiting_local"
		}
		m.cloudQueue = append(m.cloudQueue, queuedSourceNotice{Event: view, Notification: notice})
		if kind == "announced" && m.refresh != nil {
			m.refresh()
		}
	}
	for id := range sourceFresh {
		if m.inbox.MarkInitialized("events:"+id) != nil {
			m.state.Reset.Reason = "event_checkpoint_save_failed"
		}
	}
}
func phaseOfEvidence(e reset.Evidence) string {
	switch e.Type {
	case "author_announcement":
		return "announced"
	case "author_completion", "contextual_author_completion", "source_recorded_completion":
		return "author_completed"
	case "external_observation":
		return "externally_observed"
	default:
		return "signal"
	}
}
