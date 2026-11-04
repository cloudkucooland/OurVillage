package spatial

import (
	"fmt"
	"sync"
	"testing"
)

func TestSensoryRingBuffer_CapacityAndEviction(t *testing.T) {
	buf := NewSensoryRingBuffer(3)
	if buf.Capacity() != 3 {
		t.Fatalf("expected capacity 3, got %d", buf.Capacity())
	}
	if buf.Len() != 0 {
		t.Fatalf("expected len 0, got %d", buf.Len())
	}

	buf.AddLog("CHAT", "10:00", "Message 1")
	buf.AddLog("CHAT", "10:01", "Message 2")
	buf.AddLog("CHAT", "10:02", "Message 3")

	if buf.Len() != 3 {
		t.Fatalf("expected len 3, got %d", buf.Len())
	}

	// 4th addition should evict Message 1
	buf.AddLog("CHAT", "10:03", "Message 4")
	if buf.Len() != 3 {
		t.Fatalf("expected len 3 after eviction, got %d", buf.Len())
	}

	recent := buf.Recent()
	if len(recent) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(recent))
	}
	if recent[0].Text != "Message 2" {
		t.Errorf("expected first entry to be Message 2, got %s", recent[0].Text)
	}
	if recent[1].Text != "Message 3" {
		t.Errorf("expected second entry to be Message 3, got %s", recent[1].Text)
	}
	if recent[2].Text != "Message 4" {
		t.Errorf("expected third entry to be Message 4, got %s", recent[2].Text)
	}

	logs := buf.FormatLogs()
	if logs[0] != "[10:01] Message 2" {
		t.Errorf("unexpected formatted log: %s", logs[0])
	}
}

func TestSensoryRingBuffer_Clear(t *testing.T) {
	buf := NewSensoryRingBuffer(5)
	buf.AddLog("SYS", "10:00", "Hello")
	buf.Clear()

	if buf.Len() != 0 {
		t.Fatalf("expected len 0 after clear, got %d", buf.Len())
	}
	if len(buf.Recent()) != 0 {
		t.Fatalf("expected empty slice from Recent(), got %v", buf.Recent())
	}
}

func TestSensoryRingBuffer_Concurrency(t *testing.T) {
	buf := NewSensoryRingBuffer(10)
	var wg sync.WaitGroup

	writers := 8
	writesPerWorker := 50

	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < writesPerWorker; i++ {
				buf.AddLog("SYS", fmt.Sprintf("10:%02d", i%60), fmt.Sprintf("worker %d line %d", id, i))
				_ = buf.Recent()
				_ = buf.FormatLogs()
			}
		}(w)
	}

	wg.Wait()

	if buf.Len() != 10 {
		t.Errorf("expected buffer to remain full at 10, got %d", buf.Len())
	}
}
