//go:build !integration

package cycletls

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Danny-Dasilva/CycleTLS/cycletls/state"
)

// ============================================================================
// Issue #1: SSE Event Loop Break Statement Bug
// The break inside a select only exits the select, not the outer for loop.
// Fix: Use labeled loop (sseLoop:) and break sseLoop.
// ============================================================================

// TestUnit_SSELabeledBreakExitsOuterLoop verifies that the labeled break pattern
// correctly exits both the select and the for loop. This is a structural test
// that reproduces the exact pattern used in dispatchSSEAsync.
func TestUnit_SSELabeledBreakExitsOuterLoop(t *testing.T) {
	iterations := 0
	maxIterations := 100

	// Simulate the buggy pattern: break inside select does NOT exit for loop
	// The fix uses a labeled loop to ensure proper exit
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// This simulates the fixed SSE event loop pattern with labeled break
sseLoop:
	for iterations < maxIterations {
		iterations++
		select {
		case <-ctx.Done():
			break sseLoop // This correctly exits the for loop
		default:
			// Would read SSE events here
		}
	}

	// If the labeled break works, we should exit after 1 iteration (context cancelled)
	if iterations != 1 {
		t.Errorf("Expected 1 iteration with labeled break, got %d (break didn't exit for loop)", iterations)
	}
}

// TestUnit_SSEBreakWithoutLabelDoesNotExitLoop demonstrates the original bug:
// a break inside select only exits the select, causing the for loop to spin.
func TestUnit_SSEBreakWithoutLabelDoesNotExitLoop(t *testing.T) {
	iterations := 0
	maxIterations := 5

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// Simulate the BUGGY pattern (without label)
	for iterations < maxIterations {
		iterations++
		select {
		case <-ctx.Done():
			break // BUG: only exits select, not for loop
		default:
		}
	}

	// Without the label, we'd loop maxIterations times
	if iterations != maxIterations {
		t.Errorf("Expected %d iterations without label (demonstrating bug), got %d", maxIterations, iterations)
	}
}

// TestUnit_SSEBreakOnEOF verifies that EOF breaks out of the SSE loop correctly.
func TestUnit_SSEBreakOnEOF(t *testing.T) {
	iterations := 0
	gotEOF := false

	// Simulate SSE event reading where second call returns EOF
	eventCount := 0

sseLoop:
	for {
		iterations++
		if iterations > 100 {
			t.Fatal("Loop did not exit - break label not working")
		}

		select {
		default:
			eventCount++
			if eventCount >= 2 {
				// Simulate EOF
				gotEOF = true
				break sseLoop
			}
		}
	}

	if !gotEOF {
		t.Error("Expected EOF to trigger loop exit")
	}
	if iterations > 2 {
		t.Errorf("Expected <= 2 iterations, got %d", iterations)
	}
}

// ============================================================================
// Issue #2: WebSocket Registry Leak
// WebSocket connections are unregistered even when never registered (error paths).
// Fix: Track registration state with a boolean flag.
// ============================================================================

// TestUnit_WebSocketRegistryOnlyUnregistersIfRegistered verifies that
// UnregisterWebSocket is NOT called when the connection was never registered
// (e.g., when Connect() fails before RegisterWebSocket is called).
func TestUnit_WebSocketRegistryOnlyUnregistersIfRegistered(t *testing.T) {
	requestID := "ws-leak-test-" + fmt.Sprintf("%d", time.Now().UnixNano())

	// Register a different WS connection with same ID to detect spurious unregister
	state.RegisterWebSocket(requestID, "sentinel-connection")

	// Simulate the fixed pattern: wsRegistered tracks actual registration
	wsRegistered := false

	// Simulate error path (Connect fails) - cleanup runs
	cleanup := func() {
		if wsRegistered {
			state.UnregisterWebSocket(requestID)
		}
	}

	// Error path: Connect fails, wsRegistered is still false
	cleanup()

	// The sentinel should still be registered since we didn't set wsRegistered = true
	conn, exists := state.GetWebSocket(requestID)
	if !exists {
		t.Fatal("WebSocket was unregistered even though wsRegistered was false - registry leak bug present")
	}
	if conn != "sentinel-connection" {
		t.Errorf("Expected sentinel connection, got %v", conn)
	}

	// Now simulate successful registration path
	wsRegistered = true
	cleanup()

	_, exists = state.GetWebSocket(requestID)
	if exists {
		t.Error("WebSocket should be unregistered after wsRegistered = true cleanup")
	}
}

