package resettext

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

func Sanitize(text string) string {
	if !utf8.ValidString(text) || len([]rune(text)) > 4096 || strings.ContainsAny(text, "<>\"`“”«»") || strings.HasPrefix(strings.TrimSpace(text), "RT @") {
		return ""
	}
	for _, r := range text {
		if unicode.In(r, unicode.Cf) || unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' {
			return ""
		}
	}
	return strings.Join(strings.Fields(text), " ")
}

var negative = regexp.MustCompile(`(?i)\b(not|never|no|won['’]?t|isn['’]?t|aren['’]?t|wasn['’]?t|weren['’]?t|haven['’]?t|hasn['’]?t|hadn['’]?t|didn['’]?t|don['’]?t|doesn['’]?t|can['’]?t|cannot|maybe|might|could|should|if|unless|assuming|hope|wish|rumou?r|joke|false|untrue|incorrect)\b|未|没有|不重置|不会|不會|还没|還沒|可能|假如|传言|傳言|并非|並非|不是|假消息|不实|不實|错误|錯誤`)
var quotaWords = regexp.MustCompile(`(?i)\b(limits?|quota|usage)\b|额度|額度|用量`)

const codexQuota = `codex\s+(?:(?:usage|rate)\s+)?(?:limits?|quota)`
const quotaForCodex = `(?:(?:usage|rate)\s+)?(?:limits?|quota)\s+(?:for|on)\s+codex`
const target = `(?:` + codexQuota + `|` + quotaForCodex + `)`

// Match a complete assertion, not a positive fragment inside a question,
// historical statement or narrower claim. Unknown modifiers remain unclassified.
const completedTimeOrScope = `(?:\s+(?:now|today|just now|for everyone|for all (?:users|accounts)))?`
const plannedTime = `(?:\s+(?:tomorrow|tonight|soon|later(?: today)?|today|now|in [1-9][0-9]* (?:minutes?|hours?)))?`

var announced = regexp.MustCompile(`(?i)^(?:(?:` + codexQuota + `\s+(?:(?:have|has) (?:now |just )?been reset|are (?:now )?reset)|(?:we|i) (?:have |just )?reset (?:all |the |your )?` + target + `|reset (?:is )?(?:done|complete|completed) (?:for )?` + target + `)` + completedTimeOrScope + `|codex\s*(?:额度|額度|用量)\s*(?:已重置|已经重置|已經重置|重置完成))$`)
var planned = regexp.MustCompile(`(?i)^(?:(?:` + codexQuota + `\s+(?:will|shall) (?:be )?reset|(?:we['’]ll|i['’]ll|we will|i will) reset (?:all |the |your )?` + target + `|(?:we['’]re|we are|i am) (?:resetting|going to reset) (?:all |the |your )?` + target + `|` + codexQuota + ` reset (?:is )?scheduled)` + plannedTime + `|codex\s*(?:额度|額度|用量)\s*(?:将重置|將重置|计划重置|計劃重置|预计重置|預計重置))$`)
var clauses = regexp.MustCompile(`[.!。！\n\r]+`)
var reported = regexp.MustCompile(`(?i)\b(said|says|claimed|heard|according|quoted|someone|wrote)\b`)
var futureWording = regexp.MustCompile(`(?i)\b(tomorrow|tonight|soon|later|next|scheduled)\b|明天|今晚|稍后|稍後|预计|預計`)

// These qualifiers can precede or follow the assertion in a separate clause.
// Failing closed avoids converting test/local/recurring events into a global notice.
var restrictedContext = regexp.MustCompile(`(?i)\b(test|tests|testing|staging|sandbox|demo|example|hypothetical|rehearsal|pretend|only|partial|partially|subset|individual|single|some|certain|limited|specific|selected|private|experimental|every|each|daily|weekly|monthly|previously|yesterday|last|historical|except)\b|\b(?:one|two|few|[0-9]+) (?:test )?(?:users?|accounts?|teams?|organizations?)\b|测试|測試|演练|演練|沙盒|部分|局部|单个|單個|仅|僅|每周|每週|每天|每月|上周|上週|上个月|上個月|去年|昨天|之前`)

// Narrow, observed series signature. This is only a scope-unknown signal;
// Parse still verifies the configured author before this classifier runs.
// It does not imply a Codex reset or attach a deadline from a separate reply.
var seriesResetSignal = regexp.MustCompile(`(?i)^day\s+[1-9][0-9]{0,2}\s*/\s*rest and reset[.!。！]?$`)

var dayResetSignal = regexp.MustCompile(`(?i)^day\s+[1-9][0-9]{0,2}\s*/\s*(?:wonder what happens the remaining seven days[.!]\s*)?(?:but\s+)?today is a reset[.!]?$`)
var dayPrefix = regexp.MustCompile(`(?i)^day\s+[1-9][0-9]{0,2}\s*/\s*`)
var resetWord = regexp.MustCompile(`(?i)\breset\b|重置`)

// A joke pivot is accepted only when it retracts a preceding non-reset claim.
// A joke after the reset assertion, or a joke about reset itself, stays rejected.
func independentDayReset(text string) bool {
	prefix := dayPrefix.FindStringIndex(text)
	if prefix == nil {
		return false
	}
	parts := clauses.Split(text[prefix[1]:], -1)
	claim := false
	for i, part := range parts {
		part = strings.ToLower(strings.TrimSpace(part))
		if part == "" {
			continue
		}
		if strings.HasPrefix(part, "just kidding, ") {
			if claim || i == 0 {
				return false
			}
			part = strings.TrimPrefix(part, "just kidding, ")
		} else if strings.Contains(part, "kidding") {
			return false
		}
		if part == "today is a reset" || part == "but today is a reset" {
			if claim {
				return false
			}
			claim = true
		} else if resetWord.MatchString(part) || quotaWords.MatchString(part) {
			return false
		}
	}
	return claim
}

func Classify(text string) string {
	text = Sanitize(text)
	if text == "" {
		return ""
	}
	if strings.ContainsAny(text, "?？") || negative.MatchString(text) || reported.MatchString(text) || restrictedContext.MatchString(text) {
		return ""
	}
	if seriesResetSignal.MatchString(text) || dayResetSignal.MatchString(text) || independentDayReset(text) {
		return "signal"
	}
	kind := ""
	for _, clause := range clauses.Split(text, -1) {
		clause = strings.TrimSpace(clause)
		if !strings.Contains(strings.ToLower(clause), "codex") || !quotaWords.MatchString(clause) {
			continue
		}
		current := ""
		if planned.MatchString(clause) {
			current = "notice"
		} else if announced.MatchString(clause) && !futureWording.MatchString(clause) {
			current = "announced"
		}
		if current != "" {
			if kind != "" && kind != current {
				return ""
			}
			kind = current
		}
	}
	return kind
}
