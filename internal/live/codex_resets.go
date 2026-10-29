package live

import (
	"codex-quota-monitor/internal/messageinbox"
	"codex-quota-monitor/internal/reset"
	"codex-quota-monitor/internal/resettext"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const CodexResetsURL = "https://codex-resets.com/api/v1/resets?limit=20"
const CodexResetsSourceID = "codex-resets.com/v1"
const MonitorConfigFile = "public-reset-monitor.json"

type monitorCache struct {
	FailureKind     string          `json:"failureKind,omitempty"`
	RetryAfterUntil int64           `json:"retryAfterUntil,omitempty"`
	ScheduleVersion int             `json:"scheduleVersion,omitempty"`
	ScheduleReason  string          `json:"scheduleReason,omitempty"`
	Endpoint        string          `json:"endpoint,omitempty"`
	Version         int             `json:"version"`
	Raw             json.RawMessage `json:"raw"`
	ETag            string          `json:"etag"`
	LastAttemptAt   *int64          `json:"lastAttemptAt"`
	LastSuccessAt   *int64          `json:"lastSuccessAt"`
	NextAllowedAt   int64           `json:"nextAllowedAt"`
	Failures        int             `json:"failures"`
}
type CodexResetsSource struct {
	proxyState     *publicProxyState
	mu             sync.Mutex
	root, endpoint string
	client         *http.Client
	now            func() time.Time
	cache          monitorCache
}

func loadMonitor(root string) (*CodexResetsSource, error) {
	endpoint := CodexResetsURL
	raw, err := os.ReadFile(filepath.Join(root, MonitorConfigFile))
	if err == nil {
		var cfg struct {
			SchemaVersion int    `json:"schemaVersion"`
			Enabled       *bool  `json:"enabled"`
			Endpoint      string `json:"endpoint,omitempty"`
		}
		if len(raw) > 2048 || strictJSON(raw, &cfg) != nil || cfg.SchemaVersion != 1 || cfg.Enabled == nil {
			return nil, errors.New("monitor_config_invalid")
		}
		if !*cfg.Enabled {
			return nil, nil
		}
		if cfg.Endpoint != "" {
			endpoint = cfg.Endpoint
		}
	} else if !os.IsNotExist(err) {
		return nil, errors.New("monitor_config_unavailable")
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" || (u.RawQuery != "" && u.RawQuery != "limit=20") {
		return nil, errors.New("monitor_https_endpoint_invalid")
	}
	client, proxyState := newPublicHTTPClient(12 * time.Second)
	s := &CodexResetsSource{root: root, endpoint: endpoint, client: client, proxyState: proxyState, now: time.Now, cache: monitorCache{Version: 1}}
	if err = s.reload(); err != nil {
		return nil, err
	}
	return s, nil
}
func (s *CodexResetsSource) reload() error {
	raw, err := os.ReadFile(filepath.Join(s.root, "codex-resets-poll.json"))
	if os.IsNotExist(err) {
		return nil
	}
	var cached monitorCache
	if err != nil || len(raw) > 2<<20 || json.Unmarshal(raw, &cached) != nil || cached.Version != 1 || len(cached.ETag) > 512 || strings.ContainsAny(cached.ETag, "\r\n") {
		return errors.New("monitor_cache_invalid")
	}
	s.cache = cached
	if s.cache.Failures < 0 || s.cache.NextAllowedAt < 0 {
		return errors.New("monitor_cache_invalid")
	}
	if s.cache.Endpoint != "" && s.cache.Endpoint != s.endpoint {
		s.cache.Raw = nil
		s.cache.ETag = ""
		s.cache.LastSuccessAt = nil
	}
	return nil
}

// Only the successful legacy four-hour cache reservation has enough evidence
// to shorten safely. In-flight, unknown and failed/retry reservations survive.
// Called under both the source mutex and persistent poll lock, never at load.
func (s *CodexResetsSource) migrateSuccessfulReservation(now time.Time) error {
	c := &s.cache
	if c.ScheduleVersion != 0 || c.ScheduleReason != "" || c.Failures != 0 || c.LastAttemptAt == nil || c.LastSuccessAt == nil || c.Endpoint != s.endpoint {
		return nil
	}
	attempt, success := *c.LastAttemptAt, *c.LastSuccessAt
	if attempt <= 0 || attempt > now.Unix() || success < attempt || success > attempt+30 || success > now.Unix()+30 || c.NextAllowedAt-attempt != 14400 {
		return nil
	}
	if _, err := ParseCodexResets(c.Raw, success); err != nil {
		return nil
	}
	c.NextAllowedAt = max(attempt+300, now.Unix())
	c.ScheduleVersion, c.ScheduleReason = 1, "poll"
	return s.save()
}
func (s *CodexResetsSource) save() error {
	return messageinbox.SaveJSON(filepath.Join(s.root, "codex-resets-poll.json"), s.cache)
}
func (s *CodexResetsSource) SourceDescriptor() (string, string) {
	return "public_aggregator", "公开重置监控"
}
func (s *CodexResetsSource) PollInterval() time.Duration { return 300 * time.Second }
func (s *CodexResetsSource) FetchTimeout() time.Duration { return 12 * time.Second }
func (s *CodexResetsSource) NextPollTime() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return time.Unix(s.cache.NextAllowedAt, 0)
}
func (s *CodexResetsSource) Fetch(ctx context.Context) (SourceBatch, error) {
	if err := ctx.Err(); err != nil {
		return SourceBatch{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if os.MkdirAll(s.root, 0700) != nil {
		return SourceBatch{}, errors.New("monitor_state_unavailable")
	}
	release, err := lockPublicPoll(filepath.Join(s.root, "codex-resets-poll.lock"))
	if err != nil {
		return SourceBatch{}, errors.New("monitor_poll_in_use")
	}
	defer release()
	if err = s.reload(); err != nil {
		return SourceBatch{}, err
	}
	now := s.now()
	if err = s.migrateSuccessfulReservation(now); err != nil {
		return SourceBatch{}, errors.New("monitor_migration_save_failed")
	}
	if now.Unix() < s.cache.NextAllowedAt {
		return s.cached("cached_public_monitor")
	}
	stamp := now.Unix()
	s.cache.LastAttemptAt = &stamp
	s.cache.Endpoint = s.endpoint
	s.cache.NextAllowedAt = stamp + 300
	s.cache.ScheduleVersion, s.cache.ScheduleReason = 1, "in_flight"
	if err = s.save(); err != nil {
		return SourceBatch{}, errors.New("monitor_reservation_failed")
	}
	req, _ := http.NewRequestWithContext(ctx, "GET", s.endpoint, nil)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "CodexEverforge/0.1 (public reset monitor; https://codex-resets.com)")
	if s.cache.ETag != "" {
		req.Header.Set("If-None-Match", s.cache.ETag)
	}
	response, err := s.client.Do(req)
	if err != nil {
		s.fail(0, networkFailureKind(err))
		return SourceBatch{}, errors.New("monitor_network")
	}
	defer response.Body.Close()
	if response.StatusCode == 304 {
		if len(s.cache.Raw) == 0 {
			s.fail(0, "schema")
			return SourceBatch{}, errors.New("monitor_304_without_cache")
		}
		s.cache.Failures = 0
		s.cache.FailureKind, s.cache.RetryAfterUntil = "", 0
		done := s.now().Unix()
		s.cache.LastSuccessAt = &done
		s.cache.ScheduleReason = "poll"
		if err = s.save(); err != nil {
			return SourceBatch{}, err
		}
		return s.cached("public_monitor_304")
	}
	if response.StatusCode != 200 {
		s.fail(parseRetryAfter(response.Header.Get("Retry-After"), s.now()), httpFailureKind(response.StatusCode))
		return SourceBatch{}, errors.New("monitor_http_" + strconv.Itoa(response.StatusCode))
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 1<<20+1))
	if err != nil || len(raw) > 1<<20 {
		kind := "body"
		if err != nil {
			kind = networkFailureKind(err)
		}
		s.fail(0, kind)
		return SourceBatch{}, errors.New("monitor_body")
	}
	if _, err = ParseCodexResets(raw, s.now().Unix()); err != nil {
		s.fail(0, "schema")
		return SourceBatch{}, err
	}
	s.cache.Raw = raw
	s.cache.Endpoint = s.endpoint
	s.cache.Failures = 0
	s.cache.FailureKind, s.cache.RetryAfterUntil = "", 0
	done := s.now().Unix()
	s.cache.LastSuccessAt = &done
	s.cache.ScheduleReason = "poll"
	tag := response.Header.Get("ETag")
	if len(tag) <= 512 && !strings.ContainsAny(tag, "\r\n") {
		s.cache.ETag = tag
	}
	if err = s.save(); err != nil {
		return SourceBatch{}, err
	}
	return s.cached("public_monitor_poll")
}
func (s *CodexResetsSource) fail(retry int64, kind string) {
	s.cache.Failures++
	s.cache.ScheduleVersion, s.cache.ScheduleReason = 1, "retry"
	s.cache.FailureKind = kind
	now := s.now().Unix()
	if retry > 0 {
		s.cache.RetryAfterUntil = max(s.cache.RetryAfterUntil, now+retry)
	}
	s.cache.NextAllowedAt = max(s.cache.NextAllowedAt, now+publicFailureDelay(300, s.cache.Failures, kind), s.cache.RetryAfterUntil)
	_ = s.save()
}
func (s *CodexResetsSource) cached(origin string) (SourceBatch, error) {
	if len(s.cache.Raw) == 0 {
		return SourceBatch{}, errors.New("monitor_waiting")
	}
	collected := int64(0)
	if s.cache.LastSuccessAt != nil {
		collected = *s.cache.LastSuccessAt
	}
	b, err := ParseCodexResets(s.cache.Raw, collected)
	if err != nil {
		return b, err
	}
	next := s.cache.NextAllowedAt
	b.Sync.LastAttemptAt = s.cache.LastAttemptAt
	b.Sync.ProxyMode = s.proxyState.Mode()
	b.Sync.LastSuccessAt = s.cache.LastSuccessAt
	b.Sync.NextPollAt = &next
	b.Sync.Origin = origin
	b.CheckedAt = s.cache.LastAttemptAt
	b.PublicFeedAvailable = s.cache.LastSuccessAt != nil && *s.cache.LastSuccessAt > 0 && *s.cache.LastSuccessAt <= s.now().Unix()+30 && s.now().Unix()-*s.cache.LastSuccessAt <= 1200 && s.cache.Failures == 0
	if !b.PublicFeedAvailable {
		b.Sync.State = "stale"
		b.Sync.Reason = "monitor_unavailable_or_stale"
		if s.cache.Failures > 0 {
			b.Sync.State = "offline"
			b.Sync.Reason = publicFailureReason("monitor", s.cache.FailureKind, s.cache.RetryAfterUntil, s.now().Unix(), b.Sync.Reason)
		}
	}
	return b, nil
}
func (s *CodexResetsSource) HealthSnapshot() messageinbox.SourceState {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, err := s.cached("public_monitor_cache")
	if err == nil {
		return *b.Sync
	}
	next := s.cache.NextAllowedAt
	a := reset.Attribution{Name: "Codex Resets", URL: "https://codex-resets.com"}
	reason := err.Error()
	if s.cache.Failures > 0 {
		reason = publicFailureReason("monitor", s.cache.FailureKind, s.cache.RetryAfterUntil, s.now().Unix(), reason)
	}
	return messageinbox.SourceState{ID: CodexResetsSourceID, Label: "Codex Resets", State: "offline", Reason: reason, LastAttemptAt: s.cache.LastAttemptAt, LastSuccessAt: s.cache.LastSuccessAt, NextPollAt: &next, Attribution: &a, ProxyMode: s.proxyState.Mode()}
}
func ParseCodexResets(raw []byte, collected int64) (SourceBatch, error) {
	var payload struct {
		Data []struct {
			ID     string `json:"id"`
			Kind   string `json:"reset_type"`
			At     string `json:"announced_at"`
			Text   string `json:"text"`
			Source struct {
				Type   string `json:"type"`
				Author string `json:"author"`
				URL    string `json:"url"`
			} `json:"source"`
		} `json:"data"`
		Pagination *struct {
			HasMore bool `json:"has_more"`
		} `json:"pagination"`
		Meta struct {
			Version   string `json:"api_version"`
			Generated string `json:"generated_at"`
		} `json:"meta"`
	}
	if len(raw) > 1<<20 || !utf8.Valid(raw) || json.Unmarshal(raw, &payload) != nil || payload.Data == nil || len(payload.Data) > 100 || payload.Pagination == nil || payload.Meta.Version != "v1" {
		return SourceBatch{}, errors.New("monitor_schema")
	}
	generated, err := nullableTime(&payload.Meta.Generated)
	if err != nil || generated == nil || *generated <= 0 || *generated > collected+300 {
		return SourceBatch{}, errors.New("monitor_generated_time")
	}
	attribution := reset.Attribution{Name: "Codex Resets", URL: "https://codex-resets.com"}
	health := messageinbox.SourceState{ID: CodexResetsSourceID, Label: "Codex Resets", State: "available", Attribution: &attribution, GeneratedAt: generated, UpstreamStatus: "collection_time_not_provided"}
	batch := SourceBatch{SourceID: CodexResetsSourceID, SourceKind: "public_aggregator", SourceLabel: "Codex Resets", Sync: &health, Evidence: []reset.Evidence{}, ResetProjection: true}
	seen := map[string]bool{}
	for _, record := range payload.Data {
		if record.ID == "" || len(record.ID) > 64 || seen[record.ID] || len(record.Text) > 16384 || (record.Kind != "regular" && record.Kind != "banked") {
			return SourceBatch{}, errors.New("monitor_record_invalid")
		}
		seen[record.ID] = true
		at, err := nullableTime(&record.At)
		if err != nil || at == nil || *at <= 0 || *at > collected+300 {
			return SourceBatch{}, errors.New("monitor_record_time")
		}
		kind := "reset"
		if record.Kind == "banked" {
			kind = "banked_credit"
		}
		e := reset.Evidence{ID: CodexResetsSourceID + ":" + record.ID, SourceID: CodexResetsSourceID, SourceEventID: record.ID, EventKind: kind, CollectedAt: collected, URL: record.Source.URL, Text: record.Text, Status: "active", Attribution: attribution}
		switch record.Source.Type {
		case "observed":
			e.Type = "external_observation"
			e.ObservedAt = at
			e.RelatedPostID = messageinbox.PostID(record.Source.URL)
			if record.Source.URL != "" && (e.RelatedPostID == "" || !strings.Contains(record.Source.URL, "/thsottiaux/status/")) {
				return SourceBatch{}, errors.New("monitor_observation_url")
			}
			if e.URL == "" {
				e.URL = attribution.URL
			}
		case "x_post":
			if record.Source.Author != "thsottiaux" || messageinbox.PostID(record.Source.URL) != record.ID || !strings.Contains(record.Source.URL, "/thsottiaux/status/") {
				return SourceBatch{}, errors.New("monitor_post_identity")
			}
			e.PostID = record.ID
			e.PublishedAt = at
			classified := resettext.Classify(record.Text)
			e.Type = "unclassified"
			if classified == "notice" {
				e.Type = "author_announcement"
			} else if classified == "announced" || reset.CompletionClaim(record.Text) {
				e.Type = "source_recorded_completion"
			} else if classified == "signal" {
				e.Type = "reset_signal"
			}
		default:
			return SourceBatch{}, errors.New("monitor_evidence_type")
		}
		batch.Evidence = append(batch.Evidence, e)
	}
	return batch, nil
}
