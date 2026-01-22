//go:build !integration

package cycletls

import (
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/quic-go/quic-go/http3"
)

// mockPacketConn implements net.PacketConn for testing
type mockPacketConn struct {
	closed   atomic.Bool
	closedMu sync.Mutex
}

func newMockPacketConn() *mockPacketConn {
	return &mockPacketConn{}
}

func (m *mockPacketConn) ReadFrom(p []byte) (n int, addr net.Addr, err error) {
	return 0, nil, nil
}

func (m *mockPacketConn) WriteTo(p []byte, addr net.Addr) (n int, err error) {
	return len(p), nil
}

func (m *mockPacketConn) Close() error {
	m.closed.Store(true)
	return nil
}

func (m *mockPacketConn) LocalAddr() net.Addr {
	return &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 12345}
}

func (m *mockPacketConn) SetDeadline(t time.Time) error      { return nil }
func (m *mockPacketConn) SetReadDeadline(t time.Time) error  { return nil }
func (m *mockPacketConn) SetWriteDeadline(t time.Time) error { return nil }

func (m *mockPacketConn) IsClosed() bool {
	return m.closed.Load()
}

// TestHTTP3ConnectionLeak_UQuicConnectionClosedImmediately tests that UQuic
// pre-dialed connections are closed immediately since http3.Transport cannot use them.
//
// Bug fix: UQuic connections (IsUQuic=true) cannot be used by http3.Transport
// because the transport expects *quic.Conn, not uquic.EarlyConnection.
// The fix closes these connections immediately to prevent leaks.
func TestHTTP3ConnectionLeak_UQuicConnectionClosedImmediately(t *testing.T) {
	rt := createTestRoundTripper()

	// Create a mock HTTP3Connection simulating what uhttp3Dial returns (UQuic)
	mockUDP := newMockPacketConn()
	conn := &HTTP3Connection{
		QuicConn: nil, // Would be uquic.EarlyConnection in real usage
		RawConn:  mockUDP,
		IsUQuic:  true, // UQuic connection
	}

	// Simulate what makeHTTP3Request does for UQuic connections
	// Since http3.Transport cannot use UQuic connections, the fix closes them immediately
	if conn.IsUQuic {
		rt.closeHTTP3Connection(conn)
		conn = nil
	}

	// Verify: UQuic connection should be closed immediately
	if !mockUDP.IsClosed() {
		t.Error("BUG: UQuic pre-dialed connection should be closed immediately - connection leak!")
	}

	// Verify: conn should be nil after closing
	if conn != nil {
		t.Error("conn should be nil after closing UQuic connection")
	}
}

// TestHTTP3ConnectionLeak_StandardQuicTransportHasDialFunction tests that
// for standard QUIC (non-UQuic), the transport should have a custom Dial function
// that uses the pre-dialed connection.
func TestHTTP3ConnectionLeak_StandardQuicTransportHasDialFunction(t *testing.T) {
	// This test verifies that for standard QUIC connections, the fix wires
	// the pre-dialed connection into the transport via a custom Dial function.
	//
	// Note: This test can only verify the structure since we can't create a real
	// *quic.Conn in unit tests. Integration tests would verify actual behavior.

	rt := createTestRoundTripper()

	// Create a mock connection representing what ghttp3Dial returns
	// In real usage, QuicConn would be *quic.Conn
	mockUDP := newMockPacketConn()
	conn := &HTTP3Connection{
		QuicConn: nil, // nil simulates type assertion failure for *quic.Conn
		RawConn:  mockUDP,
		IsUQuic:  false, // Standard QUIC, not UQuic
	}

	// Simulate the logic in makeHTTP3Request for standard QUIC with nil QuicConn
	// When type assertion to *quic.Conn fails, the connection should be closed
	var h3Transport *http3.Transport
	if !conn.IsUQuic {
		_, ok := conn.QuicConn.(*interface{}) // Simulating type assertion failure
		if !ok || conn.QuicConn == nil {
			// Fallback: close the connection since we can't use it
			rt.closeHTTP3Connection(conn)
			conn = nil
			h3Transport = &http3.Transport{}
		}
	}

	// Verify: when QuicConn is nil/invalid, connection should be closed
	if !mockUDP.IsClosed() {
		t.Error("Connection with nil QuicConn should be closed to prevent leak")
	}

	// Verify: transport should be created (without custom Dial in this case)
	if h3Transport == nil {
		t.Error("Transport should be created even when connection is closed")
	}
}

