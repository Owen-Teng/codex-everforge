// Package messageinbox retains public source records independently of alerts.
package messageinbox

import (
	"codex-quota-monitor/internal/reset"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const FileName = "message-inbox.json"
const classificationVersion = 2

type disk struct {
	ClassificationVersion int                       `json:"classificationVersion,omitempty"`
	Evidence              map[string]reset.Evidence `json:"evidence,omitempty"`
	EventDeliveries       map[string]reset.Delivery `json:"eventDeliveries,omitempty"`
	Sources               map[string]SourceState    `json:"sources,omitempty"`
	Version               int                       `json:"version"`
	Revision              uint64                    `json:"revision"`
	Messages              map[string]Message        `json:"messages"`
	Initialized           map[string]bool           `json:"initialized"`
	Source                SourceState               `json:"source"`
}
type Store struct {
	mu   sync.Mutex
	path string
	data disk
}

func Open(root string) (*Store, error) {
	s := &Store{path: filepath.Join(root, FileName), data: disk{Version: 1, Messages: map[string]Message{}, Initialized: map[string]bool{}}}
	raw, err := os.ReadFile(s.path)
	if err == nil {
		if len(raw) > 16<<20 || json.Unmarshal(raw, &s.data) != nil || s.data.Version != 1 || s.data.Messages == nil || s.data.Initialized == nil {
			return nil, errors.New("inbox_state_invalid")
		}
	} else if !os.IsNotExist(err) {
		return nil, errors.New("inbox_state_unavailable")
	}
	if s.data.Evidence == nil {
		s.data.Evidence = map[string]reset.Evidence{}
	}
	if s.data.EventDeliveries == nil {
		s.data.EventDeliveries = map[string]reset.Delivery{}
	}
	if s.data.Sources == nil {
		s.data.Sources = map[string]SourceState{}
	}
	changed := s.prune(time.Now().Unix())
	// Upgrade raw retained rows, including previously related messages. Receipts,
	// source initialization, publication and receive times remain authoritative.
	needsClassification := s.data.ClassificationVersion != classificationVersion
	for _, m := range s.data.Messages {
		needsClassification = needsClassification || m.Category == "pending"
	}
	if needsClassification {
		s.reclassify()
		if len(s.data.Messages) > 0 {
			s.data.Revision++
			changed = true
		}
	}
	if changed {
		if err := SaveJSON(s.path, s.data); err != nil {
			return nil, errors.New("inbox_migration_save_failed")
		}
	}
	return s, nil
}
func scrub(m Message) Message {
	m.OriginalText = ""
	m.Translation = nil
	m.References = nil
	m.TextCompleteness = "removed"
	return m
}

// This is a bounded private recent cache, not an indefinite full-text archive.
// Retraction/removal clears downstream text promptly; tiny dedupe tombstones
// are retained for at most 90 days, and stale cached text for at most 7 days.
func (s *Store) prune(now int64) bool {
	changed := false
	for id, e := range s.data.Evidence {
		if now-e.CollectedAt > 7*86400 {
			delete(s.data.Evidence, id)
			changed = true
		}
	}
	for id, d := range s.data.EventDeliveries {
		stamp := d.QueuedAt
		if d.ResultAt != nil {
			stamp = d.ResultAt
		} else if d.EmittedAt != nil {
			stamp = d.EmittedAt
		}
		if stamp != nil && now-*stamp > 90*86400 {
			delete(s.data.EventDeliveries, id)
			changed = true
		}
	}
	for id, m := range s.data.Messages {
		stateChanged := false
		if now-m.LastSeenAt > 90*86400 {
			delete(s.data.Messages, id)
			changed = true
			continue
		}
		if m.Status == "withdrawn" || m.Status == "not_in_latest_snapshot" || now-m.LastSeenAt > 7*86400 {
			if now-m.LastSeenAt > 7*86400 && m.Status == "active" {
				m.Status = "expired"
				stateChanged = true
			}
			if m.OriginalText != "" || m.Translation != nil || len(m.References) > 0 {
				m = scrub(m)
				s.data.Messages[id] = m
				changed = true
			}
			if stateChanged {
				s.data.Messages[id] = m
				changed = true
			}
		}
	}
	return changed
}
func SaveJSON(path string, value any) error {
	if os.MkdirAll(filepath.Dir(path), 0700) != nil {
		return errors.New("state_save_failed")
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return errors.New("state_save_failed")
	}
	f, err := os.CreateTemp(filepath.Dir(path), "inbox-*.tmp")
	if err != nil {
		return errors.New("state_save_failed")
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(raw)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(f.Name(), path)
	}
	if err != nil {
		return errors.New("state_save_failed")
	}
	return nil
}

var numeric = regexp.MustCompile(`^[1-9][0-9]{0,24}$`)
var authorPattern = regexp.MustCompile(`^[A-Za-z0-9_]{1,30}$`)

func PostID(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Host != "x.com" && u.Host != "twitter.com") {
		return ""
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) == 3 && parts[1] == "status" && authorPattern.MatchString(parts[0]) && numeric.MatchString(parts[2]) {
		return parts[2]
	}
	if len(parts) == 4 && parts[0] == "i" && parts[1] == "web" && parts[2] == "status" && numeric.MatchString(parts[3]) {
		return parts[3]
	}
	return ""
}
func NotificationID(postID string) string {
	sum := sha256.Sum256([]byte("x-post:" + postID))
	return hex.EncodeToString(sum[:])
}
func Valid(m Message) bool {
	return numeric.MatchString(m.PostID) && PostID(m.URL) == m.PostID && authorPattern.MatchString(m.Author) && utf8.ValidString(m.OriginalText) && len(m.OriginalText) <= 16384 && (m.PublishedAt == nil || *m.PublishedAt > 0)
}
func contains(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}
func (s *Store) Initialized(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.data.Initialized[id]
}
func (s *Store) MessagesFor(posts []Message) []Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Message, 0, len(posts))
	for _, p := range posts {
		if record, ok := s.data.Messages[p.PostID]; ok {
			out = append(out, record)
		}
	}
	return out
}
func (s *Store) Message(postID string) (Message, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, ok := s.data.Messages[postID]
	return m, ok
}
func (s *Store) FindNotification(id string) (Message, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, m := range s.data.Messages {
		if NotificationID(m.PostID) == id {
			return m, true
		}
	}
	return Message{}, false
}
func (s *Store) SeenNotification(postID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, ok := s.data.Messages[postID]
	return ok && m.Delivery.State != "" && m.Delivery.State != "pending"
}
func (s *Store) reclassify() {
	posts := s.resetPostsLocked()
	for id, m := range s.data.Messages {
		m.Category = reset.Category(posts[id], posts)
		s.data.Messages[id] = m
	}
	s.data.ClassificationVersion = classificationVersion
}

