package responses

// These reasons are explicit turn termination decisions, not an accidentally
// empty generation. In particular, max_output_tokens and unknown reasons must
// retain the ordinary empty-response retry behavior.
func isIntentionalIncomplete(details *ResponseIncompleteDetails) bool {
	if details == nil {
		return false
	}
	switch details.Reason {
	case "interrupted", "steered", "content_filter", "max_messages":
		return true
	default:
		return false
	}
}
