package orchestrator

import (
	"encoding/json"
	"strings"
	"unicode"
)

// Codex question replies include transport IDs and tool names. These are not
// user search terms: matching them recalls unrelated tool documentation and
// rewrites an otherwise reusable history prefix.
func unifiedHistoryQuery(item json.RawMessage) string {
	text := strings.TrimSpace(unifiedMessageText(item))
	const open = "<send_user_message_question_reply>"
	const close = "</send_user_message_question_reply>"
	if strings.HasPrefix(text, open) && strings.HasSuffix(text, close) {
		payload := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(text, open), close))
		var replies []struct {
			Question string `json:"question"`
			Answer string `json:"answer"`
		}
		if json.Unmarshal([]byte(payload), &replies) == nil && len(replies) > 0 {
			var parts []string
			for _, reply := range replies {
				parts = append(parts, reply.Question, reply.Answer)
			}
			text = strings.Join(parts, "\n")
		}
	}
	return strings.ToLower(text)
}

func unifiedHistoryTerms(query string) []string {
	var normalized strings.Builder
	var previousHan, previousWord bool
	for _, r := range strings.ToLower(query) {
		word := unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_'
		han := unicode.Is(unicode.Han, r)
		if !word || previousWord && han != previousHan {
			normalized.WriteByte(' ')
		}
		if word {
			normalized.WriteRune(r)
		}
		previousHan, previousWord = han, word
	}
	var terms []string
	seen := make(map[string]bool)
	for _, term := range strings.Fields(normalized.String()) {
		if seen[term] || len([]rune(term)) < 5 || len(term) > 96 || strings.Contains(" continue please following current previous conversation assistant content output input ", " "+term+" ") {
			continue
		}
		seen[term] = true
		terms = append(terms, term)
	}
	return terms
}

func unifiedHistorySegmentRelevance(query string, segments []unifiedHistorySegment) []bool {
	result := make([]bool, len(segments))
	terms := unifiedHistoryTerms(query)
	if len(terms) == 0 {
		return result
	}
	texts := make([]string, len(segments))
	for i, segment := range segments {
		var text strings.Builder
		for _, item := range segment.items {
			text.Write(item)
			text.WriteByte('\n')
		}
		texts[i] = strings.ToLower(text.String())
	}
	for _, term := range terms {
		var matches []int
		for i, text := range texts {
			if strings.Contains(text, term) {
				matches = append(matches, i)
			}
		}
		// A project/model name in most segments cannot distinguish relevant
		// evidence. Keep their existing summaries rather than expand arbitrary
		// early segments and invalidate the reusable prefix on every reply.
		if len(segments) >= 3 && len(matches)*2 >= len(segments) {
			continue
		}
		for _, i := range matches {
			result[i] = true
		}
	}
	return result
}
