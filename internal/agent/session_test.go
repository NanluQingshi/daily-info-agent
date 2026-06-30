package agent

import (
	"fmt"
	"testing"

	openai "github.com/sashabaranov/go-openai"
)

func TestSessionStore_CapEvictsWhenExceeded(t *testing.T) {
	s := NewSessionStore()
	msg := []openai.ChatCompletionMessage{
		{Role: openai.ChatMessageRoleSystem, Content: "sys"},
		{Role: openai.ChatMessageRoleUser, Content: "hi"},
	}
	for i := 0; i < maxSessions+5; i++ {
		s.Set(fmt.Sprintf("sess-%d", i), msg)
	}
	s.mu.RLock()
	n := len(s.sessions)
	s.mu.RUnlock()
	if n != maxSessions {
		t.Fatalf("expected %d sessions after cap, got %d", maxSessions, n)
	}
	// The most recently added session must survive eviction.
	if s.Get(fmt.Sprintf("sess-%d", maxSessions+4)) == nil {
		t.Fatal("most-recent session was evicted; cap should drop the oldest instead")
	}
}
