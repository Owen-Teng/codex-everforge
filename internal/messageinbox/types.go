package messageinbox

import "codex-quota-monitor/internal/reset"

type Delivery struct {
	State     string `json:"state"`
	QueuedAt  *int64 `json:"queuedAt"`
	EmittedAt *int64 `json:"emittedAt"`
	ResultAt  *int64 `json:"resultAt"`
	Reason    string `json:"reason"`
}
type Reference struct {
	PostID   string `json:"postId"`
	Author   string `json:"author"`
	Relation string `json:"relation"`
}
type Message struct {
	ResetEventIDs     []string     `json:"resetEventIds,omitempty"`
	DisplayText       *DisplayText `json:"displayText,omitempty"`
	ConfirmationBasis string       `json:"confirmationBasis,omitempty"`
	Preview           *Preview     `json:"preview,omitempty"`
	NotificationID    string       `json:"notificationId"`
	PostID            string       `json:"postId"`
	URL               string       `json:"url"`
	Author            string       `json:"author"`
	PublishedAt       *int64       `json:"publishedAt"`
	FirstReceivedAt   int64        `json:"firstReceivedAt"`
	LastSeenAt        int64        `json:"lastSeenAt"`
	OriginalText      string       `json:"originalText"`
	Translation       *string      `json:"translation"`
	TextCompleteness  string       `json:"textCompleteness"`
	Category          string       `json:"category"`
	Verification      string       `json:"verification"`
	Status            string       `json:"status"`
	Sources           []string     `json:"sources"`
	References        []Reference  `json:"references"`
	AggregateStatus   string       `json:"aggregateStatus,omitempty"`
	AggregateAction   string       `json:"aggregateAction,omitempty"`
	Delivery          Delivery     `json:"delivery"`
}
type SourceState struct {
	ProxyMode           string             `json:"proxyMode,omitempty"`
	Attribution         *reset.Attribution `json:"attribution,omitempty"`
	GeneratedAt         *int64             `json:"generatedAt,omitempty"`
	ID                  string             `json:"id"`
	Label               string             `json:"label"`
	State               string             `json:"state"`
	Reason              string             `json:"reason"`
	LastAttemptAt       *int64             `json:"lastAttemptAt"`
	LastSuccessAt       *int64             `json:"lastSuccessAt"`
	UpstreamCheckedAt   *int64             `json:"upstreamCheckedAt"`
	UpstreamCollectedAt *int64             `json:"upstreamCollectedAt"`
	UpstreamStatus      string             `json:"upstreamStatus"`
	NextPollAt          *int64             `json:"nextPollAt"`
	Origin              string             `json:"origin"`
}
type Snapshot struct {
	ResetEvents   []reset.Event `json:"resetEvents,omitempty"`
	Sources       []SourceState `json:"sources,omitempty"`
	SchemaVersion int           `json:"schemaVersion"`
	Revision      uint64        `json:"revision"`
	Source        SourceState   `json:"source"`
	Messages      []Message     `json:"messages"`
}
type DisplayText struct {
	Text   string `json:"text"`
	Basis  string `json:"basis"`
	Source string `json:"source"`
}
