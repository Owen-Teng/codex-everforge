// Package reset models attributed public evidence, never account recovery.
package reset

type Attribution struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}
type Evidence struct {
	ID            string      `json:"id"`
	SourceID      string      `json:"sourceId"`
	SourceEventID string      `json:"sourceEventId"`
	Type          string      `json:"type"`
	EventKind     string      `json:"eventKind"`
	PostID        string      `json:"postId,omitempty"`
	RelatedPostID string      `json:"relatedPostId,omitempty"`
	PublishedAt   *int64      `json:"publishedAt"`
	ObservedAt    *int64      `json:"observedAt"`
	CollectedAt   int64       `json:"collectedAt"`
	URL           string      `json:"url"`
	Text          string      `json:"text,omitempty"`
	Status        string      `json:"status"`
	Attribution   Attribution `json:"attribution"`
}
type Delivery struct {
	State     string `json:"state"`
	QueuedAt  *int64 `json:"queuedAt"`
	EmittedAt *int64 `json:"emittedAt"`
	ResultAt  *int64 `json:"resultAt"`
	Reason    string `json:"reason"`
}
type Event struct {
	Conflict                bool          `json:"conflict,omitempty"`
	ID                      string        `json:"id"`
	NotificationID          string        `json:"notificationId"`
	Kind                    string        `json:"kind"`
	Phase                   string        `json:"phase"`
	Label                   string        `json:"label"`
	Summary                 string        `json:"summary"`
	AnnouncedAt             *int64        `json:"announcedAt"`
	ObservedAt              *int64        `json:"observedAt"`
	ConfirmedAt             *int64        `json:"confirmedAt"`
	CollectedAt             int64         `json:"collectedAt"`
	PostIDs                 []string      `json:"postIds"`
	AccountRecoveryVerified bool          `json:"accountRecoveryVerified"`
	Independence            string        `json:"independence"`
	Attributions            []Attribution `json:"attributions"`
	Evidence                []Evidence    `json:"evidence"`
	Delivery                Delivery      `json:"delivery"`
}
type Link struct{ PostID, Author, Relation string }
type Post struct {
	ID, Author, Text, Category, Status, Verification, AggregateStatus, AggregateAction, ConfirmationBasis, TextCompleteness string
	PublishedAt                                                                                                             *int64
	CollectedAt                                                                                                             int64
	SourceIDs                                                                                                               []string
	Links                                                                                                                   []Link
}
