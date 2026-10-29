package reset

import (
	"codex-quota-monitor/internal/resettext"
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
)

func NotificationID(key string) string {
	sum := sha256.Sum256([]byte("reset-stage:" + key))
	return hex.EncodeToString(sum[:])
}
func Root(id string, posts map[string]Post) string {
	seen := map[string]bool{}
	// Legacy monitor-recorded propagation statements already have independent
	// source evidence; retain their historical grouping through context excerpts.
	// New generic completions never inherit this compatibility exception.
	legacyPropagation := legacyFollowup(posts[id].Text)
	var walk func(string, int) string
	walk = func(current string, depth int) string {
		p, ok := posts[current]
		if !ok || depth > 8 || seen[current] {
			return current
		}
		seen[current] = true
		defer delete(seen, current)
		if p.Author == "thsottiaux" && p.Status == "active" && resettext.Classify(p.Text) != "" {
			return current
		}
		roots := []string{}
		for _, r := range p.Links {
			parent, exists := posts[r.PostID]
			if !exists || r.Author != "thsottiaux" || parent.Author != "thsottiaux" || (r.Relation != "reply" && r.Relation != "quote") || parent.Status != "active" {
				continue
			}
			if resettext.ContextKind(p.Text) != "" && !legacyPropagation {
				if !completePost(parent) || !contextTimes(parent, p, posts[id]) || parent.TextCompleteness != "" && parent.PublishedAt == nil {
					continue
				}
				if resettext.Classify(parent.Text) == "" && (resettext.ContextKind(parent.Text) != "deadline" || !knownResetChain(parent.ID, posts)) {
					continue
				}
			}
			roots = append(roots, walk(r.PostID, depth+1))
		}
		if len(roots) == 0 {
			return current
		}
		sort.Strings(roots)
		return roots[0]
	}
	return walk(id, 0)
}
func completion(text string) bool {
	return resettext.ContextKind(text) == "completion"
}

// CompletionClaim is the legacy monitor-record contract. New generic follow-ups
// such as "And it's done" need the explicit post graph, never this shortcut.
func CompletionClaim(text string) bool {
	return completion(text) && strings.HasPrefix(strings.ToLower(resettext.Sanitize(text)), "it is all propagated")
}
func legacyFollowup(text string) bool {
	clean := strings.TrimSuffix(strings.ToLower(resettext.Sanitize(text)), ".")
	return CompletionClaim(text) || clean == "by midnight pst"
}
func resetChainKind(id string, posts map[string]Post) string {
	seen := map[string]bool{}
	origin, ok := posts[id]
	if !ok {
		return ""
	}
	var visit func(string, Post, int) string
	visit = func(current string, child Post, depth int) string {
		if depth > 8 || seen[current] {
			return ""
		}
		seen[current] = true
		defer delete(seen, current)
		p, ok := posts[current]
		if !ok || p.Author != "thsottiaux" || p.Status != "active" {
			return ""
		}
		if !completePost(p) || p.TextCompleteness != "" && p.PublishedAt == nil {
			// Preserve the old narrow Day-series signal/context contract only for
			// old propagation/deadline wording. A new generic done still needs a
			// complete dated root, so excerpts cannot newly establish completion.
			if !legacyFollowup(origin.Text) || p.TextCompleteness != "context" || resettext.Classify(p.Text) != "signal" {
				return ""
			}
		}
		if !contextTimes(p, child, origin) {
			return ""
		}
		if kind := resettext.Classify(p.Text); kind != "" {
			return kind
		}
		// Arbitrary intervening replies cannot bridge an unrelated completion.
		if resettext.ContextKind(p.Text) != "deadline" {
			return ""
		}
		for _, r := range p.Links {
			if r.Author == "thsottiaux" && (r.Relation == "reply" || r.Relation == "quote") {
				if kind := visit(r.PostID, p, depth+1); kind != "" {
					return kind
				}
			}
		}
		return ""
	}
	for _, r := range origin.Links {
		if r.Author == "thsottiaux" && (r.Relation == "reply" || r.Relation == "quote") {
			if kind := visit(r.PostID, origin, 0); kind != "" {
				return kind
			}
		}
	}
	return ""
}
func knownResetChain(id string, posts map[string]Post) bool { return resetChainKind(id, posts) != "" }
func completePost(p Post) bool                              { return p.TextCompleteness == "" || p.TextCompleteness == "full" }
func contextTimes(parent, child, origin Post) bool {
	if parent.PublishedAt != nil && child.PublishedAt != nil && (*parent.PublishedAt > *child.PublishedAt || *child.PublishedAt-*parent.PublishedAt > 86400) {
		return false
	}
	return parent.PublishedAt == nil || origin.PublishedAt == nil || *origin.PublishedAt-*parent.PublishedAt <= 86400
}

