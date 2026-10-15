package gemini

import (
	"context"
	"time"
	"touchdict/internal/model"
)

// MeasureQuery uses the same dictionary request as a real lookup, without
// accepting cached answers or adding the sample word to user history.
func MeasureQuery(ctx context.Context, key, name string) (time.Duration, error) {
	client := New(key, name)
	client.skipHistory = true
	start := time.Now()
	_, err := client.Lookup(ctx, model.Selection{Text: "hello", BypassCache: true})
	return time.Since(start), err
}