// Ingest durably saves ALL raw records before classification. The result is a
// view for alert policy; message retention does not depend on classifier output.
func (s *Store) Ingest(posts []Message, source SourceState, historyFrom *int64, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(posts) > 2000 {
		return errors.New("inbox_batch_limit")
	}
	present := map[string]bool{}
	before, _ := json.Marshal(s.data)
	restore := func() {
		var previous disk
		_ = json.Unmarshal(before, &previous)
		s.data = previous
		s.ensureEventMaps()
	}
	observed := now.Unix()
	if source.LastSuccessAt != nil && *source.LastSuccessAt > 0 && *source.LastSuccessAt <= now.Unix()+300 {
		observed = *source.LastSuccessAt
	}
	for _, incoming := range posts {
		if !Valid(incoming) {
			restore()
			return errors.New("inbox_post_invalid")
		}
		present[incoming.PostID] = true
		incoming.NotificationID = NotificationID(incoming.PostID)
		old, exists := s.data.Messages[incoming.PostID]
		if exists {
			incoming.FirstReceivedAt = old.FirstReceivedAt
			incoming.Delivery = old.Delivery
			incoming.Sources = append([]string(nil), old.Sources...)
			if old.Status == "withdrawn" {
				incoming.Status = "withdrawn"
			}
			if old.TextCompleteness == "full" && incoming.TextCompleteness != "full" {
				incoming.OriginalText = old.OriginalText
				incoming.Translation = old.Translation
				incoming.TextCompleteness = old.TextCompleteness
				incoming.References = old.References
			}
			if incoming.PublishedAt == nil {
				incoming.PublishedAt = old.PublishedAt
			}
		} else {
			incoming.FirstReceivedAt = observed
		}
		if !contains(incoming.Sources, source.ID) {
			incoming.Sources = append(incoming.Sources, source.ID)
		}
		incoming.LastSeenAt = observed
		incoming.Category = "pending"
		if incoming.Status == "" {
			incoming.Status = "active"
		}
		s.data.Messages[incoming.PostID] = incoming
	}
	if historyFrom != nil {
		for id, m := range s.data.Messages {
			if contains(m.Sources, source.ID) && m.PublishedAt != nil && *m.PublishedAt >= *historyFrom && !present[id] && m.Status == "active" {
				m.Status = "not_in_latest_snapshot"
				s.data.Messages[id] = m
			}
		}
	}
	s.data.Source = source
	s.data.Sources[source.ID] = source
	s.prune(now.Unix())
	s.data.Revision++
	if len(s.data.Messages) > 4000 {
		restore()
		return errors.New("inbox_capacity")
	}
	encoded, _ := json.Marshal(s.data)
	if len(encoded) > 16<<20 {
		restore()
		return errors.New("inbox_capacity")
	}
	if err := SaveJSON(s.path, s.data); err != nil {
		restore()
		return errors.New("inbox_raw_save_failed")
	}
	s.reclassify()
	if err := SaveJSON(s.path, s.data); err != nil {
		return errors.New("inbox_classification_save_failed")
	}
	return nil
}
func (s *Store) MarkInitialized(source string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	old := s.data.Initialized[source]
	s.data.Initialized[source] = true
	if err := SaveJSON(s.path, s.data); err != nil {
		s.data.Initialized[source] = old
		return err
	}
	return nil
}
func (s *Store) Delivery(postID, state, reason string, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, ok := s.data.Messages[postID]
	if !ok {
		return errors.New("inbox_post_not_found")
	}
	old := m
	stamp := now.Unix()
	m.Delivery.State = state
	m.Delivery.Reason = reason
	switch state {
	case "queued":
		m.Delivery.QueuedAt = &stamp
	case "worker_emitted":
		m.Delivery.EmittedAt = &stamp
	case "submitted", "failed", "unknown":
		m.Delivery.ResultAt = &stamp
	}
	s.data.Messages[postID] = m
	s.data.Revision++
	if err := SaveJSON(s.path, s.data); err != nil {
		s.data.Messages[postID] = old
		s.data.Revision--
		return err
	}
	return nil
}
func (s *Store) UpdateSource(source SourceState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.Source = source
	return SaveJSON(s.path, s.data)
}
func (s *Store) Snapshot() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.prune(time.Now().Unix()) {
		_ = SaveJSON(s.path, s.data)
	}
	messages := make([]Message, 0, len(s.data.Messages))
	for _, m := range s.data.Messages {
		preview := PreviewFor(m)
		m.Preview = &preview
		messages = append(messages, m)
	}
	sort.Slice(messages, func(i, j int) bool {
		if (messages[i].PublishedAt == nil) != (messages[j].PublishedAt == nil) {
			return messages[i].PublishedAt != nil
		}
		a, b := messages[i].FirstReceivedAt, messages[j].FirstReceivedAt
		if messages[i].PublishedAt != nil {
			a = *messages[i].PublishedAt
		}
		if messages[j].PublishedAt != nil {
			b = *messages[j].PublishedAt
		}
		if a == b {
			return messages[i].PostID > messages[j].PostID
		}
		return a > b
	})
	if len(messages) > 200 {
		messages = messages[:200]
	}
	result := Snapshot{SchemaVersion: 1, Revision: s.data.Revision, Source: s.data.Source, Messages: messages}
	result.ResetEvents = s.resetEventsLocked()
	for _, source := range s.data.Sources {
		result.Sources = append(result.Sources, source)
	}
	sort.Slice(result.Sources, func(i, j int) bool { return result.Sources[i].ID < result.Sources[j].ID })
	for i := range result.Messages {
		m := &result.Messages[i]
		m.DisplayText = DisplayFor(*m)
		for _, event := range result.ResetEvents {
			for _, id := range event.PostIDs {
				if id == m.PostID {
					m.ResetEventIDs = append(m.ResetEventIDs, event.ID)
				}
			}
		}
	}
	if result.Source.State == "" {
		result.Source.State = "disabled"
		result.Source.Reason = "public_source_not_enabled"
	}
	raw, _ := json.Marshal(result)
	for len(raw) > 160<<10 && len(result.Messages) > 0 {
		result.Messages = result.Messages[:len(result.Messages)-1]
		raw, _ = json.Marshal(result)
	}
	for len(raw) > 160<<10 && len(result.ResetEvents) > 0 {
		result.ResetEvents = result.ResetEvents[:len(result.ResetEvents)-1]
		raw, _ = json.Marshal(result)
	}
	_ = json.Unmarshal(raw, &result)
	return result
}
