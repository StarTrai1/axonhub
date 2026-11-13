package gemini

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGeminiToolAdjacencyRequiresCompleteMatchingResults(t *testing.T) {
	for _, ids := range [][]string{{"a"}, {"a", "a"}, {"a", "foreign"}} {
		contents := []*Content{
			{Role: "model", Parts: []*Part{{FunctionCall: &FunctionCall{ID: "a", Name: "first"}}, {FunctionCall: &FunctionCall{ID: "b", Name: "second"}}}},
			{Role: "user", Parts: []*Part{{Text: "keep reminder"}}},
		}
		for _, id := range ids {
			contents = append(contents, &Content{Role: "user", Parts: []*Part{{FunctionResponse: &FunctionResponse{ID: id, Name: "result"}}}})
		}
		contents = append(contents, &Content{Role: "model", Parts: []*Part{{Text: "next turn"}}})
		require.Equal(t, contents, adjacentGeminiToolResponses(contents))
	}
}
