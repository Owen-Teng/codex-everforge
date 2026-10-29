// Package protocol is the public, account-free reset feed contract.
package protocol

const SourceID = "tibo-reset-cloud-v1"
const EventTTL int64 = 86400
const FeedTTL int64 = 180
const WireTTL int64 = 90

type Event struct {
	ID           string `json:"id"`
	Version      uint64 `json:"version"`
	Kind         string `json:"kind"`
	PublishedAt  int64  `json:"publishedAt"`
	ExpiresAt    int64  `json:"expiresAt"`
	SourceURL    string `json:"sourceUrl"`
	Summary      string `json:"summary"`
	Verification string `json:"verification"`
	Status       string `json:"status"`
}
type Upstream struct {
	State       string `json:"state"`
	Reason      string `json:"reason"`
	Connected   bool   `json:"connected"`
	LastWireAt  *int64 `json:"lastWireAt"`
	LastEventAt *int64 `json:"lastEventAt"`
}
type Feed struct {
	SchemaVersion int      `json:"schemaVersion"`
	SourceID      string   `json:"sourceId"`
	Revision      uint64   `json:"revision"`
	GeneratedAt   int64    `json:"generatedAt"`
	ExpiresAt     int64    `json:"expiresAt"`
	Synthetic     bool     `json:"synthetic"`
	Upstream      Upstream `json:"upstream"`
	Events        []Event  `json:"events"`
}
