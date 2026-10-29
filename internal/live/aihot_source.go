package live

import (
	"codex-quota-monitor/internal/messageinbox"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const AIHOTURL = "https://aihot.news/api/v1/codex-resets/recent"
const AIHOTSourceID = "aihot.news/codex-resets/v1"
const AIHOTConfigFile = "internal-public-source.json"
const AIHOTCacheFile = "aihot-poll-state.json"
const AIHOTMinInterval = 600 * time.Second

type InternalPublicConfig struct {
	SchemaVersion       int    `json:"schemaVersion"`
	Enabled             bool   `json:"enabled"`
	Source              string `json:"source"`
	InternalUseAccepted bool   `json:"internalUseAccepted"`
}
type aihotCache struct {
	ScheduleReason  string          `json:"scheduleReason,omitempty"`
	FailureKind     string          `json:"failureKind,omitempty"`
	RetryAfterUntil int64           `json:"retryAfterUntil,omitempty"`
	SafetyFailures  int             `json:"safetyFailures"`
	SafetyBlocked   bool            `json:"safetyBlocked"`
	LastRequestID   string          `json:"lastRequestId,omitempty"`
	Version         int             `json:"version"`
	ETag            string          `json:"etag"`
	Raw             json.RawMessage `json:"raw"`
	NextAllowedAt   int64           `json:"nextAllowedAt"`
	LastAttemptAt   *int64          `json:"lastAttemptAt"`
	LastSuccessAt   *int64          `json:"lastSuccessAt"`
	Attempts        uint64          `json:"attempts"`
	Failures        int             `json:"failures"`
	Origin          string          `json:"origin"`
}
type AIHOTSource struct {
	proxyState *publicProxyState
	mu         sync.Mutex
	root       string
	cache      aihotCache
	client     *http.Client
	now        func() time.Time
	endpoint   string
}

func LoadInternalPublicSource(root string) (*AIHOTSource, error) {
	raw, err := os.ReadFile(filepath.Join(root, AIHOTConfigFile))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil || len(raw) > 2048 {
		return nil, errors.New("public_source_config_invalid")
	}
	var c InternalPublicConfig
	if strictJSON(raw, &c) != nil || c.SchemaVersion != 1 {
		return nil, errors.New("public_source_config_invalid")
	}
	if !c.Enabled {
		return nil, nil
	}
	if c.Source != "aihot_internal" || !c.InternalUseAccepted {
		return nil, errors.New("internal_use_not_accepted")
	}
	return newAIHOTSource(root)
}
func newAIHOTSource(root string) (*AIHOTSource, error) {
	client, proxyState := newPublicHTTPClient(12 * time.Second)
	s := &AIHOTSource{root: root, cache: aihotCache{Version: 1}, now: time.Now, endpoint: AIHOTURL, client: client, proxyState: proxyState}
	raw, err := os.ReadFile(filepath.Join(root, AIHOTCacheFile))
	if err == nil {
		if len(raw) > 2<<20 || json.Unmarshal(raw, &s.cache) != nil || s.cache.Version != 1 || len(s.cache.ETag) > 512 || strings.ContainsAny(s.cache.ETag, "\r\n") {
			return nil, errors.New("public_source_cache_invalid")
		}
	} else if !os.IsNotExist(err) {
		return nil, errors.New("public_source_cache_unavailable")
	}
	return s, nil
}
func (s *AIHOTSource) SourceDescriptor() (string, string) {
	return "public_aggregator", "AIHOT 公开聚合（本机内部评估）"
}
func (s *AIHOTSource) PersistentSourceID() string  { return AIHOTSourceID }
func (s *AIHOTSource) PollInterval() time.Duration { return AIHOTMinInterval }
func (s *AIHOTSource) FetchTimeout() time.Duration { return 12 * time.Second }
func (s *AIHOTSource) NextPollTime() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return time.Unix(s.cache.NextAllowedAt, 0)
}
func (s *AIHOTSource) save() error {
	return messageinbox.SaveJSON(filepath.Join(s.root, AIHOTCacheFile), s.cache)
}
func (s *AIHOTSource) HealthSnapshot() messageinbox.SourceState {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, err := s.cachedBatch(s.cache.Origin, s.now())
	if err != nil {
		next := s.cache.NextAllowedAt
		reason := err.Error()
		if s.cache.Failures > 0 {
			reason = publicFailureReason("public_source", s.cache.FailureKind, s.cache.RetryAfterUntil, s.now().Unix(), reason)
		}
		return messageinbox.SourceState{ID: AIHOTSourceID, Label: "AIHOT 公开聚合（本机内部评估）", State: "offline", LastAttemptAt: s.cache.LastAttemptAt, LastSuccessAt: s.cache.LastSuccessAt, NextPollAt: &next, Reason: reason, ProxyMode: s.proxyState.Mode()}
	}
	return *b.Sync
}
func (s *AIHOTSource) Fetch(ctx context.Context) (SourceBatch, error) {
	if err := ctx.Err(); err != nil {
		return SourceBatch{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if os.MkdirAll(s.root, 0700) != nil {
		return SourceBatch{}, errors.New("public_source_state_unavailable")
	}
	release, err := lockPublicPoll(filepath.Join(s.root, "aihot-poll.lock"))
	if err != nil {
		return SourceBatch{}, errors.New("public_source_poll_in_use")
	}
	defer release()
	latest, err := newAIHOTSource(s.root)
	if err != nil {
		return SourceBatch{}, err
	}
	s.cache = latest.cache
	if s.cache.SafetyBlocked {
		return SourceBatch{}, errors.New("public_source_safety_review_required")
	}
	now := s.now()
	stamp := now.Unix()
	if stamp < s.cache.NextAllowedAt {
		return s.cachedBatch("cached_"+s.cache.Origin, now)
	}
	// Persist the next allowed time before the network, including failed requests.
	s.cache.NextAllowedAt = stamp + 600
	s.cache.ScheduleReason = "in_flight"
	s.cache.LastAttemptAt = &stamp
	s.cache.Attempts++
	if err := s.save(); err != nil {
		return SourceBatch{}, errors.New("public_source_reservation_failed")
	}
	req, err := http.NewRequestWithContext(ctx, "GET", s.endpoint, nil)
	if err != nil {
		return SourceBatch{}, errors.New("public_source_request")
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "CodexEverforge-InternalEvaluation/0.1")
	if s.cache.ETag != "" {
		req.Header.Set("If-None-Match", s.cache.ETag)
	}
	res, err := s.client.Do(req)
	if err != nil {
		s.failure(0, s.now(), networkFailureKind(err))
		return SourceBatch{}, errors.New("public_source_network")
	}
	defer res.Body.Close()
	if res.StatusCode == 304 {
		if len(s.cache.Raw) == 0 {
			s.failure(0, now, "schema")
			return SourceBatch{}, errors.New("public_source_304_without_cache")
		}
		successAt := s.now().Unix()
		s.cache.LastSuccessAt = &successAt
		s.cache.Failures = 0
		s.cache.FailureKind, s.cache.RetryAfterUntil = "", 0
		s.cache.Origin = "automatic_public_poll_304"
		s.cache.ScheduleReason = "poll"
		if err = s.save(); err != nil {
			return SourceBatch{}, err
		}
		return s.cachedBatch(s.cache.Origin, now)
	}
	if res.StatusCode != 200 {
		if res.StatusCode == 566 || res.StatusCode == 567 {
			s.cache.SafetyFailures++
			if s.cache.SafetyFailures >= 2 {
				s.cache.SafetyBlocked = true
			}
			id := res.Header.Get("X-Request-Id")
			if len(id) <= 128 && !strings.ContainsAny(id, "\r\n<>") {
				s.cache.LastRequestID = id
			}
		}
		retry := parseRetryAfter(res.Header.Get("Retry-After"), s.now())
		s.failure(retry, s.now(), httpFailureKind(res.StatusCode))
		return SourceBatch{}, errors.New("public_source_http_" + strconv.Itoa(res.StatusCode))
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, 1<<20+1))
	if err != nil || len(raw) > 1<<20 {
		kind := "body"
		if err != nil {
			kind = networkFailureKind(err)
		}
		s.failure(0, s.now(), kind)
		return SourceBatch{}, errors.New("public_source_body")
	}
	if _, err = ParseAIHOT(raw, stamp); err != nil {
		s.failure(0, s.now(), "schema")
		return SourceBatch{}, err
	}
	s.cache.Raw = raw
	successAt := s.now().Unix()
	s.cache.LastSuccessAt = &successAt
	s.cache.Failures = 0
	s.cache.FailureKind, s.cache.RetryAfterUntil = "", 0
	s.cache.SafetyFailures = 0
	s.cache.Origin = "automatic_public_poll"
	s.cache.ScheduleReason = "poll"
	tag := res.Header.Get("ETag")
	if len(tag) <= 512 && !strings.ContainsAny(tag, "\r\n") {
		s.cache.ETag = tag
	} else {
		s.cache.ETag = ""
	}
	if err = s.save(); err != nil {
		return SourceBatch{}, errors.New("public_source_cache_save_failed")
	}
	return s.cachedBatch(s.cache.Origin, now)
}
func parseRetryAfter(value string, now time.Time) int64 {
	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil && seconds > 0 {
		return min(seconds, 7*86400)
	}
	if date, err := http.ParseTime(value); err == nil && date.After(now) {
		return min(int64(date.Sub(now)/time.Second), 7*86400)
	}
	return 0
}
func (s *AIHOTSource) failure(retry int64, now time.Time, kind string) {
	s.cache.Failures++
	s.cache.FailureKind = kind
	s.cache.ScheduleReason = "retry"
	if retry > 0 {
		s.cache.RetryAfterUntil = max(s.cache.RetryAfterUntil, now.Unix()+retry)
	}
	delay := publicFailureDelay(600, s.cache.Failures, kind)
	s.cache.NextAllowedAt = max(s.cache.NextAllowedAt, now.Unix()+delay, s.cache.RetryAfterUntil)
	_ = s.save()
}
func (s *AIHOTSource) cachedBatch(origin string, now time.Time) (SourceBatch, error) {
	if len(s.cache.Raw) == 0 {
		return SourceBatch{}, errors.New("public_source_waiting_for_poll")
	}
	collected := int64(0)
	if s.cache.LastSuccessAt != nil {
		collected = *s.cache.LastSuccessAt
	}
	batch, err := ParseAIHOT(s.cache.Raw, collected)
	if err != nil {
		return SourceBatch{}, err
	}
	batch.Sync.LastAttemptAt = s.cache.LastAttemptAt
	batch.Sync.ProxyMode = s.proxyState.Mode()
	batch.Sync.LastSuccessAt = s.cache.LastSuccessAt
	batch.Sync.Origin = origin
	batch.CheckedAt = s.cache.LastAttemptAt
	next := s.cache.NextAllowedAt
	batch.Sync.NextPollAt = &next
	batch.PublicFeedAvailable = s.cache.LastSuccessAt != nil && now.Unix()-*s.cache.LastSuccessAt <= 1800 && *s.cache.LastSuccessAt <= now.Unix()+30
	if !batch.PublicFeedAvailable {
		batch.Sync.State = "stale"
		batch.Sync.Reason = "public_source_cache_stale"
	}
	if s.cache.Failures > 0 {
		batch.PublicFeedAvailable = false
		batch.Sync.State = "offline"
		batch.Sync.Reason = publicFailureReason("public_source", s.cache.FailureKind, s.cache.RetryAfterUntil, now.Unix(), "public_source_fetch_failed")
	}
	batch.UnavailableReason = batch.Sync.Reason
	return batch, nil
}

type aihotContext struct {
	ID       string  `json:"id"`
	Author   string  `json:"author"`
	Relation string  `json:"relation"`
	Text     *string `json:"text"`
	Original string  `json:"originalText"`
	URL      string  `json:"url"`
}
type aihotPost struct {
	ID           string         `json:"id"`
	PublishedAt  string         `json:"publishedAt"`
	Text         *string        `json:"text"`
	Original     string         `json:"originalText"`
	FullText     *string        `json:"fullText"`
	FullOriginal *string        `json:"fullOriginalText"`
	Context      []aihotContext `json:"context"`
	URL          string         `json:"url"`
	Action       *string        `json:"action"`
}

func nullableTime(value *string) (*int64, error) {
	if value == nil {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339Nano, *value)
	if err != nil {
		return nil, errors.New("public_source_time_invalid")
	}
	stamp := t.Unix()
	return &stamp, nil
}
func ParseAIHOT(raw []byte, collectedAt int64) (SourceBatch, error) {
	var d struct {
		SchemaVersion int     `json:"schemaVersion"`
		Timezone      string  `json:"timezone"`
		CheckedAt     *string `json:"checkedAt"`
		HistoryFrom   string  `json:"historyFrom"`
		Count         int     `json:"count"`
		Events        []struct {
			ConfirmationBasis string      `json:"confirmationBasis"`
			Posts             []aihotPost `json:"posts"`
			Presentation      *struct {
				Status string `json:"status"`
			} `json:"presentation"`
		} `json:"events"`
		Activities []aihotPost `json:"activities"`
		Monitor    *struct {
			Status        string  `json:"status"`
			LastCollected *string `json:"lastCollectedAt"`
		} `json:"monitor"`
		Outage *struct {
			ID          string  `json:"postId"`
			PublishedAt string  `json:"publishedAt"`
			Text        *string `json:"text"`
			Original    string  `json:"originalText"`
			URL         string  `json:"url"`
		} `json:"outage"`
	}
	if len(raw) > 1<<20 || json.Unmarshal(raw, &d) != nil || d.SchemaVersion != 1 || d.Timezone != "Asia/Shanghai" || d.Events == nil || d.Count != len(d.Events) || d.Count > 200 || len(d.Activities) > 1000 {
		return SourceBatch{}, errors.New("public_source_schema")
	}
	checked, err := nullableTime(d.CheckedAt)
	if err != nil {
		return SourceBatch{}, err
	}
	history, err := nullableTime(&d.HistoryFrom)
	if err != nil {
		return SourceBatch{}, err
	}
	source := messageinbox.SourceState{ID: AIHOTSourceID, Label: "AIHOT 公开聚合（本机内部评估）", State: "available", UpstreamCheckedAt: checked, UpstreamStatus: "unknown"}
	if d.Monitor != nil {
		source.UpstreamStatus = d.Monitor.Status
		source.UpstreamCollectedAt, err = nullableTime(d.Monitor.LastCollected)
		if err != nil {
			return SourceBatch{}, err
		}
	}
	if checked == nil || source.UpstreamStatus != "healthy" || collectedAt-*checked > 1800 {
		source.Reason = "public_source_upstream_delayed"
	}
	posts := map[string]messageinbox.Message{}
	add := func(p aihotPost, aggregate string, full bool, basis string) error {
		stamp, err := nullableTime(&p.PublishedAt)
		if err != nil {
			return err
		}
		text, translation, completeness := p.Original, p.Text, "excerpt"
		if full {
			completeness = "full"
		}
		if p.FullOriginal != nil {
			text = *p.FullOriginal
			translation = p.FullText
			completeness = "full"
		}
		m := messageinbox.Message{PostID: p.ID, URL: p.URL, Author: "thsottiaux", PublishedAt: stamp, OriginalText: text, Translation: translation, TextCompleteness: completeness, Verification: "aggregated_source_quote", Status: "active", AggregateStatus: aggregate, References: []messageinbox.Reference{}}
		m.ConfirmationBasis = basis
		if p.Action != nil {
			m.AggregateAction = *p.Action
			if *p.Action == "withdraw" {
				m.Status = "withdrawn"
			}
		}
		for _, c := range p.Context {
			author := c.Author
			if author == "" {
				return errors.New("public_source_context_author")
			}
			u := "https://x.com/" + author + "/status/" + c.ID
			parent := messageinbox.Message{PostID: c.ID, URL: u, Author: author, OriginalText: c.Original, Translation: c.Text, TextCompleteness: "context", Verification: "aggregated_context_quote", Status: "active", References: []messageinbox.Reference{}}
			if !messageinbox.Valid(parent) || messageinbox.PostID(c.URL) != c.ID {
				return errors.New("public_source_context_invalid")
			}
			if _, exists := posts[c.ID]; !exists {
				posts[c.ID] = parent
			}
			m.References = append(m.References, messageinbox.Reference{PostID: c.ID, Author: author, Relation: c.Relation})
		}
		if !messageinbox.Valid(m) || m.URL != "https://x.com/thsottiaux/status/"+p.ID {
			return errors.New("public_source_post_invalid")
		}
		if old, exists := posts[m.PostID]; exists && aggregate == "" {
			m.AggregateStatus = old.AggregateStatus
			m.ConfirmationBasis = old.ConfirmationBasis
		}
		posts[m.PostID] = m
		return nil
	}
	for _, e := range d.Events {
		aggregate := ""
		if e.Presentation != nil {
			aggregate = e.Presentation.Status
		}
		for _, p := range e.Posts {
			if err = add(p, aggregate, false, e.ConfirmationBasis); err != nil {
				return SourceBatch{}, err
			}
		}
	}
	for _, p := range d.Activities {
		if err = add(p, "", true, ""); err != nil {
			return SourceBatch{}, err
		}
	}
	if d.Outage != nil {
		p := d.Outage
		if err = add(aihotPost{ID: p.ID, PublishedAt: p.PublishedAt, Text: p.Text, Original: p.Original, URL: p.URL}, "", true, ""); err != nil {
			return SourceBatch{}, err
		}
	}
	b := SourceBatch{SourceID: AIHOTSourceID, SourceKind: "public_aggregator", SourceLabel: source.Label, UpstreamSuccessAt: checked, Sync: &source, HistoryFrom: history, Posts: []messageinbox.Message{}}
	for _, p := range posts {
		b.Posts = append(b.Posts, p)
	}
	return b, nil
}

// SeedAIHOT imports the captured public response silently, with its original
// collection metadata. This operation makes no request and enables no source.
func SeedAIHOT(root string, raw, metaRaw []byte) error {
	if os.MkdirAll(root, 0700) != nil {
		return errors.New("seed_directory_unavailable")
	}
	release, err := lockPublicPoll(filepath.Join(root, "aihot-poll.lock"))
	if err != nil {
		return errors.New("public_source_poll_in_use")
	}
	defer release()
	var meta struct {
		URL         string `json:"url"`
		Status      int    `json:"status"`
		CollectedAt string `json:"collectedAt"`
		Headers     struct {
			ETag string `json:"ETag"`
		} `json:"headers"`
	}
	if json.Unmarshal(metaRaw, &meta) != nil || meta.URL != AIHOTURL || meta.Status != 200 {
		return errors.New("seed_metadata_invalid")
	}
	at, err := time.Parse(time.RFC3339Nano, meta.CollectedAt)
	if err != nil {
		return errors.New("seed_metadata_invalid")
	}
	previous, err := newAIHOTSource(root)
	if err != nil {
		return err
	}
	if previous.cache.LastSuccessAt != nil && *previous.cache.LastSuccessAt > at.Unix() {
		return errors.New("seed_would_replace_newer_snapshot")
	}
	batch, err := ParseAIHOT(raw, at.Unix())
	if err != nil {
		return err
	}
	store, err := messageinbox.Open(root)
	if err != nil {
		return err
	}
	batch.Sync.Origin = "manual_public_import"
	batch.Sync.State = "cached"
	batch.Sync.Reason = "manual_public_import_not_active"
	stamp := at.Unix()
	batch.Sync.LastAttemptAt = &stamp
	batch.Sync.LastSuccessAt = &stamp
	next := stamp + 600
	batch.Sync.NextPollAt = &next
	if err = store.Ingest(batch.Posts, *batch.Sync, batch.HistoryFrom, at); err != nil {
		return err
	}
	for _, m := range store.Snapshot().Messages {
		if m.Delivery.State == "" {
			if err = store.Delivery(m.PostID, "baseline", "manual_public_import_silent", at); err != nil {
				return err
			}
		}
	}
	// No initialized marker: the first automatic snapshot is also a quiet baseline.
	c := aihotCache{Version: 1, ETag: meta.Headers.ETag, Raw: raw, NextAllowedAt: next, LastAttemptAt: &stamp, LastSuccessAt: &stamp, Origin: "manual_public_import"}
	c.NextAllowedAt = max(c.NextAllowedAt, previous.cache.NextAllowedAt)
	c.Attempts = previous.cache.Attempts
	c.Failures = previous.cache.Failures
	if previous.cache.LastAttemptAt != nil && *previous.cache.LastAttemptAt > stamp {
		c.LastAttemptAt = previous.cache.LastAttemptAt
	}
	return messageinbox.SaveJSON(filepath.Join(root, AIHOTCacheFile), c)
}
