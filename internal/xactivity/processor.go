package xactivity

import (
	"bytes"
	"codex-quota-monitor/internal/live"
	"codex-quota-monitor/internal/resettext"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"
	"unicode/utf8"
)

type Post struct {
	ID         string
	EventID    string
	Text       string
	CreatedAt  time.Time
	DeliveryAt time.Time
}
type envelope struct {
	Data *struct {
		EventID        string `json:"event_uuid"`
		CreatedAt      string `json:"created_at"`
		EventType      string `json:"event_type"`
		Tag            string `json:"tag"`
		SubscriptionID string `json:"subscription_id"`
		Filter         struct {
			UserID string `json:"user_id"`
		} `json:"filter"`
		Payload struct {
			ID         string `json:"id"`
			AuthorID   string `json:"author_id"`
			CreatedAt  string `json:"created_at"`
			Text       string `json:"text"`
			Referenced []struct {
				Type string `json:"type"`
				ID   string `json:"id"`
			} `json:"referenced_tweets"`
			ReferencedPosts []struct {
				Type string `json:"type"`
				ID   string `json:"id"`
			} `json:"referenced_posts"`
			QuotedID  string          `json:"quoted_status_id_str"`
			Retweeted json.RawMessage `json:"retweeted_status"`
			Quoted    json.RawMessage `json:"quoted_status"`
		} `json:"payload"`
	} `json:"data"`
	Errors json.RawMessage `json:"errors"`
}

var deliveryID = regexp.MustCompile(`^[A-Za-z0-9-]{1,64}$`)

func Parse(raw []byte, c Config) (Post, error) {
	if len(raw) > 65536 || !utf8.Valid(raw) || !uniqueJSONKeys(raw) {
		return Post{}, errors.New("invalid_frame")
	}
	var e envelope
	if json.Unmarshal(raw, &e) != nil || e.Data == nil || !noErrors(e.Errors) {
		return Post{}, errors.New("invalid_frame")
	}
	d := e.Data
	p := d.Payload
	if d.EventType != "post.create" || d.Tag != c.Tag || d.Filter.UserID != c.AuthorID || p.AuthorID != c.AuthorID || d.SubscriptionID != "" && d.SubscriptionID != c.SubscriptionID {
		return Post{}, errors.New("target_mismatch")
	}
	if !deliveryID.MatchString(d.EventID) || !numericID.MatchString(p.ID) {
		return Post{}, errors.New("invalid_identity")
	}
	if p.QuotedID != "" || len(p.Quoted) > 0 && string(p.Quoted) != "null" || len(p.Retweeted) > 0 && string(p.Retweeted) != "null" {
		return Post{}, errors.New("quoted_or_reposted")
	}
	for _, ref := range append(p.Referenced, p.ReferencedPosts...) {
		if ref.Type != "replied_to" {
			return Post{}, errors.New("quoted_or_reposted")
		}
	}
	created, err := time.Parse(time.RFC3339, p.CreatedAt)
	if err != nil {
		return Post{}, errors.New("invalid_time")
	}
	delivery, err := time.Parse(time.RFC3339, d.CreatedAt)
	if err != nil {
		return Post{}, errors.New("invalid_time")
	}
	text := safeText(p.Text)
	if text == "" {
		return Post{}, errors.New("unsafe_text")
	}
	return Post{ID: p.ID, EventID: d.EventID, Text: text, CreatedAt: created, DeliveryAt: delivery}, nil
}

func uniqueJSONKeys(raw []byte) bool {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	var walk func(int) bool
	walk = func(depth int) bool {
		if depth > 32 {
			return false
		}
		token, err := decoder.Token()
		if err != nil {
			return false
		}
		delimiter, ok := token.(json.Delim)
		if !ok {
			return true
		}
		switch delimiter {
		case '{':
			seen := map[string]bool{}
			for decoder.More() {
				key, err := decoder.Token()
				name, ok := key.(string)
				if err != nil || !ok || seen[name] {
					return false
				}
				seen[name] = true
				if !walk(depth + 1) {
					return false
				}
			}
			end, err := decoder.Token()
			return err == nil && end == json.Delim('}')
		case '[':
			for decoder.More() {
				if !walk(depth + 1) {
					return false
				}
			}
			end, err := decoder.Token()
			return err == nil && end == json.Delim(']')
		}
		return false
	}
	if !walk(0) {
		return false
	}
	_, err := decoder.Token()
	return err == io.EOF
}
func safeText(text string) string { return resettext.Sanitize(text) }
func Classify(text string) string { return resettext.Classify(text) }

type Decision struct {
	SourcePost   *Post `json:"-"`
	Notification *live.Notification
	Reason       string
	Accepted     bool
}
type Processor struct {
	mu        sync.Mutex
	Config    Config
	Now       func() time.Time
	started   time.Time
	seen      map[string]int64
	stateFile string
}

