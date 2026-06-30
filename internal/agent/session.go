package agent

import (
	"sync"
	"time"

	openai "github.com/sashabaranov/go-openai"
)

const (
	maxHistoryMessages = 20            // 每个会话最多保留的消息条数
	sessionTTL         = 2 * time.Hour // 不活跃超过此时长的会话自动清理
	maxSessions        = 1000          // 最多并发活跃会话数；超出时淘汰最久未活跃的
)

// sessionEntry holds the conversation history for one session.
type sessionEntry struct {
	messages  []openai.ChatCompletionMessage
	updatedAt time.Time
}

// SessionStore manages per-session conversation history in memory.
// It is safe for concurrent use.
type SessionStore struct {
	mu       sync.RWMutex
	sessions map[string]*sessionEntry
}

// NewSessionStore creates an empty SessionStore and starts a background
// goroutine that evicts stale sessions every hour.
func NewSessionStore() *SessionStore {
	s := &SessionStore{
		sessions: make(map[string]*sessionEntry),
	}
	go s.evictLoop()
	return s
}

// Get returns the stored messages for sessionID, or nil if not found.
func (s *SessionStore) Get(sessionID string) []openai.ChatCompletionMessage {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if e, ok := s.sessions[sessionID]; ok {
		// Return a copy so callers cannot mutate the stored slice.
		out := make([]openai.ChatCompletionMessage, len(e.messages))
		copy(out, e.messages)
		return out
	}
	return nil
}

// Set replaces (or creates) the history for sessionID.
// If the slice exceeds maxHistoryMessages the oldest messages are dropped,
// always keeping the system message (index 0) intact.
func (s *SessionStore) Set(sessionID string, messages []openai.ChatCompletionMessage) {
	if len(messages) > maxHistoryMessages {
		// Keep system message + the most-recent (maxHistoryMessages-1) messages.
		tail := messages[len(messages)-(maxHistoryMessages-1):]
		trimmed := make([]openai.ChatCompletionMessage, 0, maxHistoryMessages)
		if len(messages) > 0 && messages[0].Role == openai.ChatMessageRoleSystem {
			trimmed = append(trimmed, messages[0])
		}
		trimmed = append(trimmed, tail...)
		messages = trimmed
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[sessionID] = &sessionEntry{
		messages:  messages,
		updatedAt: time.Now(),
	}
	// Evict the least-recently-active session when over capacity. Skip the
	// just-touched entry so we never immediately drop what we just stored.
	if len(s.sessions) > maxSessions {
		var oldestID string
		var oldestAt time.Time
		for id, e := range s.sessions {
			if id == sessionID {
				continue
			}
			if oldestID == "" || e.updatedAt.Before(oldestAt) {
				oldestID = id
				oldestAt = e.updatedAt
			}
		}
		if oldestID != "" {
			delete(s.sessions, oldestID)
		}
	}
}

// Delete removes a session.
func (s *SessionStore) Delete(sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, sessionID)
}

// evictLoop runs periodically and removes sessions that have not been
// updated within sessionTTL.
func (s *SessionStore) evictLoop() {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for range ticker.C {
		s.mu.Lock()
		cutoff := time.Now().Add(-sessionTTL)
		for id, e := range s.sessions {
			if e.updatedAt.Before(cutoff) {
				delete(s.sessions, id)
			}
		}
		s.mu.Unlock()
	}
}