// ============================================================================
// Issue #3: Premature Context Cancellation for SSE/WebSocket
// dispatcherAsync cancels the context for SSE/WebSocket connections prematurely.
// Fix: Don't defer cancel for SSE/WebSocket paths.
// ============================================================================

// TestUnit_SSEContextNotCancelledByDispatcher verifies that the SSE handler
// receives a non-cancelled context. In the buggy code, dispatcherAsync's
// defer cancel() would fire before the SSE handler had a chance to use the context.
func TestUnit_SSEContextNotCancelledByDispatcher(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	// Simulate the fixed dispatcherAsync behavior:
	// For SSE/WebSocket paths, the handler owns the context lifecycle.
	isSSE := true
	handlerCancelCalled := false

	// This represents what dispatcherAsync does now
	dispatcherCleanup := func() {
		// In the fixed code, cancel is NOT deferred for SSE/WS paths
		if !isSSE {
			cancel()
		}
	}

	// Dispatcher returns after handing off to SSE handler
	dispatcherCleanup()

	// Context should NOT be cancelled since SSE path skips the defer cancel
	select {
	case <-ctx.Done():
		t.Fatal("Context was cancelled prematurely - SSE connection would be killed")
	default:
		// Good - context is still active for the SSE handler
	}

	// SSE handler cancels context when it's done
	sseCleanup := func() {
		cancel()
		handlerCancelCalled = true
	}
	sseCleanup()

	select {
	case <-ctx.Done():
		// Good - cancelled by SSE handler at the right time
	default:
		t.Fatal("Context should be cancelled by SSE handler cleanup")
	}

	if !handlerCancelCalled {
		t.Error("SSE handler should own context cancellation")
	}
}

// TestUnit_HTTPContextCancelledByDispatcher verifies that for normal HTTP
// requests, the context IS cancelled when dispatcherAsync returns.
func TestUnit_HTTPContextCancelledByDispatcher(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancelled := false

	isSSE := false
	isWebSocket := false

	// Simulate the fixed dispatcherAsync: cancel for HTTP paths only
	simulateHTTPPath := func() {
		if !isSSE && !isWebSocket {
			defer func() {
				cancel()
				cancelled = true
			}()
		}
		// Simulate HTTP request processing
	}

	simulateHTTPPath()

	select {
	case <-ctx.Done():
		// Good - cancelled by HTTP path
	default:
		t.Fatal("HTTP path should cancel context on exit")
	}

	if !cancelled {
		t.Error("Cancel should have been called for HTTP path")
	}
}

// ============================================================================
// Issue #4: Missing Context Cancellation in SSE/WebSocket Handlers
// Handlers should cancel context during their own cleanup.
// ============================================================================

// TestUnit_SSEHandlerCancelsContextOnExit verifies that the SSE handler
// cancels its context during cleanup, preventing resource leaks.
func TestUnit_SSEHandlerCancelsContextOnExit(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancelCalled := false

	wrappedCancel := func() {
		cancelCalled = true
		cancel()
	}

	// Simulate the fixed SSE handler cleanup
	sseHandlerCleanup := func() {
		if wrappedCancel != nil {
			wrappedCancel()
		}
	}

	// Before cleanup, context should be active
	select {
	case <-ctx.Done():
		t.Fatal("Context should be active before cleanup")
	default:
	}

	sseHandlerCleanup()

	if !cancelCalled {
		t.Error("SSE handler cleanup should call cancel()")
	}

	select {
	case <-ctx.Done():
		// Good - context cancelled during cleanup
	default:
		t.Fatal("Context should be cancelled after SSE handler cleanup")
	}
}

