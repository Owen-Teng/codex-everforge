package live

import (
	"codex-quota-monitor/internal/messageinbox"
	"errors"
	"strings"
	"time"
)

func (m *Manager) InboxSnapshot() messageinbox.Snapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.inbox != nil {
		return m.inboxSnapshotLockedAt(time.Now())
	}
	return messageinbox.Snapshot{SchemaVersion: 1, Source: messageinbox.SourceState{State: "error", Reason: m.inboxError}, Messages: []messageinbox.Message{}}
}
func (m *Manager) applyInboxLocked(batch *SourceBatch, now time.Time) error {
	if batch.ResetProjection {
		return m.applyEventSegmentsLocked(batch, now)
	}
	if batch.Posts == nil || batch.Sync == nil {
		return nil
	}
	if m.inbox == nil {
		return errors.New("inbox_unavailable")
	}
	if err := m.inbox.Ingest(batch.Posts, *batch.Sync, batch.HistoryFrom, now); err != nil {
		return err
	}
	batch.Events = []Event{}
	for _, record := range m.inbox.MessagesFor(batch.Posts) {
		if !m.baseline && (record.Delivery.State == "" || record.Delivery.State == "queued") {
			if err := m.inbox.Delivery(record.PostID, "baseline", "initial_or_reenabled_history_silent", now); err != nil {
				return err
			}
		}
		if m.baseline && record.PublishedAt != nil && (record.Category == "related" || record.Category == "unknown") && record.Delivery.State == "" {
			if err := m.inbox.Delivery(record.PostID, "stored", "retained_without_alert", now); err != nil {
				return err
			}
		}
		if record.Status != "active" {
			batch.SuppressedIDs = append(batch.SuppressedIDs, "post-"+record.PostID)
			continue
		}
		if record.PublishedAt == nil {
			continue
		}
		kind := record.Category
		if kind == "signal" {
			kind = "notice"
		}
		if kind != "notice" && kind != "announced" {
			continue
		}
		summary := "公开聚合来源记录了作者的额度重置预告；范围及本机恢复仍待核对。"
		if record.Category == "signal" {
			summary = "作者发布重置信号，适用范围未明确；不能认定本机额度已恢复。"
		} else if kind == "announced" {
			summary = "公开聚合来源记录了作者的完成声明；本机额度恢复需独立核对。"
		}
		batch.Events = append(batch.Events, Event{ID: "post-" + record.PostID, Kind: kind, PublishedAt: *record.PublishedAt, SourceURL: record.URL, Summary: summary, Verification: "third_party"})
	}
	return nil
}
func canonicalEventKey(source string, event Event) string {
	if id := messageinbox.PostID(event.SourceURL); id != "" && strings.Contains(event.SourceURL, "/thsottiaux/status/") {
		return messageinbox.NotificationID(id)
	}
	return eventKey(source, event.ID)
}
func (m *Manager) inboxDeliveryLocked(event Event, state, reason string, now time.Time) error {
	if m.inbox == nil {
		return errors.New("inbox_unavailable")
	}
	id := messageinbox.PostID(event.SourceURL)
	if id == "" {
		return nil
	}
	return m.inbox.Delivery(id, state, reason, now)
}
func (m *Manager) NotificationEmitted(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.inbox == nil {
		return
	}
	if record, ok := m.inbox.FindNotification(id); ok && record.Delivery.State == "queued" {
		if err := m.inbox.Delivery(record.PostID, "worker_emitted", "stdout_written_visibility_unknown", time.Now()); err != nil {
			m.state.Reset.Reason = "inbox_delivery_save_failed"
			m.state.Notification = nil
			m.cloudQueue = nil
			m.state.Reset.SourceFresh = false
		}
		m.signal()
		return
	}
	if record, ok := m.inbox.FindEventDelivery(id); ok && record.State == "queued" {
		if m.inbox.EventDelivery(id, "worker_emitted", "stdout_written_visibility_unknown", time.Now()) != nil {
			m.state.Reset.Reason = "event_delivery_save_failed"
			m.state.Notification = nil
			m.cloudQueue = nil
		}
		m.signal()
	}
}
func (m *Manager) WorkerStateCurrent(state State) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return state.generation == m.generation && (state.Notification == nil || m.state.Preferences.ResetAlertsEnabled)
}
func (m *Manager) RecordNotificationResult(id, status, reason string) error {
	if status != "submitted" && status != "failed" && status != "unknown" || len(reason) > 160 || strings.ContainsAny(reason, "\r\n<>") {
		return errors.New("invalid_notification_result")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.inbox == nil {
		return errors.New("inbox_unavailable")
	}
	if record, ok := m.inbox.FindNotification(id); ok {
		if record.Delivery.QueuedAt == nil {
			return errors.New("notification_not_queued")
		}
		err := m.inbox.Delivery(record.PostID, status, reason, time.Now())
		if err == nil {
			m.signal()
		}
		return err
	}
	if _, ok := m.inbox.FindEventDelivery(id); ok {
		err := m.inbox.EventDelivery(id, status, reason, time.Now())
		if err == nil {
			m.signal()
		}
		return err
	}
	return errors.New("notification_not_found")
}
