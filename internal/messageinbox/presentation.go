package messageinbox

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Preview is an additive display projection, never classification, translation,
// notification eligibility or evidence of this device's quota recovery.
type Preview struct {
	Kind    string `json:"kind"`
	Label   string `json:"label"`
	Summary string `json:"summary"`
	Basis   string `json:"basis"`
}

var leadingMentions = regexp.MustCompile(`^(?:@[A-Za-z0-9_]+\s+)+`)
var questionWords = regexp.MustCompile(`(?i)^(?:is|are|did|has|have|will|when|why|does|do|can)\b.*\breset\b`)
var uncertainWords = regexp.MustCompile(`(?i)\b(?:not|never|no|won['’]?t|haven['’]?t|hasn['’]?t|didn['’]?t|isn['’]?t|aren['’]?t|cannot|can['’]?t|maybe|might|could|should|if|unless|joke|false|rumou?r)\b`)
var deadlineOnly = regexp.MustCompile(`(?i)^by midnight (?:pst|pdt)[.!]?$`)
var nonQuotaReset = regexp.MustCompile(`(?i)\b(?:password|passcode|grok|cache|router)\b`)
var periodicReset = regexp.MustCompile(`(?i)\b(?:every|each)\s+(?:day|week|month)\b|\b(?:daily|weekly|monthly)\b`)
var chineseNegativeReset = regexp.MustCompile(`(?:不会|不會|没有|沒有|不是|并非|並非|尚未|还没|還沒|不|未).{0,12}重置`)
var reportedClaim = regexp.MustCompile(`(?i)\b(?:someone|according to|they said|he said|she said|reported|quoted)\b|转述|轉述|据说|據說|听说|聽說|有人说|有人說|别人说|別人說|引用|转发|轉發`)
var observedUnscopedReset = regexp.MustCompile(`(?i)\b(?:but no,?\s*it['’]s reset day today|but yes[\s.!…,]+that['’]s a reset)\b`)

func display(kind, label, summary, basis string) Preview {
	return Preview{Kind: kind, Label: label, Summary: summary, Basis: "rule_summary:" + basis}
}
func insufficient(summary, basis string) Preview {
	return display("insufficient", "信息不足", summary, basis)
}
func hasReset(text string) bool {
	return strings.Contains(text, "reset") || strings.Contains(text, "重置")
}

