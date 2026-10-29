package xactivity

import (
	"codex-quota-monitor/internal/live"
	"strings"
)

// Delivery uses the existing Notification DTO. Origin/claim metadata belongs
// to this candidate envelope, not the R2 Event verification enum.
type Delivery struct {
	SchemaVersion int                `json:"schemaVersion"`
	Mode          string             `json:"mode"`
	Real          *bool              `json:"real,omitempty"`
	Source        string             `json:"source,omitempty"`
	Verification  string             `json:"verification,omitempty"`
	Notification  *live.Notification `json:"notification"`
}
type ReplayLine = Delivery

func LiveDelivery(decision Decision, real bool) Delivery {
	verification := "unverified"
	if decision.Notification != nil {
		verification = "author_claim"
	}
	return Delivery{SchemaVersion: 1, Mode: "x_activity_candidate", Real: &real, Source: "x_activity_stream", Verification: verification, Notification: decision.Notification}
}
func ReplayDelivery(decision Decision) Delivery {
	line := Delivery{SchemaVersion: 1, Mode: "synthetic_x_replay"}
	if decision.Notification != nil {
		copy := *decision.Notification
		copy.Title = "联调 · " + copy.Title
		copy.Body = "合成 X 事件联调，非真实公告。" + strings.Split(copy.Body, " https://x.com/")[0]
		line.Notification = &copy
	}
	return line
}
