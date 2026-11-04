package spatial

import (
	"fmt"
	"sync"
	"time"
)

// SensoryEntry represents a discrete sensory event perceived by the agent.
type SensoryEntry struct {
	Timestamp  string    `json:"timestamp"`
	ReceivedAt time.Time `json:"received_at"`
	Source     string    `json:"source"` // "CHAT", "WHISPER", "SYSTEM", "TIME", "DIRECT"
	Sender     string    `json:"sender,omitempty"`
	Text       string    `json:"text"`
}

// Format returns the canonical context log format e.g. "[10:05] SYSTEM: ..."
func (e SensoryEntry) Format() string {
	ts := e.Timestamp
	if ts == "" {
		ts = e.ReceivedAt.Format("15:04")
	}
	return fmt.Sprintf("[%s] %s", ts, e.Text)
}

// SensoryRingBuffer is a concurrent-safe, fixed-capacity sliding buffer for sensory stream events.
type SensoryRingBuffer struct {
	mu       sync.RWMutex
	capacity int
	entries  []SensoryEntry
	start    int
	count    int
}

// NewSensoryRingBuffer creates a ring buffer with the given capacity (defaults to 12 if <= 0).
func NewSensoryRingBuffer(capacity int) *SensoryRingBuffer {
	if capacity <= 0 {
		capacity = 12
	}
	return &SensoryRingBuffer{
		capacity: capacity,
		entries:  make([]SensoryEntry, capacity),
	}
}

// Add appends a new sensory event, evicting the oldest entry when capacity is reached.
func (b *SensoryRingBuffer) Add(entry SensoryEntry) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if entry.ReceivedAt.IsZero() {
		entry.ReceivedAt = time.Now()
	}

	if b.count < b.capacity {
		idx := (b.start + b.count) % b.capacity
		b.entries[idx] = entry
		b.count++
	} else {
		// Evict oldest by advancing start pointer
		b.entries[b.start] = entry
		b.start = (b.start + 1) % b.capacity
	}
}

// AddLog is a convenience helper for adding a text log with optional timestamp.
func (b *SensoryRingBuffer) AddLog(source string, timestamp string, text string) {
	b.Add(SensoryEntry{
		Timestamp:  timestamp,
		ReceivedAt: time.Now(),
		Source:     source,
		Text:       text,
	})
}

// Recent returns all entries currently in the buffer ordered from oldest to newest.
func (b *SensoryRingBuffer) Recent() []SensoryEntry {
	b.mu.RLock()
	defer b.mu.RUnlock()

	result := make([]SensoryEntry, b.count)
	for i := 0; i < b.count; i++ {
		idx := (b.start + i) % b.capacity
		result[i] = b.entries[idx]
	}
	return result
}

// FormatLogs returns formatted strings for all recent entries ready for prompt assembly.
func (b *SensoryRingBuffer) FormatLogs() []string {
	entries := b.Recent()
	lines := make([]string, len(entries))
	for i, e := range entries {
		lines[i] = e.Format()
	}
	return lines
}

// Len returns the number of active entries currently stored.
func (b *SensoryRingBuffer) Len() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.count
}

// Capacity returns the maximum buffer capacity.
func (b *SensoryRingBuffer) Capacity() int {
	return b.capacity
}

// Clear resets the buffer state.
func (b *SensoryRingBuffer) Clear() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.start = 0
	b.count = 0
}
