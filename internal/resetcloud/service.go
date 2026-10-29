// Package resetcloud serves only normalized public author claims. Ingestion is
// an in-process operation; there is deliberately no HTTP publishing endpoint.
package resetcloud

import (
	"codex-quota-monitor/internal/resetcloud/protocol"
	"codex-quota-monitor/internal/xactivity"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

type Grant struct {
	ID                string `json:"id"`
	Deadline          int64  `json:"deadline"`
	ReservedAttempts  int    `json:"reservedAttempts"`
	ReservedFrames    int    `json:"reservedFrames"`
	ReservedWireBytes int64  `json:"reservedWireBytes"`
	UsedAttempts      int    `json:"usedAttempts"`
	UsedFrames        int    `json:"usedFrames"`
	UsedWireBytes     int64  `json:"usedWireBytes"`
}
type diskState struct {
	SchemaVersion int                       `json:"schemaVersion"`
	Synthetic     bool                      `json:"synthetic"`
	Revision      uint64                    `json:"revision"`
	Events        map[string]protocol.Event `json:"events"`
	Grants        map[string]Grant          `json:"grants"`
	Upstream      protocol.Upstream         `json:"upstream"`
}
type Service struct {
	mu       sync.Mutex
	path     string
	now      func() time.Time
	disk     diskState
	upstream protocol.Upstream
}

func Open(path string, synthetic bool, now func() time.Time) (*Service, error) {
	if now == nil {
		now = time.Now
	}
	s := &Service{path: path, now: now, disk: diskState{SchemaVersion: 1, Synthetic: synthetic, Events: map[string]protocol.Event{}, Grants: map[string]Grant{}}, upstream: protocol.Upstream{State: "disabled", Reason: "not_authorized"}}
	raw, err := os.ReadFile(path)
	if err == nil {
		if len(raw) > 2<<20 || json.Unmarshal(raw, &s.disk) != nil || s.disk.SchemaVersion != 1 || s.disk.Synthetic != synthetic || s.disk.Events == nil || s.disk.Grants == nil || len(s.disk.Events) > 2000 || len(s.disk.Grants) > 1000 {
			return nil, errors.New("cloud_state_invalid")
		}
		s.upstream = s.disk.Upstream
		s.upstream.State = "stopped"
		s.upstream.Reason = "service_restarted"
		s.upstream.Connected = false
	} else if !os.IsNotExist(err) {
		return nil, errors.New("cloud_state_unavailable")
	}
	return s, nil
}
func atomicSave(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), "reset-*.tmp")
	if err != nil {
		return err
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
	if err == nil {
		err = syncDirectory(filepath.Dir(path))
	}
	return err
}

