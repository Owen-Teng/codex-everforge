package resettext

import "strings"

// ContextKind identifies a bounded follow-up, never an independent reset claim.
// Callers must verify an explicit active same-author reply/quote reset chain.
func ContextKind(text string) string {
	text = strings.ToLower(Sanitize(text))
	if text == "" || strings.ContainsAny(text, "?？") || negative.MatchString(text) || reported.MatchString(text) {
		return ""
	}
	words := []string{}
	for _, word := range strings.Fields(text) {
		if !strings.HasPrefix(word, "https://") {
			words = append(words, word)
		}
	}
	text = strings.Join(words, " ")
	switch text {
	case "by midnight pst", "by midnight pst.", "lands by midnight pst", "lands by midnight pst.":
		return "deadline"
	case "it is all propagated", "it is all propagated.", "it is all propagated. enjoy some mad codexing tomorrow. fun week ahead too.",
		"and it's done", "and it's done.", "and it's done. see you tomorrow for some good news.",
		"and it’s done", "and it’s done.", "and it’s done. see you tomorrow for some good news.":
		return "completion"
	}
	return ""
}
