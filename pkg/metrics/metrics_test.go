package metrics

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCounters_ZeroValues(t *testing.T) {
	assert.Equal(t, int64(0), App.ItemsFetched.Load())
	assert.Equal(t, int64(0), App.LLMCalls.Load())
	assert.Equal(t, int64(0), App.RunsCompleted.Load())
}

func TestCounters_AddAndLoad(t *testing.T) {
	App.ItemsFetched.Add(5)
	App.ItemsFetched.Add(3)
	assert.Equal(t, int64(8), App.ItemsFetched.Load())
}

func TestCounters_Independent(t *testing.T) {
	App.ItemsPublished.Add(2)
	App.PublishFailed.Add(1)
	assert.Equal(t, int64(2), App.ItemsPublished.Load())
	assert.Equal(t, int64(1), App.PublishFailed.Load())
}