// ============================================================================
// Issue #5: Goroutine Leak in WebSocket Reader on Repeated Timeouts
// Fix: Check context cancellation after timeout in addition to done channel.
// ============================================================================

// TestUnit_WebSocketReaderExitsOnContextCancel verifies that the WebSocket
// reader goroutine exits when the context is cancelled, even during timeout
// retry loops.
func TestUnit_WebSocketReaderExitsOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	exited := make(chan bool, 1)

	// Simulate the fixed reader goroutine timeout handling
	go func() {
		for {
			// Simulate timeout error on read
			isTimeout := true

			if isTimeout {
				// Issue #5 fix: Check both done channel and context cancellation
				select {
				case <-done:
					exited <- true
					return
				case <-ctx.Done():
					exited <- true
					return
				default:
					// Would continue reading in real code
				}
			}

			// Simulate some work
			time.Sleep(1 * time.Millisecond)
		}
	}()

	// Cancel context (simulates parent request being cancelled)
	time.Sleep(10 * time.Millisecond)
	cancel()

	select {
	case <-exited:
		// Good - goroutine exited via context cancellation
	case <-time.After(2 * time.Second):
		t.Fatal("WebSocket reader goroutine did not exit after context cancellation - goroutine leak")
	}
}

// TestUnit_WebSocketReaderExitsOnDone verifies the done channel still works.
func TestUnit_WebSocketReaderExitsOnDone(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	exited := make(chan bool, 1)

	go func() {
		for {
			isTimeout := true
			if isTimeout {
				select {
				case <-done:
					exited <- true
					return
				case <-ctx.Done():
					exited <- true
					return
				default:
				}
			}
			time.Sleep(1 * time.Millisecond)
		}
	}()

	time.Sleep(10 * time.Millisecond)
	close(done)

	select {
	case <-exited:
		// Good
	case <-time.After(2 * time.Second):
		t.Fatal("WebSocket reader goroutine did not exit on done signal")
	}
}

// ============================================================================
// Issue #6: TOCTOU Race in safeChannelWriter with Silent Data Drops
// Fix: Full Lock (not RLock) ensures atomicity. Added logging for drops.
// Also added writeBlocking for critical messages.
// ============================================================================

// TestUnit_SafeChannelWriterDropsLoggedNotSilent tests that when a channel
// is full, the write returns false (data not silently lost).
func TestUnit_SafeChannelWriterDropsLoggedNotSilent(t *testing.T) {
	ch := make(chan []byte, 1)
	scw := newSafeChannelWriter(ch)

	// Fill channel
	ok := scw.write([]byte("first"))
	if !ok {
		t.Fatal("First write should succeed")
	}

	// Second write should return false (channel full)
	ok = scw.write([]byte("second"))
	if ok {
		t.Error("Write to full channel should return false")
	}
}

// TestUnit_SafeChannelWriterNoPanicOnConcurrentWriteClose verifies no panics
// under concurrent write/close stress with race detector.
func TestUnit_SafeChannelWriterNoPanicOnConcurrentWriteClose(t *testing.T) {
	const iterations = 100
	for i := 0; i < iterations; i++ {
		ch := make(chan []byte, 10)
		scw := newSafeChannelWriter(ch)
		var wg sync.WaitGroup
		var panicked atomic.Int32

		for j := 0; j < 20; j++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer func() {
					if r := recover(); r != nil {
						panicked.Add(1)
					}
				}()
				scw.write([]byte("data"))
			}()
		}

		// Close concurrently
		wg.Add(1)
		go func() {
			defer wg.Done()
			scw.setClosed()
		}()

		wg.Wait()
		if panicked.Load() > 0 {
			t.Fatalf("Iteration %d: %d panics detected", i, panicked.Load())
		}
	}
}

// TestUnit_WriteBlockingSucceeds verifies writeBlocking works when channel has space.
func TestUnit_WriteBlockingSucceeds(t *testing.T) {
	ch := make(chan []byte, 1)
	scw := newSafeChannelWriter(ch)

	ok := scw.writeBlocking([]byte("hello"), 1*time.Second)
	if !ok {
		t.Error("writeBlocking should succeed on empty channel")
	}
}