// ReserveGrant burns the whole finite allowance BEFORE any credential/network
// access. A crash/restart may lose unused capacity, never recreate capacity.
// The same ID can never be resumed, even after changing its configured limits.
func (s *Service) ReserveGrant(id string, deadline int64, c xactivity.Config) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now().Unix()
	if len(id) < 8 || len(id) > 80 || deadline <= now || deadline > now+int64(c.MaxDurationSeconds) || len(s.disk.Grants) >= 1000 {
		return errors.New("authorization_invalid")
	}
	if _, ok := s.disk.Grants[id]; ok {
		return errors.New("authorization_already_consumed")
	}
	s.disk.Grants[id] = Grant{ID: id, Deadline: deadline, ReservedAttempts: c.MaxAttempts, ReservedFrames: c.MaxDeliveredFrames, ReservedWireBytes: c.MaxWireBytes}
	if err := atomicSave(s.path, s.disk); err != nil {
		delete(s.disk.Grants, id)
		return errors.New("authorization_save_failed")
	}
	return nil
}
func (s *Service) RecordStatus(grantID string, status xactivity.Status) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.upstream = protocol.Upstream{State: status.State, Reason: status.Reason, Connected: status.ConnectionActive, LastWireAt: status.LastWireAt, LastEventAt: s.upstream.LastEventAt}
	s.disk.Upstream = s.upstream
	if grantID == "" {
		return nil
	}
	grant, ok := s.disk.Grants[grantID]
	if !ok {
		return errors.New("authorization_missing")
	}
	grant.UsedAttempts = max(grant.UsedAttempts, status.Attempts)
	grant.UsedFrames = max(grant.UsedFrames, status.Frames)
	grant.UsedWireBytes = max(grant.UsedWireBytes, status.WireBytes)
	s.disk.Grants[grantID] = grant
	if err := atomicSave(s.path, s.disk); err != nil {
		s.upstream.Connected = false
		s.upstream.State = "stopped"
		s.upstream.Reason = "state_save_failed"
		return errors.New("state_save_failed")
	}
	return nil
}
func (s *Service) Stop(reason string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.upstream.State = "stopped"
	s.upstream.Connected = false
	s.upstream.Reason = reason
	s.disk.Upstream = s.upstream
	if atomicSave(s.path, s.disk) != nil {
		s.upstream.Reason = "state_save_failed"
	}
}
func (s *Service) Ingest(decision xactivity.Decision) error {
	if decision.SourcePost == nil || decision.Notification == nil || (decision.Reason != "notice" && decision.Reason != "announced") {
		return nil
	}
	p := decision.SourcePost
	stamp := p.CreatedAt.Unix()
	s.mu.Lock()
	defer s.mu.Unlock()
	if stamp <= 0 || stamp > s.now().Unix()+300 || stamp+protocol.EventTTL <= s.now().Unix() {
		return nil
	}
	sum := sha256.Sum256([]byte("thsottiaux:" + p.ID))
	id := hex.EncodeToString(sum[:])
	if _, ok := s.disk.Events[id]; ok {
		return nil
	}
	for key, previous := range s.disk.Events {
		if previous.ExpiresAt <= s.now().Unix() {
			delete(s.disk.Events, key)
		}
	}
	if len(s.disk.Events) >= 2000 {
		return errors.New("event_capacity")
	}
	summary := "作者预告 Codex 额度将重置，具体安排以原帖为准；本机恢复尚未确认。"
	if decision.Reason == "announced" {
		summary = "作者声称 Codex 额度已重置；本机恢复仍待独立额度核对。"
	}
	e := protocol.Event{ID: id, Version: 1, Kind: decision.Reason, PublishedAt: stamp, ExpiresAt: stamp + protocol.EventTTL, SourceURL: "https://x.com/thsottiaux/status/" + p.ID, Summary: summary, Verification: "author_claim", Status: "active"}
	s.disk.Events[id] = e
	s.disk.Revision++
	previousEventAt := s.upstream.LastEventAt
	observed := s.now().Unix()
	s.upstream.LastEventAt = &observed
	s.disk.Upstream = s.upstream
	if err := atomicSave(s.path, s.disk); err != nil {
		delete(s.disk.Events, id)
		s.disk.Revision--
		s.upstream.LastEventAt = previousEventAt
		s.disk.Upstream = s.upstream
		return errors.New("event_save_failed")
	}
	return nil
}

// Withdraw is available only to trusted in-process maintenance, never HTTP.
func (s *Service) Withdraw(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.disk.Events[id]
	if !ok || e.Status == "withdrawn" {
		return nil
	}
	old := e
	e.Version++
	e.Status = "withdrawn"
	s.disk.Events[id] = e
	s.disk.Revision++
	if err := atomicSave(s.path, s.disk); err != nil {
		s.disk.Events[id] = old
		s.disk.Revision--
		return errors.New("event_save_failed")
	}
	return nil
}
func (s *Service) Feed() protocol.Feed {
	s.mu.Lock()
	defer s.mu.Unlock()
	stamp := s.now().Unix()
	u := s.upstream
	if u.Connected && (u.LastWireAt == nil || *u.LastWireAt > stamp+5 || stamp-*u.LastWireAt > protocol.WireTTL) {
		u.Connected = false
		u.State = "stale"
		u.Reason = "upstream_heartbeat_stale"
	}
	events := []protocol.Event{}
	for _, e := range s.disk.Events {
		if e.ExpiresAt > stamp {
			events = append(events, e)
		}
	}
	sort.Slice(events, func(i, j int) bool {
		if events[i].PublishedAt == events[j].PublishedAt {
			return events[i].ID < events[j].ID
		}
		return events[i].PublishedAt < events[j].PublishedAt
	})
	return protocol.Feed{SchemaVersion: 1, SourceID: protocol.SourceID, Revision: s.disk.Revision, GeneratedAt: stamp, ExpiresAt: stamp + protocol.FeedTTL, Synthetic: s.disk.Synthetic, Upstream: u, Events: events}
}
func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")
	if r.URL.Path != "/v1/feed" && r.URL.Path != "/v1/health" {
		http.Error(w, `{"error":"not_found"}`, 404)
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, `{"error":"read_only"}`, 405)
		return
	}
	feed := s.Feed()
	if r.URL.Path == "/v1/health" {
		_ = json.NewEncoder(w).Encode(struct {
			SchemaVersion int               `json:"schemaVersion"`
			Service       string            `json:"service"`
			GeneratedAt   int64             `json:"generatedAt"`
			Synthetic     bool              `json:"synthetic"`
			Upstream      protocol.Upstream `json:"upstream"`
		}{1, "ready", feed.GeneratedAt, feed.Synthetic, feed.Upstream})
		return
	}
	_ = json.NewEncoder(w).Encode(feed)
}
