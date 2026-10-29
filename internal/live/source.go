package live

import (
	"codex-quota-monitor/internal/messageinbox"
	"codex-quota-monitor/internal/reset"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
)

const SourceTTL = 6 * time.Hour
const EventTTL = 24 * time.Hour
const FutureTolerance = 5 * time.Minute

type SourceBatch struct {
	Evidence            []reset.Evidence
	Segments            []SourceBatch
	ResetProjection     bool
	RecordOnly          bool
	SourceID            string
	SourceKind          string
	SourceLabel         string
	UnavailableReason   string
	SuppressedIDs       []string
	CloudFeedAvailable  bool
	Events              []Event
	Posts               []messageinbox.Message
	Sync                *messageinbox.SourceState
	HistoryFrom         *int64
	CheckedAt           *int64
	PublicFeedAvailable bool
	// Must mean a proven upstream successful collection, not HTTP fetch time,
	// health checkedAt, generatedAt, or the latest event's publication time.
	UpstreamSuccessAt *int64
}
type Source interface {
	Fetch(context.Context) (SourceBatch, error)
}

type TiboSource struct{ Client *http.Client }

func NewTiboSource() *TiboSource {
	return &TiboSource{Client: &http.Client{Timeout: 12 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}
func readPublicJSON(ctx context.Context, client *http.Client, endpoint string, out any) error {
	req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if err != nil {
		return errors.New("source_request")
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "CodexQuotaMonitor-R2-Local/0.1")
	res, err := client.Do(req)
	if err != nil {
		return errors.New("source_network")
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return errors.New("source_http_" + strconv.Itoa(res.StatusCode))
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, 1<<20+1))
	if err != nil || len(data) > 1<<20 {
		return errors.New("source_body")
	}
	if json.Unmarshal(data, out) != nil {
		return errors.New("source_schema")
	}
	return nil
}
func (s *TiboSource) Fetch(ctx context.Context) (SourceBatch, error) {
	var health struct {
		Status string `json:"status"`
	}
	if err := readPublicJSON(ctx, s.Client, "https://tibo.modelyard.dev/api/health", &health); err != nil {
		return SourceBatch{}, err
	}
	if health.Status != "ok" {
		return SourceBatch{}, errors.New("source_health")
	}
	var payload struct {
		Data []json.RawMessage `json:"data"`
	}
	if err := readPublicJSON(ctx, s.Client, "https://tibo.modelyard.dev/api/events", &payload); err != nil {
		return SourceBatch{}, err
	}
	if payload.Data == nil || len(payload.Data) > 200 {
		return SourceBatch{}, errors.New("source_schema")
	}
	batch := SourceBatch{SourceID: "tibo.modelyard.dev", Events: []Event{}}
	for _, raw := range payload.Data {
		if event, ok := parseTiboEvent(raw); ok {
			batch.Events = append(batch.Events, event)
		}
	}
	// The current endpoint exposes only health.checkedAt, NOT upstream success.
	// Keep freshness unknown/false even when both HTTP requests succeeded.
	return batch, nil
}

var postPath = regexp.MustCompile(`^/[A-Za-z0-9_]{1,30}/status/[0-9]+/?$`)

func validPostURL(raw string) bool {
	if len(raw) > 512 || strings.ContainsAny(raw, "\\\r\n\t") {
		return false
	}
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.User == nil && (u.Host == "x.com" || u.Host == "twitter.com") && postPath.MatchString(u.Path) && u.RawQuery == "" && u.Fragment == ""
}
func plainSummary(raw string) string {
	// Reject markup rather than convert it to an executable or misleading label.
	if strings.ContainsAny(raw, "<>") {
		return ""
	}
	runes := []rune(strings.TrimSpace(raw))
	if len(runes) > 280 {
		runes = runes[:280]
	}
	for _, r := range runes {
		if unicode.IsControl(r) || unicode.In(r, unicode.Cf) {
			return ""
		}
	}
	return string(runes)
}
func parseTiboEvent(raw json.RawMessage) (Event, bool) {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return Event{}, false
	}
	str := func(name string) string { var s string; _ = json.Unmarshal(fields[name], &s); return s }
	kind := ""
	switch str("category") {
	case "RESET_PLANNED":
		kind = "notice"
	case "RESET_COMPLETED":
		kind = "announced"
	case "RESET_CORRECTION":
		kind = "correction"
	default:
		return Event{}, false
	}
	id := str("id")
	if id == "" {
		var number json.Number
		if json.Unmarshal(fields["id"], &number) == nil {
			id = number.String()
		}
	}
	if len(id) == 0 || len(id) > 128 || plainSummary(id) != id {
		return Event{}, false
	}
	stamp, err := time.Parse(time.RFC3339, str("published_at"))
	if err != nil {
		return Event{}, false
	}
	summary := plainSummary(str("summary_zh"))
	if summary == "" {
		summary = plainSummary(str("summary"))
	}
	if summary == "" {
		return Event{}, false
	}
	u := str("source_url")
	if !validPostURL(u) {
		return Event{}, false
	}
	return Event{ID: id, Kind: kind, PublishedAt: stamp.Unix(), SourceURL: u, Summary: summary, Verification: "third_party"}, true
}

func validEvent(event Event) bool {
	return event.ID != "" && len(event.ID) <= 128 && plainSummary(event.ID) == event.ID &&
		(event.Kind == "notice" || event.Kind == "announced" || event.Kind == "correction") &&
		event.PublishedAt > 0 && validPostURL(event.SourceURL) && plainSummary(event.Summary) == event.Summary && event.Summary != "" && (event.Verification == "third_party" || event.Verification == "author_claim")
}
