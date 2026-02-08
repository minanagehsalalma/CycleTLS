//go:build !integration

package unit

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type testSafeChannelWriter struct {
	ch     chan []byte
	mu     sync.RWMutex
	closed bool
}

func newTestSafeChannelWriter(ch chan []byte) *testSafeChannelWriter {
	return &testSafeChannelWriter{ch: ch, closed: false}
}

func (scw *testSafeChannelWriter) write(data []byte) bool {
	scw.mu.Lock()
	defer scw.mu.Unlock()
	if scw.closed {
		return false
	}
	select {
	case scw.ch <- data:
		return true
	default:
		return false
	}
}

func (scw *testSafeChannelWriter) setClosed() {
	scw.mu.Lock()
	defer scw.mu.Unlock()
	scw.closed = true
}

func TestSafeChannelWriter_ConcurrentWriteAndClose(t *testing.T) {
	const numWriters = 100
	const writesPerWriter = 50
	ch := make(chan []byte, numWriters*writesPerWriter)
	scw := newTestSafeChannelWriter(ch)
	var wg sync.WaitGroup
	var writeSuccesses atomic.Int64
	var writeFails atomic.Int64
	var panicked atomic.Int64
	for i := 0; i < numWriters; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					panicked.Add(1)
				}
			}()
			for j := 0; j < writesPerWriter; j++ {
				if scw.write([]byte{byte(id), byte(j)}) {
					writeSuccesses.Add(1)
				} else {
					writeFails.Add(1)
				}
			}
		}(i)
	}
	time.Sleep(1 * time.Millisecond)
	scw.setClosed()
	wg.Wait()
	if panicked.Load() > 0 {
		t.Errorf("write() panicked %d times", panicked.Load())
	}
	total := writeSuccesses.Load() + writeFails.Load()
	if total != int64(numWriters*writesPerWriter) {
		t.Errorf("Total = %d, expected %d", total, numWriters*writesPerWriter)
	}
}

func TestSafeChannelWriter_WriteAfterClose(t *testing.T) {
	ch := make(chan []byte, 10)
	scw := newTestSafeChannelWriter(ch)
	if !scw.write([]byte("hello")) {
		t.Error("write() should succeed before close")
	}
	scw.setClosed()
	if scw.write([]byte("world")) {
		t.Error("write() should return false after setClosed()")
	}
}

func TestSafeChannelWriter_WriteToFullChannel(t *testing.T) {
	ch := make(chan []byte, 1)
	scw := newTestSafeChannelWriter(ch)
	if !scw.write([]byte("first")) {
		t.Error("First write should succeed")
	}
	done := make(chan bool, 1)
	go func() {
		done <- scw.write([]byte("second"))
	}()
	select {
	case result := <-done:
		if result {
			t.Error("write() to full channel should return false")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("write() blocked on full channel")
	}
}

func TestSafeChannelWriter_NoRaceCondition(t *testing.T) {
	for i := 0; i < 100; i++ {
		ch := make(chan []byte, 1000)
		scw := newTestSafeChannelWriter(ch)
		var wg sync.WaitGroup
		wg.Add(11)
		for j := 0; j < 10; j++ {
			go func(id int) {
				defer wg.Done()
				for k := 0; k < 100; k++ {
					scw.write([]byte{byte(id), byte(k)})
				}
			}(j)
		}
		go func() {
			defer wg.Done()
			time.Sleep(time.Microsecond * 10)
			scw.setClosed()
		}()
		wg.Wait()
	}
}
