package responses

import "strings"

// Some compatible providers return reasoning_text in content without a summary.
// Prefer a nonempty summary and never combine it with the alternative text.
func reasoningItemText(item Item) string {
	var text strings.Builder
	for _, summary := range item.Summary {
		text.WriteString(summary.Text)
	}
	if text.Len() > 0 || item.Content == nil {
		return text.String()
	}
	for _, part := range item.Content.Items {
		if part.Type == "reasoning_text" && part.Text != nil {
			text.WriteString(*part.Text)
		}
	}
	return text.String()
}