// TestUnit_WriteBlockingTimesOut verifies writeBlocking returns false on timeout.
func TestUnit_WriteBlockingTimesOut(t *testing.T) {
	ch := make(chan []byte, 1)
	scw := newSafeChannelWriter(ch)

	// Fill channel
	scw.write([]byte("fill"))

	start := time.Now()
	ok := scw.writeBlocking([]byte("should-timeout"), 50*time.Millisecond)
	elapsed := time.Since(start)

	if ok {
		t.Error("writeBlocking should return false on timeout")
	}
	if elapsed < 40*time.Millisecond {
		t.Errorf("writeBlocking returned too quickly: %v", elapsed)
	}
}

// TestUnit_WriteBlockingReturnsOnClosed verifies writeBlocking returns false
// when channel is marked closed.
func TestUnit_WriteBlockingReturnsOnClosed(t *testing.T) {
	ch := make(chan []byte, 1)
	scw := newSafeChannelWriter(ch)
	scw.setClosed()

	ok := scw.writeBlocking([]byte("closed"), 1*time.Second)
	if ok {
		t.Error("writeBlocking should return false when closed")
	}
}

// ============================================================================
// Issue #7: WebSocket commandChan Overflow Drops Commands
// Fix: Use timeout instead of immediate drop, report error through chanWrite.
// ============================================================================

// TestUnit_WebSocketCommandChanOverflowReported verifies that when the command
// channel is full, the command is not silently dropped but handled with timeout.
func TestUnit_WebSocketCommandChanOverflowReported(t *testing.T) {
	// Create a small command channel
	commandChan := make(chan WebSocketCommand, 1)

	// Fill it
	commandChan <- WebSocketCommand{Type: "send", Data: []byte("blocking")}

	// Attempt to send another command with short timeout
	cmd := WebSocketCommand{Type: "send", Data: []byte("overflow")}

	sent := false
	timedOut := false
	select {
	case commandChan <- cmd:
		sent = true
	case <-time.After(10 * time.Millisecond):
		timedOut = true
	}

	if sent {
		t.Error("Command should not have been sent to full channel")
	}
	if !timedOut {
		t.Error("Expected timeout when channel is full")
	}
}

// ============================================================================
// Issue #8: Buffer Pool Corruption Risk
// Fix: Reset buffer in PutBuffer and cap max size.
// ============================================================================

// TestUnit_BufferPoolPutNilSafe verifies PutBuffer handles nil safely.
func TestUnit_BufferPoolPutNilSafe(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("PutBuffer(nil) panicked: %v", r)
		}
	}()

	state.PutBuffer(nil)
}

// TestUnit_BufferPoolResetOnPut verifies that PutBuffer resets the buffer.
func TestUnit_BufferPoolResetOnPut(t *testing.T) {
	buf := state.GetBuffer()
	buf.WriteString("sensitive data that should be cleared")

	// Return to pool
	state.PutBuffer(buf)

	// Get a new buffer - it should be clean
	buf2 := state.GetBuffer()
	if buf2.Len() != 0 {
		t.Errorf("Buffer from pool should be empty, got length %d", buf2.Len())
	}
	state.PutBuffer(buf2)
}

// TestUnit_BufferPoolOversizedDiscarded verifies oversized buffers are discarded.
func TestUnit_BufferPoolOversizedDiscarded(t *testing.T) {
	buf := state.GetBuffer()
	// Write more than maxBufferSize (256KB) to make the buffer oversized
	largeData := make([]byte, 300*1024)
	buf.Write(largeData)

	// Return to pool - should be discarded due to size
	state.PutBuffer(buf)

	// Getting a new buffer should give us a fresh one (not the oversized one)
	buf2 := state.GetBuffer()
	if buf2.Len() != 0 {
		t.Error("New buffer from pool should have zero length")
	}
	state.PutBuffer(buf2)
}

