package live

import (
	"codex-quota-monitor/internal/resetcloud/protocol"
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"time"
)

type CloudSource struct {
	endpoint  string
	synthetic bool
	client    *http.Client
}

func NewCloudSource(endpoint string) (*CloudSource, error) { return cloudSource(endpoint, false) }

// NewSyntheticCloudSource permits only a literal loopback HTTP fixture. The
// core additionally requires --demo before this constructor can be selected.
func NewSyntheticCloudSource(endpoint string) (*CloudSource, error) {
	return cloudSource(endpoint, true)
}
func cloudSource(endpoint string, synthetic bool) (*CloudSource, error) {
	if endpoint != "" {
		u, err := url.Parse(endpoint)
		if err != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "/v1/feed" || u.Hostname() == "" {
			return nil, errors.New("cloud_endpoint_invalid")
		}
		if synthetic {
			ip := net.ParseIP(u.Hostname())
			if u.Scheme != "http" || ip == nil || !ip.IsLoopback() {
				return nil, errors.New("cloud_test_endpoint_not_loopback")
			}
		} else if u.Scheme != "https" {
			return nil, errors.New("cloud_https_required")
		}
	}
	return &CloudSource{endpoint: endpoint, synthetic: synthetic, client: &http.Client{Timeout: 12 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}
func (s *CloudSource) SourceDescriptor() (string, string) {
	if s.synthetic {
		return "synthetic_cloud", "云公告联调（合成）"
	}
	return "cloud_feed", "作者公开公告"
}
func (s *CloudSource) PollInterval() time.Duration {
	if s.synthetic {
		return time.Second
	}
	return 30 * time.Second
}
func (s *CloudSource) Fetch(ctx context.Context) (SourceBatch, error) {
	if s.endpoint == "" {
		return SourceBatch{}, errors.New("cloud_endpoint_not_configured")
	}
	var f protocol.Feed
	if err := readPublicJSON(ctx, s.client, s.endpoint, &f); err != nil {
		return SourceBatch{}, err
	}
	now := time.Now().Unix()
	if f.SchemaVersion != 1 || f.SourceID == "" || len(f.SourceID) > 128 || plainSummary(f.SourceID) != f.SourceID || f.Events == nil || len(f.Events) > 2000 || f.GeneratedAt <= 0 || f.GeneratedAt > now+30 || f.ExpiresAt <= now || f.ExpiresAt <= f.GeneratedAt || f.ExpiresAt-f.GeneratedAt > protocol.FeedTTL || now-f.GeneratedAt > protocol.FeedTTL {
		return SourceBatch{}, errors.New("cloud_feed_invalid_or_expired")
	}
	if f.Synthetic != s.synthetic {
		return SourceBatch{}, errors.New("cloud_source_mode_mismatch")
	}
	kind, label := s.SourceDescriptor()
	b := SourceBatch{SourceID: f.SourceID, SourceKind: kind, SourceLabel: label, Events: []Event{}, UpstreamSuccessAt: f.Upstream.LastWireAt, CloudFeedAvailable: true}
	if !f.Upstream.Connected || f.Upstream.State != "connected" || f.Upstream.LastWireAt == nil || *f.Upstream.LastWireAt <= 0 || *f.Upstream.LastWireAt > now+5 || now-*f.Upstream.LastWireAt > protocol.WireTTL {
		b.UnavailableReason = "cloud_upstream_stopped_or_stale"
	}
	ids := map[string]bool{}
	for _, entry := range f.Events {
		e := Event{ID: entry.ID, Kind: entry.Kind, PublishedAt: entry.PublishedAt, SourceURL: entry.SourceURL, Summary: entry.Summary, Verification: entry.Verification}
		if ids[e.ID] || entry.Version == 0 || entry.Verification != "author_claim" || (entry.Kind != "notice" && entry.Kind != "announced") || !validEvent(e) || entry.ExpiresAt <= entry.PublishedAt || entry.ExpiresAt-entry.PublishedAt > protocol.EventTTL || (entry.Status != "active" && entry.Status != "withdrawn") {
			return SourceBatch{}, errors.New("cloud_event_invalid")
		}
		ids[e.ID] = true
		if entry.Status == "withdrawn" || entry.ExpiresAt <= now || entry.PublishedAt > now+300 {
			b.SuppressedIDs = append(b.SuppressedIDs, entry.ID)
			continue
		}
		b.Events = append(b.Events, e)
	}
	return b, nil
}
