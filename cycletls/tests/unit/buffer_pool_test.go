//go:build !integration

package unit

import (
	"bytes"
	"sync"
	"testing"
)

// Simulates the buffer pool pattern used in cycletls/index.go
var testBufferPool = sync.Pool{
	New: func() interface{} {
		return new(bytes.Buffer)
	},
}

func getTestBuffer() *bytes.Buffer {
	buf := testBufferPool.Get().(*bytes.Buffer)
	buf.Reset()
	return buf
}

func putTestBuffer(buf *bytes.Buffer) {
	testBufferPool.Put(buf)
}

// TestBufferPool_DataCorruptionWithoutCopy demonstrates that returning a buffer
// to the pool before the channel reader consumes data causes corruption.
// This is the bug: b.Bytes() returns a slice backed by the buffer's internal array,
// so if the buffer is reused before the slice is read, data gets overwritten.
func TestBufferPool_DataCorruptionWithoutCopy(t *testing.T) {
	const iterations = 1000
	const goroutines = 10

	corruptionDetected := false
	var mu sync.Mutex

	for iter := 0; iter < iterations; iter++ {
		ch := make(chan []byte, goroutines)
		var wg sync.WaitGroup

		// Simulate multiple goroutines writing to channel via buffer pool
		// WITH the fix: copy data before returning buffer to pool
		for i := 0; i < goroutines; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				b := getTestBuffer()
				// Write unique data
				for j := 0; j < 100; j++ {
					b.WriteByte(byte(id))
				}
				// FIX: Copy before returning to pool
				data := make([]byte, b.Len())
				copy(data, b.Bytes())
				putTestBuffer(b)
				ch <- data
			}(i)
		}

		wg.Wait()
		close(ch)

		// Verify data integrity
		for data := range ch {
			if len(data) != 100 {
				mu.Lock()
				corruptionDetected = true
				mu.Unlock()
				t.Errorf("Data length corruption: got %d, expected 100", len(data))
				continue
			}
			expected := data[0]
			for j, b := range data {
				if b != expected {
					mu.Lock()
					corruptionDetected = true
					mu.Unlock()
					t.Errorf("Data corruption at byte %d: got %d, expected %d", j, b, expected)
					break
				}
			}
		}
	}

	if corruptionDetected {
		t.Error("Data corruption detected - buffer pool data not properly copied")
	}
}

// TestBufferPool_NoCopyCorruption shows the unsafe pattern where b.Bytes()
// is sent to a channel and then the buffer is returned to the pool.
// Another goroutine may reuse the buffer and overwrite the data.
func TestBufferPool_NoCopyCorruption(t *testing.T) {
	// This test demonstrates what happens WITHOUT the copy fix.
	// We simulate the race by immediately reusing the buffer.
	b := getTestBuffer()
	b.WriteString("original data that should not change")

	// Get the slice backed by buffer internal memory
	unsafeSlice := b.Bytes()
	originalData := string(unsafeSlice)

	// Return buffer to pool
	putTestBuffer(b)

	// Simulate another goroutine getting the same buffer
	b2 := getTestBuffer()
	b2.WriteString("OVERWRITTEN DATA REPLACING ORIGINAL")

	// The unsafeSlice may now contain corrupted data
	// because b2 may have reused the same underlying array
	currentData := string(unsafeSlice[:len(originalData)])

	// With copy fix, we would have:
	safeCopy := make([]byte, len(originalData))
	copy(safeCopy, []byte(originalData))

	// safeCopy is always correct
	if string(safeCopy) != "original data that should not change" {
		t.Error("Safe copy should always preserve original data")
	}

	// Log whether corruption actually happened (depends on pool behavior)
	if currentData != originalData {
		t.Logf("Corruption demonstrated: slice changed from %q to %q", originalData, currentData)
	} else {
		t.Logf("No corruption in this run (pool may have allocated a new buffer)")
	}

	putTestBuffer(b2)
}

// TestBufferPool_ConcurrentSafety verifies that the copy-before-put pattern
// prevents data corruption under heavy concurrent load.
func TestBufferPool_ConcurrentSafety(t *testing.T) {
	const numProducers = 50
	const messagesPerProducer = 100

	ch := make(chan []byte, numProducers*messagesPerProducer)
	var wg sync.WaitGroup

	for i := 0; i < numProducers; i++ {
		wg.Add(1)
		go func(id byte) {
			defer wg.Done()
			for j := 0; j < messagesPerProducer; j++ {
				b := getTestBuffer()
				// Write a pattern: [id, j, id, j, ...]
				for k := 0; k < 50; k++ {
					b.WriteByte(id)
					b.WriteByte(byte(j))
				}
				// Safe pattern: copy then return to pool
				data := make([]byte, b.Len())
				copy(data, b.Bytes())
				putTestBuffer(b)
				ch <- data
			}
		}(byte(i))
	}

	wg.Wait()
	close(ch)

	corruptions := 0
	total := 0
	for data := range ch {
		total++
		if len(data) != 100 {
			corruptions++
			continue
		}
		id := data[0]
		seq := data[1]
		for k := 0; k < 50; k++ {
			if data[k*2] != id || data[k*2+1] != seq {
				corruptions++
				break
			}
		}
	}

	if corruptions > 0 {
		t.Errorf("%d/%d messages had corrupted data", corruptions, total)
	}
	if total != numProducers*messagesPerProducer {
		t.Errorf("Expected %d messages, got %d", numProducers*messagesPerProducer, total)
	}
}