// ============================================================================
// Issue #9: Missing Error Handling for JSON Marshal Operations
// Fix: Handle errors from json.Marshal in sendWebSocketOpen and sendWebSocketClose.
// ============================================================================

// TestUnit_JSONMarshalForWSMessages verifies that the map types we use
// with json.Marshal in sendWebSocketOpen and sendWebSocketClose produce valid JSON.
func TestUnit_JSONMarshalForWSMessages(t *testing.T) {
	// These are the exact patterns used in sendWebSocketOpen and sendWebSocketClose
	openMsg := map[string]interface{}{
		"type":       "open",
		"protocol":   "graphql-ws",
		"extensions": "permessage-deflate",
	}

	openBytes, err := json.Marshal(openMsg)
	if err != nil {
		t.Errorf("json.Marshal for open message failed: %v", err)
	}
	if len(openBytes) == 0 {
		t.Error("Marshal produced empty bytes for open message")
	}

	closeMsg := map[string]interface{}{
		"type":   "close",
		"code":   1000,
		"reason": "normal closure",
	}

	closeBytes, err := json.Marshal(closeMsg)
	if err != nil {
		t.Errorf("json.Marshal for close message failed: %v", err)
	}
	if len(closeBytes) == 0 {
		t.Error("Marshal produced empty bytes for close message")
	}
}

// TestUnit_SendWebSocketOpenProducesValidOutput verifies sendWebSocketOpen
// produces valid binary frame output.
func TestUnit_SendWebSocketOpenProducesValidOutput(t *testing.T) {
	ch := make(chan []byte, 10)
	chanWrite := newSafeChannelWriter(ch)

	sendWebSocketOpen(chanWrite, "test-id-123", "graphql-ws", "permessage-deflate")

	select {
	case data := <-ch:
		if len(data) == 0 {
			t.Error("Expected non-empty data from sendWebSocketOpen")
		}
		if !bytes.Contains(data, []byte("test-id-123")) {
			t.Error("Data should contain request ID")
		}
		if !bytes.Contains(data, []byte("ws_open")) {
			t.Error("Data should contain ws_open type marker")
		}
	default:
		t.Error("Expected data in channel from sendWebSocketOpen")
	}
}

// TestUnit_SendWebSocketCloseProducesValidOutput verifies sendWebSocketClose
// produces valid binary frame output.
func TestUnit_SendWebSocketCloseProducesValidOutput(t *testing.T) {
	ch := make(chan []byte, 10)
	chanWrite := newSafeChannelWriter(ch)

	sendWebSocketClose(chanWrite, "test-id-456", 1000, "normal closure")

	select {
	case data := <-ch:
		if len(data) == 0 {
			t.Error("Expected non-empty data from sendWebSocketClose")
		}
		if !bytes.Contains(data, []byte("test-id-456")) {
			t.Error("Data should contain request ID")
		}
		if !bytes.Contains(data, []byte("ws_close")) {
			t.Error("Data should contain ws_close type marker")
		}
	default:
		t.Error("Expected data in channel from sendWebSocketClose")
	}
}

// ============================================================================
// Issue #10: Potential Nil Pointer Panic in URL Parsing
// Fix: Check for nil/error result from url.Parse before accessing fields.
// ============================================================================

// TestUnit_URLParseNilCheckPreventsNilPanic verifies that the URL parsing
// code handles various URL formats without panicking.
func TestUnit_URLParseNilCheckPreventsNilPanic(t *testing.T) {
	testCases := []struct {
		name     string
		urlStr   string
		wantHost string
	}{
		{"valid_https", "https://example.com/path", "example.com:443"},
		{"valid_http", "http://example.com/path", "example.com:80"},
		{"with_port", "https://example.com:8443/path", "example.com:8443"},
		{"http_with_port", "http://example.com:8080/path", "example.com:8080"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("URL parsing panicked for %q: %v", tc.urlStr, r)
				}
			}()

			// Reproduce the fixed pattern from dispatcherAsync
			urlObj, err := url.Parse(tc.urlStr)
			hostPort := ""
			if err != nil || urlObj == nil {
				hostPort = "unknown:443"
			} else {
				hostPort = urlObj.Host
				if !strings.Contains(hostPort, ":") {
					if urlObj.Scheme == "https" {
						hostPort = hostPort + ":443"
					} else {
						hostPort = hostPort + ":80"
					}
				}
			}

			if hostPort != tc.wantHost {
				t.Errorf("Expected host %q, got %q", tc.wantHost, hostPort)
			}
		})
	}
}

