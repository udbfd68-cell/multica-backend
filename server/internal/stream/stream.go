package stream

import (
	"sync"
)

// Event represents a streaming event from agent execution.
type Event struct {
	Type    string `json:"type"`    // "text", "thinking", "tool-use", "done", "error"
	Content string `json:"content"` // text content
}

// Registry manages per-session SSE channels for streaming agent responses.
type Registry struct {
	mu      sync.RWMutex
	streams map[string][]chan Event // keyed by chat_session_id
}

// Global is the global stream registry instance.
var Global = &Registry{
	streams: make(map[string][]chan Event),
}

// Subscribe creates a new channel for streaming events for a session.
func (r *Registry) Subscribe(sessionID string) chan Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	ch := make(chan Event, 256)
	r.streams[sessionID] = append(r.streams[sessionID], ch)
	return ch
}

// Unsubscribe removes a channel from the session's subscribers.
func (r *Registry) Unsubscribe(sessionID string, ch chan Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	subs := r.streams[sessionID]
	for i, sub := range subs {
		if sub == ch {
			r.streams[sessionID] = append(subs[:i], subs[i+1:]...)
			break
		}
	}
	if len(r.streams[sessionID]) == 0 {
		delete(r.streams, sessionID)
	}
	close(ch)
}

// Broadcast sends an event to all subscribers for a session.
func (r *Registry) Broadcast(sessionID string, event Event) {
	r.mu.RLock()
	subs := r.streams[sessionID]
	r.mu.RUnlock()
	for _, ch := range subs {
		select {
		case ch <- event:
		default:
			// Drop if full — SSE client is too slow
		}
	}
}
