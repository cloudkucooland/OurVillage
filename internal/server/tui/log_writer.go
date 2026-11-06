package tui

import (
	"bytes"
	"strconv"
	"sync"
)

const (
	maxLogLineSize = 16 * 1024
	logChannelSize = 256
)

func NewLogChannel() chan string {
	return make(chan string, logChannelSize)
}

type LogWriter struct {
	mu         sync.Mutex
	pending    []byte
	logs       chan<- string
	dropped    int
	discarding bool
}

func NewLogWriter(logs chan<- string) *LogWriter {
	return &LogWriter{logs: logs}
}

func (w *LogWriter) Write(p []byte) (int, error) {
	inputLength := len(p)
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.discarding {
		newline := bytes.IndexByte(p, '\n')
		if newline < 0 {
			return inputLength, nil
		}
		p = p[newline+1:]
		w.discarding = false
	}
	w.pending = append(w.pending, p...)
	for {
		newline := bytes.IndexByte(w.pending, '\n')
		if newline < 0 {
			if len(w.pending) > maxLogLineSize {
				w.pending = append(append([]byte(nil), w.pending[:maxLogLineSize]...), []byte("... [truncated]")...)
				w.emit(string(w.pending))
				w.pending = nil
				w.discarding = true
			}
			break
		}
		line := bytes.TrimSuffix(w.pending[:newline], []byte{'\r'})
		if len(line) > maxLogLineSize {
			line = append(append([]byte(nil), line[:maxLogLineSize]...), []byte("... [truncated]")...)
		}
		w.emit(string(line))
		w.pending = w.pending[newline+1:]
	}
	return inputLength, nil
}

func (w *LogWriter) emit(line string) {
	if w.dropped > 0 {
		select {
		case w.logs <- formatDroppedLogs(w.dropped):
			w.dropped = 0
		default:
			w.dropped++
			return
		}
	}
	select {
	case w.logs <- line:
	default:
		w.dropped++
	}
}

func formatDroppedLogs(count int) string {
	return "[TUI] " + strconv.Itoa(count) + " log lines omitted because the display buffer was full"
}
