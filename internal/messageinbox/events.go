package messageinbox

import (
	"codex-quota-monitor/internal/reset"
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

func (s *Store) ensureEventMaps() {
	if s.data.Evidence == nil {
		s.data.Evidence = map[string]reset.Evidence{}
	}
	if s.data.EventDeliveries == nil {
		s.data.EventDeliveries = map[string]reset.Delivery{}
	}
	if s.data.Sources == nil {
		s.data.Sources = map[string]SourceState{}
	}
}

func (s *Store) resetPostsLocked() map[string]reset.Post {
	posts := map[string]reset.Post{}
	for id, m := range s.data.Messages {
		links := []reset.Link{}
		for _, ref := range m.References {
			links = append(links, reset.Link{PostID: ref.PostID, Author: ref.Author, Relation: ref.Relation})
		}
		posts[id] = reset.Post{ID: id, Author: m.Author, Text: m.OriginalText, Category: m.Category, Status: m.Status, Verification: m.Verification, AggregateStatus: m.AggregateStatus, AggregateAction: m.AggregateAction, ConfirmationBasis: m.ConfirmationBasis, TextCompleteness: m.TextCompleteness, PublishedAt: m.PublishedAt, CollectedAt: m.LastSeenAt, SourceIDs: m.Sources, Links: links}
	}
	return posts
}
func (s *Store) resetEventsLocked() []reset.Event {
	posts := s.resetPostsLocked()
	external := []reset.Evidence{}
	for _, e := range s.data.Evidence {
		external = append(external, e)
	}
	events := reset.Build(posts, external)
	for i := range events {
		events[i].Delivery = s.data.EventDeliveries[events[i].NotificationID]
	}
	return events
}
func (s *Store) ResetEvents() []reset.Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.resetEventsLocked()
}
func (s *Store) IngestEvidence(entries []reset.Evidence, source SourceState, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(entries) > 1000 {
		return errors.New("evidence_batch_limit")
	}
	for _, e := range entries {
		if e.ID == "" || len(e.ID) > 256 || e.SourceID != source.ID || e.SourceEventID == "" || len(e.SourceEventID) > 128 || e.CollectedAt <= 0 || e.Type == "external_observation" && e.PostID != "" {
			return errors.New("invalid_reset_evidence")
		}
	}
	oldEvidence := map[string]reset.Evidence{}
	for id, e := range s.data.Evidence {
		oldEvidence[id] = e
	}
	oldSource, hadSource := s.data.Sources[source.ID]
	oldRevision := s.data.Revision
	for _, e := range entries {
		s.data.Evidence[e.ID] = e
	}
	for id, e := range s.data.Evidence {
		if now.Unix()-e.CollectedAt > 7*86400 {
			delete(s.data.Evidence, id)
		}
	}
	s.data.Sources[source.ID] = source
	s.data.Revision++
	if err := SaveJSON(s.path, s.data); err != nil {
		s.data.Evidence = oldEvidence
		s.data.Revision = oldRevision
		if hadSource {
			s.data.Sources[source.ID] = oldSource
		} else {
			delete(s.data.Sources, source.ID)
		}
		return err
	}
	return nil
}
func (s *Store) EventDelivery(id, state, reason string, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	old := s.data.EventDeliveries[id]
	if state != "baseline" && state != "queued" && old.QueuedAt == nil {
		return errors.New("event_not_queued")
	}
	value := old
	value.State = state
	value.Reason = reason
	stamp := now.Unix()
	switch state {
	case "queued":
		value.QueuedAt = &stamp
	case "worker_emitted":
		value.EmittedAt = &stamp
	case "submitted", "failed", "unknown":
		value.ResultAt = &stamp
	}
	s.data.EventDeliveries[id] = value
	s.data.Revision++
	if err := SaveJSON(s.path, s.data); err != nil {
		if old.State == "" {
			delete(s.data.EventDeliveries, id)
		} else {
			s.data.EventDeliveries[id] = old
		}
		s.data.Revision--
		return err
	}
	return nil
}
func (s *Store) FindEventDelivery(id string) (reset.Delivery, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.data.EventDeliveries[id]
	return v, ok
}
func (s *Store) UpdateSourceHealth(source SourceState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	old, existed := s.data.Sources[source.ID]
	s.data.Sources[source.ID] = source
	if err := SaveJSON(s.path, s.data); err != nil {
		if existed {
			s.data.Sources[source.ID] = old
		} else {
			delete(s.data.Sources, source.ID)
		}
		return err
	}
	return nil
}
func DisplayFor(m Message) *DisplayText {
	if m.Status != "active" {
		return &DisplayText{Text: PreviewFor(m).Summary, Basis: "rule_summary", Source: "Codex Everforge"}
	}
	if m.TextCompleteness == "full" && m.Translation != nil {
		text := strings.TrimSpace(*m.Translation)
		safe := text != "" && len(text) <= 16384 && utf8.ValidString(text) && !strings.ContainsAny(text, "<>")
		for _, r := range text {
			if unicode.In(r, unicode.Cf) || unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' {
				safe = false
			}
		}
		if safe {
			source := "来源提供中文"
			for _, id := range m.Sources {
				if strings.HasPrefix(id, "aihot.news/") {
					source = "AIHOT"
				}
			}
			return &DisplayText{Text: text, Basis: "source_translation", Source: source}
		}
	}
	return &DisplayText{Text: PreviewFor(m).Summary, Basis: "rule_summary", Source: "Codex Everforge"}
}