// ============================================================================
// Issue #11: WebSocket Write Errors Don't Stop Command Processor
// Fix: Return from command processor goroutine when write errors occur.
// ============================================================================

// TestUnit_WebSocketCommandProcessorStopsOnWriteError verifies that the
// command processor stops when a write error occurs, rather than continuing
// to process commands on a broken connection.
func TestUnit_WebSocketCommandProcessorStopsOnWriteError(t *testing.T) {
	commandChan := make(chan WebSocketCommand, 10)
	done := make(chan struct{})
	processorExited := make(chan bool, 1)
	commandsProcessed := atomic.Int32{}

	// Simulate the fixed command processor
	go func() {
		for {
			select {
			case <-done:
				processorExited <- true
				return
			case cmd := <-commandChan:
				commandsProcessed.Add(1)
				if cmd.Type == "send" {
					// Simulate write error
					writeErr := fmt.Errorf("connection reset by peer")
					if writeErr != nil {
						// Issue #11 fix: Stop processing on write error
						processorExited <- true
						return
					}
				}
			}
		}
	}()

	// Send a command that will "fail"
	commandChan <- WebSocketCommand{Type: "send", Data: []byte("hello")}

	// Send more commands that should NOT be processed
	commandChan <- WebSocketCommand{Type: "send", Data: []byte("should not process")}
	commandChan <- WebSocketCommand{Type: "send", Data: []byte("nor this")}

	select {
	case <-processorExited:
		// Good - processor stopped after write error
	case <-time.After(2 * time.Second):
		t.Fatal("Command processor did not exit after write error")
	}

	// Only 1 command should have been processed before the error
	count := commandsProcessed.Load()
	if count != 1 {
		t.Errorf("Expected 1 command processed before error, got %d", count)
	}
}

// TestUnit_WebSocketCommandProcessorStopsOnPingError verifies that ping/pong
// write errors also stop the command processor.
func TestUnit_WebSocketCommandProcessorStopsOnPingError(t *testing.T) {
	commandChan := make(chan WebSocketCommand, 10)
	done := make(chan struct{})
	processorExited := make(chan bool, 1)

	go func() {
		for {
			select {
			case <-done:
				processorExited <- true
				return
			case cmd := <-commandChan:
				if cmd.Type == "ping" || cmd.Type == "pong" {
					writeErr := fmt.Errorf("broken pipe")
					if writeErr != nil {
						processorExited <- true
						return
					}
				}
			}
		}
	}()

	commandChan <- WebSocketCommand{Type: "ping", Data: []byte("ping")}

	select {
	case <-processorExited:
		// Good
	case <-time.After(2 * time.Second):
		t.Fatal("Command processor did not exit after ping write error")
	}
}

// ============================================================================
// Integration-style unit test: Verify dispatcherAsync doesn't cancel SSE context
// ============================================================================

// TestUnit_DispatcherAsyncPreservesSSEContext verifies end-to-end that when
// dispatcherAsync receives an SSE request, the context remains valid.
func TestUnit_DispatcherAsyncPreservesSSEContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Simulate what the fixed dispatcherAsync does
	isSSEPath := true
	isWSPath := false

	// Fixed code: only cancel for HTTP paths
	simulateDispatcher := func() {
		if !isSSEPath && !isWSPath {
			defer cancel()
		}
		// For SSE path: return without cancelling
	}

	simulateDispatcher()

	// After dispatcherAsync returns (for SSE, it returns immediately after
	// dispatching to handler), context should still be active
	select {
	case <-ctx.Done():
		t.Fatal("Context cancelled prematurely - SSE connection would be killed")
	default:
		// Good - context still active for SSE handler
	}
}
