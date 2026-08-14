package agent

import (
	"context"
	"encoding/json"
	"log/slog"
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

// SessionPersistence is an optional backend that keeps session history across
// restarts. *store.PostgresStore implements it.
type SessionPersistence interface {
	GetSession(ctx context.Context, sessionID string) ([]byte, error)
	SaveSession(ctx context.Context, sessionID string, messagesJSON []byte) error
	DeleteSession(ctx context.Context, sessionID string) error
}

// SessionStore manages per-session conversation history in memory, optionally
// mirroring to a persistent backend so sessions survive server restarts.
// It is safe for concurrent use.
type SessionStore struct {
	mu       sync.RWMutex
	sessions map[string]*sessionEntry
	persist  SessionPersistence // nil when persistence is disabled
	logger   *slog.Logger
}

// NewSessionStore creates an empty SessionStore and starts a background
// goroutine that evicts stale sessions every hour. persist is optional.
func NewSessionStore(persist SessionPersistence, logger *slog.Logger) *SessionStore {
	s := &SessionStore{
		sessions: make(map[string]*sessionEntry),
		persist:  persist,
		logger:   logger,
	}
	go s.evictLoop()
	return s
}

// Get returns the stored messages for sessionID, or nil if not found.
// On a memory miss with a persistent backend configured, it tries the backend
// and warms the in-memory cache.
func (s *SessionStore) Get(sessionID string) []openai.ChatCompletionMessage {
	s.mu.RLock()
	if e, ok := s.sessions[sessionID]; ok {
		defer s.mu.RUnlock()
		// Return a copy so callers cannot mutate the stored slice.
		out := make([]openai.ChatCompletionMessage, len(e.messages))
		copy(out, e.messages)
		return out
	}
	s.mu.RUnlock()

	if s.persist == nil {
		return nil
	}
	// Warm from persistence.
	raw, err := s.persist.GetSession(context.Background(), sessionID)
	if err != nil || len(raw) == 0 {
		return nil
	}
	var msgs []openai.ChatCompletionMessage
	if err := json.Unmarshal(raw, &msgs); err != nil {
		s.logger.Warn("failed to decode persisted session",
			slog.String("session_id", sessionID),
			slog.String("error", err.Error()),
		)
		return nil
	}
	s.mu.Lock()
	s.sessions[sessionID] = &sessionEntry{messages: msgs, updatedAt: time.Now()}
	s.mu.Unlock()

	out := make([]openai.ChatCompletionMessage, len(msgs))
	copy(out, msgs)
	return out
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
	s.mu.Unlock()

	// Mirror to persistence (best-effort, non-blocking on the critical path).
	if s.persist != nil {
		raw, err := json.Marshal(messages)
		if err != nil {
			s.logger.Warn("failed to encode session for persistence",
				slog.String("session_id", sessionID),
				slog.String("error", err.Error()),
			)
			return
		}
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := s.persist.SaveSession(ctx, sessionID, raw); err != nil {
				s.logger.Warn("failed to persist session",
					slog.String("session_id", sessionID),
					slog.String("error", err.Error()),
				)
			}
		}()
	}
}

// Delete removes a session from memory and the persistent backend.
func (s *SessionStore) Delete(sessionID string) {
	s.mu.Lock()
	delete(s.sessions, sessionID)
	s.mu.Unlock()

	if s.persist != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.persist.DeleteSession(ctx, sessionID); err != nil {
			s.logger.Warn("failed to delete persisted session",
				slog.String("session_id", sessionID),
				slog.String("error", err.Error()),
			)
		}
	}
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