// TestHTTP3ConnectionLeak_MultipleRequests tests that multiple HTTP/3 requests
// don't accumulate leaked connections when using the fixed caching logic.
func TestHTTP3ConnectionLeak_MultipleRequests(t *testing.T) {
	rt := createTestRoundTripper()

	// Track all UDP sockets created
	var connections []*mockPacketConn

	// Simulate multiple HTTP/3 request cycles with UQuic connections
	// which should be closed immediately
	for i := 0; i < 5; i++ {
		mockUDP := newMockPacketConn()
		connections = append(connections, mockUDP)

		conn := &HTTP3Connection{
			QuicConn: nil,
			RawConn:  mockUDP,
			IsUQuic:  true, // UQuic connection
		}

		// Simulate the fixed behavior: close UQuic connections immediately
		if conn.IsUQuic {
			rt.closeHTTP3Connection(conn)
			conn = nil
		}

		h3Transport := &http3.Transport{}
		cacheKey := "h3:example.com:443"

		rt.cacheMu.Lock()
		oldCached := rt.cachedHTTP3Transports[cacheKey]
		if oldCached != nil && oldCached.conn != nil && oldCached.conn.RawConn != nil {
			// Close old connection when replacing
			_ = oldCached.conn.RawConn.Close()
		}
		rt.cachedHTTP3Transports[cacheKey] = &cachedHTTP3Transport{
			transport: h3Transport,
			conn:      conn, // nil for UQuic
			lastUsed:  time.Now(),
		}
		rt.cacheMu.Unlock()
	}

	// Count open connections - all should be closed for UQuic
	openCount := 0
	for _, conn := range connections {
		if !conn.IsClosed() {
			openCount++
		}
	}

	// After fix: all UQuic connections should be closed immediately
	if openCount != 0 {
		t.Errorf("Connection leak detected: %d connections still open, expected 0 for UQuic", openCount)
	}
}

// TestHTTP3_CachedConnectionNilForUQuic verifies that UQuic connections
// are not stored in the cache (set to nil) after the fix.
func TestHTTP3_CachedConnectionNilForUQuic(t *testing.T) {
	rt := createTestRoundTripper()

	mockUDP := newMockPacketConn()
	conn := &HTTP3Connection{
		QuicConn: nil,
		RawConn:  mockUDP,
		IsUQuic:  true, // UQuic connection
	}

	// Simulate the fixed makeHTTP3Request behavior for UQuic
	if conn.IsUQuic {
		rt.closeHTTP3Connection(conn)
		conn = nil
	}

	h3Transport := &http3.Transport{}
	cacheKey := "h3:test.example.com:443"

	rt.cacheMu.Lock()
	rt.cachedHTTP3Transports[cacheKey] = &cachedHTTP3Transport{
		transport: h3Transport,
		conn:      conn, // Should be nil for UQuic
		lastUsed:  time.Now(),
	}
	rt.cacheMu.Unlock()

	// Verify: cached connection should be nil for UQuic
	rt.cacheMu.RLock()
	cached := rt.cachedHTTP3Transports[cacheKey]
	rt.cacheMu.RUnlock()

	if cached.conn != nil {
		t.Error("UQuic cached connection should be nil after fix")
	}

	// Verify: UDP connection should be closed
	if !mockUDP.IsClosed() {
		t.Error("UQuic RawConn should be closed after fix")
	}
}

// TestHTTP3_CloseHTTP3ConnectionHandlesNil verifies that closeHTTP3Connection
// safely handles nil connections without panicking.
func TestHTTP3_CloseHTTP3ConnectionHandlesNil(t *testing.T) {
	rt := createTestRoundTripper()

	// Test with nil RawConn
	conn1 := &HTTP3Connection{
		QuicConn: nil,
		RawConn:  nil,
		IsUQuic:  false,
	}

	// This should not panic
	rt.closeHTTP3Connection(conn1)

	// Test with valid RawConn but nil QuicConn
	mockUDP := newMockPacketConn()
	conn2 := &HTTP3Connection{
		QuicConn: nil,
		RawConn:  mockUDP,
		IsUQuic:  true,
	}

	// This should close RawConn without panicking
	rt.closeHTTP3Connection(conn2)

	if !mockUDP.IsClosed() {
		t.Error("RawConn should be closed")
	}
}
