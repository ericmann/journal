package cmd

import (
	"context"

	"github.com/ericmann/journal/internal/store"
	"github.com/ericmann/journal/internal/synth"
)

// answerQuery asks the model to answer query grounded only in the retrieved
// chunks, returning markdown. It reuses the synthesis client; the caller renders
// the result. Kept separate from runSearch so it's testable with a fake client.
func answerQuery(ctx context.Context, client synth.Client, model string, maxTokens int, query string, chunks []store.Chunk) (string, error) {
	resp, err := client.Complete(ctx, synth.Request{
		Model:     model,
		MaxTokens: maxTokens,
		Prompt:    synth.AssembleAnswer(query, chunks),
	})
	if err != nil {
		return "", err
	}
	return resp.Text, nil
}