// Category derives semantics from raw text and explicit references; it never
// trusts a saved category, so late context and upgrades can rederive old rows.
func Category(p Post, posts map[string]Post) string {
	if p.Author != "thsottiaux" || p.Status != "active" {
		return "related"
	}
	if !completePost(p) {
		return "unknown"
	}
	if kind := resettext.Classify(p.Text); kind != "" {
		return kind
	}
	followup := resettext.ContextKind(p.Text)
	if followup != "" {
		root := resetChainKind(p.ID, posts)
		if root == "signal" {
			return "signal"
		}
		if root != "" {
			if followup == "completion" {
				return "announced"
			}
			return "notice"
		}
	}
	clean := strings.ToLower(resettext.Sanitize(p.Text))
	if clean == "" || strings.ContainsAny(clean, "?？") || strings.Contains(clean, "not ") || strings.Contains(clean, "withdraw") || strings.Contains(clean, "correction") {
		return "unknown"
	}
	return "related"
}
func postEvidence(p Post, posts map[string]Post) (Evidence, bool) {
	if p.Author != "thsottiaux" || p.Status != "active" || p.PublishedAt == nil || !completePost(p) {
		return Evidence{}, false
	}
	typ := ""
	classified := resettext.Classify(p.Text)
	switch p.Category {
	case "notice":
		if classified == "notice" {
			typ = "author_announcement"
		}
	case "announced":
		if classified == "announced" {
			typ = "author_completion"
		}
	case "signal":
		if classified == "signal" || resettext.ContextKind(p.Text) != "" && knownResetChain(p.ID, posts) {
			typ = "reset_signal"
		}
	}
	if completion(p.Text) && p.AggregateStatus == "confirmed" && p.AggregateAction == "confirm" && (p.ConfirmationBasis == "source_post" || p.ConfirmationBasis == "" && CompletionClaim(p.Text)) && knownResetChain(p.ID, posts) {
		typ = "contextual_author_completion"
	}
	if typ == "" {
		return Evidence{}, false
	}
	source := "aihot.news/codex-resets/v1"
	credit := Attribution{Name: "AIHOT", URL: "https://aihot.news/codex-reset"}
	return Evidence{ID: source + ":post:" + p.ID, SourceID: source, SourceEventID: p.ID, Type: typ, EventKind: "reset", PostID: p.ID, PublishedAt: p.PublishedAt, CollectedAt: p.CollectedAt, URL: "https://x.com/thsottiaux/status/" + p.ID, Status: "active", Attribution: credit}, true
}
func phaseFor(e Evidence) string {
	switch e.Type {
	case "author_announcement":
		return "announced"
	case "author_completion", "contextual_author_completion", "source_recorded_completion":
		return "author_completed"
	case "external_observation":
		return "externally_observed"
	case "withdrawn":
		return "withdrawn"
	default:
		return "signal"
	}
}
func rank(phase string) int {
	switch phase {
	case "withdrawn":
		return 5
	case "author_completed":
		return 4
	case "externally_observed":
		return 3
	case "announced":
		return 2
	default:
		return 1
	}
}
func latest(a, b *int64) *int64 {
	if a == nil {
		return b
	}
	if b == nil || *a >= *b {
		return a
	}
	return b
}
func first(a, b *int64) *int64 {
	if a == nil {
		return b
	}
	if b == nil || *a <= *b {
		return a
	}
	return b
}
func evidenceKey(e Evidence) string {
	anchor := e.PostID
	if anchor == "" {
		anchor = e.RelatedPostID
	}
	if anchor == "" {
		anchor = e.SourceID + ":" + e.SourceEventID
	}
	return anchor + ":" + phaseFor(e)
}
func Build(posts map[string]Post, external []Evidence) []Event {
	all := append([]Evidence(nil), external...)
	for _, p := range posts {
		if e, ok := postEvidence(p, posts); ok {
			all = append(all, e)
		}
	}
	grouped := map[string]*Event{}
	keys := map[string]string{}
	for _, e := range all {
		if e.Type == "unclassified" {
			continue
		}
		if e.Status != "active" && e.Type != "withdrawn" {
			continue
		}
		anchor := e.PostID
		if anchor == "" {
			anchor = e.RelatedPostID
		}
		id := "monitor:" + e.SourceID + ":" + e.SourceEventID
		if anchor != "" {
			id = "reset:" + Root(anchor, posts)
		}
		item := grouped[id]
		if item == nil {
			item = &Event{ID: id, Kind: "reset", Phase: "signal", Independence: "unverified", PostIDs: []string{}, Evidence: []Evidence{}, Attributions: []Attribution{}}
			grouped[id] = item
		}
		duplicate := false
		for _, known := range item.Evidence {
			if known.ID == e.ID {
				duplicate = true
			}
		}
		if duplicate {
			continue
		}
		if p, ok := posts[e.PostID]; ok && p.Status == "withdrawn" {
			e.Type = "withdrawn"
			e.Text = ""
		}
		item.Evidence = append(item.Evidence, e)
		if e.EventKind == "banked_credit" {
			item.Kind = "banked_credit"
		}
		current := phaseFor(e)
		if rank(current) > rank(item.Phase) || (rank(current) == rank(item.Phase) && evidenceKey(e) < keys[id]) {
			item.Phase = current
			keys[id] = evidenceKey(e)
		}
		if keys[id] == "" {
			keys[id] = evidenceKey(e)
		}
		if current == "author_completed" {
			item.ConfirmedAt = latest(item.ConfirmedAt, e.PublishedAt)
		} else if current == "externally_observed" {
			item.ObservedAt = latest(item.ObservedAt, e.ObservedAt)
		} else {
			item.AnnouncedAt = first(item.AnnouncedAt, e.PublishedAt)
		}
		item.CollectedAt = max(item.CollectedAt, e.CollectedAt)
		postID := e.PostID
		if postID == "" {
			postID = e.RelatedPostID
		}
		if postID != "" && !includes(item.PostIDs, postID) {
			item.PostIDs = append(item.PostIDs, postID)
		}
		hasCredit := false
		for _, a := range item.Attributions {
			if a.URL == e.Attribution.URL {
				hasCredit = true
			}
		}
		if !hasCredit {
			item.Attributions = append(item.Attributions, e.Attribution)
		}
	}
	result := []Event{}
	for id, item := range grouped {
		if item.Phase == "withdrawn" {
			for _, e := range item.Evidence {
				if e.Type != "withdrawn" && phaseFor(e) != "signal" {
					item.Conflict = true
				}
			}
		}
		if strings.HasPrefix(id, "reset:") {
			root := strings.TrimPrefix(id, "reset:")
			if !includes(item.PostIDs, root) {
				item.PostIDs = append(item.PostIDs, root)
			}
		}
		item.NotificationID = NotificationID(keys[id])
		sort.Strings(item.PostIDs)
		sort.Slice(item.Evidence, func(i, j int) bool { return item.Evidence[i].ID < item.Evidence[j].ID })
		sort.Slice(item.Attributions, func(i, j int) bool { return item.Attributions[i].Name < item.Attributions[j].Name })
		setCopy(item)
		result = append(result, *item)
	}
	sort.Slice(result, func(i, j int) bool {
		a, b := EventTime(result[i]), EventTime(result[j])
		if a == b {
			return result[i].ID < result[j].ID
		}
		return a > b
	})
	return result
}
func includes(values []string, v string) bool {
	for _, s := range values {
		if s == v {
			return true
		}
	}
	return false
}
func EventTime(e Event) int64 {
	for _, value := range []*int64{e.ConfirmedAt, e.ObservedAt, e.AnnouncedAt} {
		if value != nil {
			return *value
		}
	}
	return 0
}
func setCopy(e *Event) {
	switch e.Phase {
	case "announced":
		e.Label = "Tibo重置预告"
		e.Summary = "Tibo 又要重置啦，具体安排以原文为准。"
	case "author_completed":
		e.Label = "Tibo确认完成"
		e.Summary = "来源已收录作者完成声明；本机额度仍看实际读数。"
	case "externally_observed":
		e.Label = "有重置的好消息啦"
		e.Summary = "看看你的额度有没有回来；这是监控来源的观测记录。"
	case "withdrawn":
		e.Label = "公告已撤回"
		e.Summary = "来源撤回了这条公告，当前不据此判断重置。"
	default:
		e.Label = "重置范围未明"
		e.Summary = "作者提到重置，但范围及落地情况仍未明确。"
	}
	if e.Kind == "banked_credit" {
		switch e.Phase {
		case "announced":
			e.Label = "重置卡预告"
			e.Summary = "作者预告发放重置卡，不等于已经替账户重置额度。"
		case "author_completed":
			e.Label = "作者称已发重置卡"
			e.Summary = "来源记录了重置卡发放声明，是否可用仍以账户为准。"
		case "externally_observed":
			e.Label = "第三方记录重置卡"
			e.Summary = "监控来源记录了重置卡，不代表本机额度已恢复。"
		}
	}
}
