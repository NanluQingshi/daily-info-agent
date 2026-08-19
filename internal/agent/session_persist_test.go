package agent

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	openai "github.com/sashabaranov/go-openai"
)

// mockPersistence is an in-memory SessionPersistence for tests.
type mockPersistence struct {
	mu   sync.Mutex
	data map[string][]byte
	err  error
}

func newMockPersistence() *mockPersistence {
	return &mockPersistence{data: make(map[string][]byte)}
}

func (m *mockPersistence) GetSession(_ context.Context, id string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return nil, m.err
	}
	raw, ok := m.data[id]
	if !ok {
		return nil, nil
	}
	return raw, nil
}

func (m *mockPersistence) SaveSession(_ context.Context, id string, raw []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return m.err
	}
	m.data[id] = raw
	return nil
}

func (m *mockPersistence) DeleteSession(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.data, id)
	return nil
}

func TestSessionStore_PersistAndWarm(t *testing.T) {
	p := newMockPersistence()
	s := NewSessionStore(p, slog.Default())

	msgs := []openai.ChatCompletionMessage{
		{Role: openai.ChatMessageRoleSystem, Content: "sys"},
		{Role: openai.ChatMessageRoleUser, Content: "hi"},
	}
	s.Set("sess-1", msgs)

	// Persistence is mirrored asynchronously — wait until the backend has it.
	require.Eventually(t, func() bool {
		raw, err := p.GetSession(context.Background(), "sess-1")
		return err == nil && len(raw) > 0
	}, 2*time.Second, 10*time.Millisecond)

	// Simulate a restart: fresh store with the same backend.
	s2 := NewSessionStore(p, slog.Default())
	got := s2.Get("sess-1")
	require.Len(t, got, 2)
	assert.Equal(t, "hi", got[1].Content)
}

func TestSessionStore_DeleteRemovesFromPersistence(t *testing.T) {
	p := newMockPersistence()
	s := NewSessionStore(p, slog.Default())

	s.Set("sess-2", []openai.ChatCompletionMessage{{Role: openai.ChatMessageRoleUser, Content: "x"}})
	// Wait for the async mirror to land, then delete.
	require.Eventually(t, func() bool {
		raw, err := p.GetSession(context.Background(), "sess-2")
		return err == nil && len(raw) > 0
	}, 2*time.Second, 10*time.Millisecond)
	s.Delete("sess-2")

	// Fresh store must not find it.
	s2 := NewSessionStore(p, slog.Default())
	assert.Nil(t, s2.Get("sess-2"))
}

func TestSessionStore_WithoutPersistence_NoWarm(t *testing.T) {
	s := NewSessionStore(nil, slog.Default())
	s.Set("sess-3", []openai.ChatCompletionMessage{{Role: openai.ChatMessageRoleUser, Content: "x"}})

	// Fresh memory-only store has no data.
	s2 := NewSessionStore(nil, slog.Default())
	assert.Nil(t, s2.Get("sess-3"))
}
