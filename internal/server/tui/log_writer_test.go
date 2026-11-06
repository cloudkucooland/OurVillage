package tui

import (
	"strings"
	"testing"
)

func TestLogWriterSplitsAndTruncatesLines(t *testing.T) {
	logs := make(chan string, 3)
	writer := NewLogWriter(logs)

	if _, err := writer.Write([]byte("first")); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write([]byte(" line\r\nsecond\n")); err != nil {
		t.Fatal(err)
	}
	if got := <-logs; got != "first line" {
		t.Fatalf("first line = %q", got)
	}
	if got := <-logs; got != "second" {
		t.Fatalf("second line = %q", got)
	}

	longLine := strings.Repeat("x", maxLogLineSize+100) + "\n"
	if _, err := writer.Write([]byte(longLine)); err != nil {
		t.Fatal(err)
	}
	if got := <-logs; len(got) > maxLogLineSize+len("... [truncated]") || !strings.HasSuffix(got, "... [truncated]") {
		t.Fatalf("truncated line length/suffix invalid: len=%d", len(got))
	}
}

func TestLogWriterReportsDroppedLinesAndResynchronizesLongFragments(t *testing.T) {
	logs := make(chan string, 1)
	writer := NewLogWriter(logs)
	_, _ = writer.Write([]byte("queued\n"))
	_, _ = writer.Write([]byte("dropped\n"))
	if got := <-logs; got != "queued" {
		t.Fatalf("queued line = %q", got)
	}
	_, _ = writer.Write([]byte("accepted\n"))
	if got := <-logs; got != "[TUI] 1 log lines omitted because the display buffer was full" {
		t.Fatalf("drop report = %q", got)
	}

	drain := make(chan string, 1)
	longWriter := NewLogWriter(drain)
	_, _ = longWriter.Write([]byte(strings.Repeat("y", maxLogLineSize+1)))
	if got := <-drain; !strings.HasSuffix(got, "... [truncated]") {
		t.Fatalf("long fragment = %q", got)
	}
	_, _ = longWriter.Write([]byte("discarded remainder\nnext line\n"))
	if got := <-drain; got != "next line" {
		t.Fatalf("writer did not recover at line boundary: %q", got)
	}
}
