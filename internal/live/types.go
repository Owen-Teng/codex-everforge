// Package live provides local-only R2 state. It never uploads quota or identity.
package live

import (
	"codex-quota-monitor/internal/messageinbox"
	"codex-quota-monitor/internal/quota"
)

type Event struct {
	ID           string `json:"id"`
	Kind         string `json:"kind"`
	PublishedAt  int64  `json:"publishedAt"`
	SourceURL    string `json:"sourceUrl"`
	Summary      string `json:"summary"`
	Verification string `json:"verification"`
}
type Recovery struct {
	ID                 string `json:"id"`
	ObservedAt         int64  `json:"observedAt"`
	Kind               string `json:"kind"`
	Summary            string `json:"summary"`
	PreviousObservedAt int64  `json:"previousObservedAt"`
	BeforeRemaining    int    `json:"beforeRemaining"`
	AfterRemaining     int    `json:"afterRemaining"`
}
type ResetState struct {
	State         string    `json:"state"`
	SourceKind    string    `json:"sourceKind"`
	SourceLabel   string    `json:"sourceLabel"`
	LastCheckedAt *int64    `json:"lastCheckedAt"`
	LastSuccessAt *int64    `json:"lastSuccessAt"`
	SourceFresh   bool      `json:"sourceFresh"`
	Event         *Event    `json:"event"`
	LocalRecovery *Recovery `json:"localRecovery"`
	Reason        string    `json:"reason,omitempty"`
}
type Preferences struct {
	ResetAlertsEnabled bool `json:"resetAlertsEnabled"`
}
type Notification struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Title     string `json:"title"`
	Body      string `json:"body"`
	CreatedAt int64  `json:"createdAt"`
}
type State struct {
	generation    uint64
	Inbox         *messageinbox.Snapshot `json:"inbox,omitempty"`
	SchemaVersion int                    `json:"schemaVersion"`
	Mode          string                 `json:"mode"`
	Quota         quota.Snapshot         `json:"quota"`
	Usage         quota.UsageStatus      `json:"usage"`
	Reset         ResetState             `json:"reset"`
	Preferences   Preferences            `json:"preferences"`
	Notification  *Notification          `json:"notification"`
}
