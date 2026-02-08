package cycletls

import (
	"testing"
)

func TestReadBytesExceedsMaxLimit(t *testing.T) {
	// Create a small buffer - the size check happens before bounds check
	r := NewReader([]byte{1, 2, 3})

	// Request more than MaxReadBytes (10MB)
	_, err := r.ReadBytes(MaxReadBytes + 1)
	if err != ErrBytesTooLarge {
		t.Fatalf("Expected ErrBytesTooLarge, got %v", err)
	}
}

func TestReadBytesAtMaxLimit(t *testing.T) {
	// Create a reader with a small buffer
	// Reading exactly MaxReadBytes should fail with EOF (not size error)
	// since our buffer is smaller than MaxReadBytes
	r := NewReader([]byte{1, 2, 3})

	_, err := r.ReadBytes(MaxReadBytes)
	if err == ErrBytesTooLarge {
		t.Fatalf("MaxReadBytes should be allowed, got ErrBytesTooLarge")
	}
	// Should get EOF since buffer is too small
	if err == nil {
		t.Fatalf("Expected an error for buffer too small, got nil")
	}
}

func TestReadBytesMaxLimitConstant(t *testing.T) {
	// Verify the constant is 10MB
	expected := 10 * 1024 * 1024
	if MaxReadBytes != expected {
		t.Fatalf("Expected MaxReadBytes to be %d (10MB), got %d", expected, MaxReadBytes)
	}
}