// PreviewFor is deterministic and side-effect free. It only weakens uncertain
// evidence; it cannot turn an unknown/related/signal record into a confirmation.
func PreviewFor(m Message) Preview {
	switch m.Status {
	case "withdrawn":
		return display("withdrawn", "已撤回", "这条内容已撤回，不再作为重置判断依据。", "status_withdrawn")
	case "not_in_latest_snapshot":
		return display("removed", "来源已移除", "这条内容已不在来源最新记录中，当前不据此判断重置。", "status_removed")
	case "expired":
		return display("expired", "已过期", "这条消息已过保留期，不作为当前重置依据。", "status_expired")
	case "active":
	default:
		return insufficient("消息状态不完整，暂不能判断是否重置。", "status_unknown")
	}
	if m.Author == "" {
		return insufficient("缺少作者信息，不能确认这是谁发布的消息。", "author_missing")
	}
	if !strings.EqualFold(m.Author, "thsottiaux") {
		return display("related", "他人内容", "这是他人的上下文内容，不代表作者发布了重置公告。", "other_author")
	}
	text := strings.TrimSpace(m.OriginalText)
	if text == "" {
		return insufficient("原文缺失，暂不能判断是否重置。", "text_missing")
	}
	if len(text) > 16384 || !utf8.ValidString(text) || strings.ContainsAny(text, "<>") {
		return insufficient("原文格式不完整，请核对来源后再判断。", "text_unusable")
	}
	for _, r := range text {
		if unicode.In(r, unicode.Cf) || unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' {
			return insufficient("原文格式不完整，请核对来源后再判断。", "text_unusable")
		}
	}
	if m.TextCompleteness != "full" {
		return insufficient("目前只有片段或上下文，无法确认完整含义。", "text_incomplete")
	}
	if m.PublishedAt == nil || *m.PublishedAt <= 0 {
		return display("insufficient", "时间不明", "缺少原帖时间，无法确认这条消息的适用时点。", "publication_time_missing")
	}
	lower := strings.ToLower(strings.Join(strings.Fields(text), " "))
	plain := strings.TrimSpace(leadingMentions.ReplaceAllString(lower, ""))
	plain = strings.TrimRight(plain, ".!。！")
	// These observed replies are not reset announcements despite the vocabulary.
	switch plain {
	case "not enough resets":
		return display("related", "普通互动", "作者在聊重置次数，未宣布 Codex 额度重置。", "interaction_reset_count")
	case "me too, me too", "me too", "same here":
		return display("related", "普通互动", "作者表示赞同，没有提供新的额度重置信息。", "interaction_agreement")
	}
	if strings.ContainsAny(text, "?？") || questionWords.MatchString(plain) {
		if hasReset(lower) {
			return display("question", "询问重置", "这是一条关于重置的问句，未确认已经重置。", "reset_question")
		}
		return display("related", "普通问答", "这是一条普通问答，没有明确的额度重置信息。", "ordinary_question")
	}
	if hasReset(lower) && nonQuotaReset.MatchString(lower) {
		return display("related", "其他重置", "这条内容涉及其他重置，不是 Codex 额度重置公告。", "nonquota_reset")
	}
	if hasReset(lower) && periodicReset.MatchString(lower) {
		return display("related", "周期说明", "描述的是周期性重置规则，不是一次新重置公告。", "periodic_reset")
	}
	if strings.Contains(lower, "correction") || strings.Contains(lower, "withdraw") || strings.Contains(text, "更正") || strings.Contains(text, "撤回") {
		return insufficient("原文涉及更正或撤回，不作为已重置的确认。", "correction_or_retraction")
	}
	if reportedClaim.MatchString(lower) || strings.ContainsAny(text, "\"“”「」『』«»‘") || strings.HasPrefix(plain, "'") && strings.HasSuffix(plain, "'") {
		return insufficient("原文包含引用或转述，不能当作作者确认重置。", "reported_or_quoted_claim")
	}
	// The 'no' in the observed Day17 wording answers an aside; it does not
	// negate 'reset day'. Preserve the mention without claiming quota scope.
	if observedUnscopedReset.MatchString(lower) {
		return display("insufficient", "提到重置", "原文提到重置，但不能据此确认额度范围或完成情况。", "observed_unscoped_reset_mention")
	}
	if uncertainWords.MatchString(lower) || chineseNegativeReset.MatchString(text) || strings.Contains(text, "没有") || strings.Contains(text, "尚未") || strings.Contains(text, "未重置") || strings.Contains(text, "可能") || strings.Contains(text, "假如") {
		if hasReset(lower) {
			return display("negative", "未确认重置", "原文含否定或不确定表达，不能判断为已重置。", "negative_or_uncertain")
		}
		return display("related", "普通动态", "这条动态没有明确说明额度已经重置。", "ordinary_uncertain")
	}
	if (m.Category == "notice" || m.Category == "announced" || m.Category == "signal") && m.Verification != "aggregated_source_quote" && m.Verification != "author_claim" && m.Verification != "third_party" {
		return insufficient("缺少可核对的来源依据，暂不能确认重置结论。", "source_basis_missing")
	}
	if deadlineOnly.MatchString(lower) {
		if m.Category == "signal" {
			return display("signal", "时间补充", "原文提到一个截止时间，但重置范围和完成情况仍未明确。", "contextual_deadline_signal")
		}
		if m.Category == "notice" {
			return display("notice", "重置预告", "作者补充了重置时间安排，仍不代表已经完成。", "contextual_deadline_notice")
		}
		return insufficient("原文只给出截止时间，缺少对应事项，无法判断是否重置。", "deadline_context_missing")
	}
	if strings.HasPrefix(lower, "it is all propagated") && (m.Category != "announced" && m.Category != "signal") {
		return insufficient("作者说相关内容已推送，但缺少明确的重置上下文。", "completion_context_missing")
	}
	switch m.Category {
	case "notice":
		return display("notice", "重置预告", "作者预告将重置 Codex 额度，具体安排以原文为准。", "category_notice")
	case "announced":
		return display("announced", "作者称已重置", "作者称 Codex 额度已重置；本机是否恢复仍看实际读数。", "category_announced")
	case "signal":
		if strings.Contains(lower, "today is a reset") {
			return display("signal", "重置未确认", "作者称今天有重置，但范围及完成情况未明确。", "day_reset_signal")
		}
		if strings.HasPrefix(lower, "it is all propagated") {
			return display("signal", "范围未明确", "作者称相关安排已推送，但重置范围仍未明确。", "contextual_completion_signal")
		}
		return display("signal", "重置未确认", "作者提到重置，但未明确额度范围或是否已经完成。", "category_signal")
	case "related":
		if hasReset(lower) {
			return display("insufficient", "提到重置", "原文提到了重置，但未提供明确的额度重置结论。", "unclassified_reset_mention")
		}
		return display("related", "普通动态", "这条动态没有提供明确的额度重置信息。", "category_related")
	default:
		if hasReset(lower) {
			return display("insufficient", "提到重置", "原文提到了重置，但未提供明确的额度重置结论。", "unclassified_reset_mention")
		}
		return insufficient("仅凭这条内容，无法判断是否发生额度重置。", "category_unknown")
	}
}
