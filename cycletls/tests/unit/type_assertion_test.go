//go:build !integration

package unit

import (
	"testing"
)

// TestUncheckedTypeAssertion_Panics demonstrates that an unchecked type assertion
// panics when the interface holds an unexpected type. This is the bug that the
// comma-ok pattern fixes.
func TestUncheckedTypeAssertion_Panics(t *testing.T) {
	// Simulate the state.GetWebSocket pattern: returns interface{}
	var wsConnInterface interface{} = "not a WebSocketConnection"

	// The old code did: wsConn := wsConnInterface.(*WebSocketConnection)
	// which would panic. The fix uses comma-ok pattern.
	defer func() {
		if r := recover(); r != nil {
			t.Logf("Unchecked type assertion panicked as expected: %v", r)
		}
	}()

	// This simulates the bug - unchecked assertion on wrong type panics
	type FakeWSConn struct{ ID string }
	_ = wsConnInterface.(*FakeWSConn) // This will panic
	t.Error("Should have panicked on unchecked type assertion")
}

// TestCheckedTypeAssertion_Safe demonstrates the fix: comma-ok pattern
// gracefully handles wrong types without panicking.
func TestCheckedTypeAssertion_Safe(t *testing.T) {
	type FakeWSConn struct{ ID string }

	// Case 1: wrong type
	var wsConnInterface interface{} = "not a connection"
	conn, ok := wsConnInterface.(*FakeWSConn)
	if ok {
		t.Error("Should return ok=false for wrong type")
	}
	if conn != nil {
		t.Error("Should return nil for wrong type")
	}

	// Case 2: correct type
	expected := &FakeWSConn{ID: "test-123"}
	wsConnInterface = expected
	conn, ok = wsConnInterface.(*FakeWSConn)
	if !ok {
		t.Error("Should return ok=true for correct type")
	}
	if conn != expected {
		t.Error("Should return the same pointer")
	}
	if conn.ID != "test-123" {
		t.Errorf("ID mismatch: got %s, expected test-123", conn.ID)
	}

	// Case 3: nil interface
	wsConnInterface = nil
	conn, ok = wsConnInterface.(*FakeWSConn)
	if ok {
		t.Error("Should return ok=false for nil interface")
	}
	if conn != nil {
		t.Error("Should return nil for nil interface")
	}
}