func NewProcessor(c Config, now func() time.Time, stateFile string) (*Processor, error) {
	return newProcessor(c, now, stateFile, false)
}
func newProcessor(c Config, now func() time.Time, stateFile string, observation bool) (*Processor, error) {
	validate := c.Validate
	if observation {
		validate = c.ValidateObservation
	}
	if err := validate(); err != nil {
		return nil, err
	}
	if now == nil {
		now = time.Now
	}
	p := &Processor{Config: c, Now: now, started: now(), seen: map[string]int64{}, stateFile: stateFile}
	if stateFile != "" {
		data, err := os.ReadFile(stateFile)
		if err == nil {
			if len(data) > 262144 || json.Unmarshal(data, &p.seen) != nil || p.seen == nil {
				return nil, errors.New("dedup_state_invalid")
			}
		} else if !os.IsNotExist(err) {
			return nil, errors.New("dedup_state_unavailable")
		}
	}
	return p, nil
}
func digest(key string) string { sum := sha256.Sum256([]byte(key)); return hex.EncodeToString(sum[:]) }
func (p *Processor) Process(raw []byte) Decision {
	p.mu.Lock()
	defer p.mu.Unlock()
	post, err := Parse(raw, p.Config)
	if err != nil {
		return Decision{Reason: err.Error()}
	}
	now := p.Now()
	cutoff := p.started.Add(-5 * time.Minute)
	// A long observation session must not turn a late delivery from hours ago
	// into a fresh alert merely because it was published after session start.
	if rolling := now.Add(-5 * time.Minute); rolling.After(cutoff) {
		cutoff = rolling
	}
	if post.CreatedAt.Before(cutoff) || now.Sub(post.CreatedAt) > 24*time.Hour || post.CreatedAt.After(now.Add(5*time.Minute)) || post.DeliveryAt.Before(cutoff) || post.DeliveryAt.After(now.Add(5*time.Minute)) || post.CreatedAt.After(post.DeliveryAt.Add(5*time.Minute)) {
		return Decision{Reason: "old_or_future"}
	}
	eventKey := digest(p.Config.Tag + ":" + p.Config.AuthorID + ":event:" + post.EventID)
	postKey := digest(p.Config.Tag + ":" + p.Config.AuthorID + ":post:" + post.ID)
	if _, ok := p.seen[eventKey]; ok {
		return Decision{Reason: "duplicate"}
	}
	if _, ok := p.seen[postKey]; ok {
		return Decision{Reason: "duplicate"}
	}
	for key, stamp := range p.seen {
		if now.Unix()-stamp > 24*3600 {
			delete(p.seen, key)
		}
	}
	if len(p.seen) >= 2000 {
		return Decision{Reason: "dedup_capacity"}
	}
	p.seen[eventKey] = now.Unix()
	p.seen[postKey] = now.Unix()
	if err := p.persist(); err != nil {
		delete(p.seen, eventKey)
		delete(p.seen, postKey)
		return Decision{Reason: "dedup_save_failed"}
	}
	kind := Classify(post.Text)
	if kind == "" {
		return Decision{Accepted: true, Reason: "ambiguous_or_unrelated"}
	}
	title := "作者预告 Codex 额度重置"
	body := "目标作者发布了重置预告。具体安排以原帖为准；本机恢复尚未确认。"
	notificationKind := kind
	if kind == "announced" {
		title = "作者称 Codex 额度已重置"
		body = "目标作者声称已完成重置；本机恢复仍待独立额度核对。"
	} else if kind == "signal" {
		notificationKind = "notice"
		title = "作者发布重置信号（范围未明确）"
		body = "目标作者的 Day N 系列出现 Rest and reset。重置范围未明确，尚不能认定为 Codex 额度重置；本机恢复需独立核对。"
	}
	return Decision{SourcePost: &post, Accepted: true, Reason: kind, Notification: &live.Notification{ID: postKey, Kind: notificationKind, Title: title, Body: body + " https://x.com/i/web/status/" + post.ID, CreatedAt: now.Unix()}}
}
func (p *Processor) persist() error {
	if p.stateFile == "" {
		return nil
	}
	if os.MkdirAll(filepath.Dir(p.stateFile), 0700) != nil {
		return errors.New("dedup_save_failed")
	}
	data, _ := json.Marshal(p.seen)
	file, err := os.CreateTemp(filepath.Dir(p.stateFile), "x-seen-*.tmp")
	if err != nil {
		return err
	}
	name := file.Name()
	defer os.Remove(name)
	_ = file.Chmod(0600)
	_, err = file.Write(data)
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(name, p.stateFile)
	}
	return err
}
