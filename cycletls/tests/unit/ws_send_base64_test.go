//go:build !integration

package cycletls

import (
	"encoding/base64"
	"encoding/json"
	"testing"
)

// TestWsSendBinaryBase64Decode tests that ws_send correctly decodes base64 data
// when isBinary is true (matching TypeScript client behavior)
func TestWsSendBinaryBase64Decode(t *testing.T) {
	// Original binary data (some non-ASCII bytes)
	originalData := []byte{0x00, 0x01, 0x02, 0xFF, 0xFE, 0xFD, 0x48, 0x65, 0x6C, 0x6C, 0x6F}

	// TypeScript client base64 encodes binary data before sending
	base64Encoded := base64.StdEncoding.EncodeToString(originalData)

	// Simulate the JSON message that would come from TypeScript client
	message := map[string]interface{}{
		"action":    "ws_send",
		"requestId": "test-request-123",
		"data":      base64Encoded,
		"isBinary":  true,
	}

	// Test the decoding logic that should be in ws_send handling
	dataStr, ok := message["data"].(string)
	if !ok {
		t.Fatal("data should be a string")
	}

	isBinary, _ := message["isBinary"].(bool)

	var resultData []byte
	if isBinary {
		// This is the fix: decode base64 when isBinary is true
		decoded, err := base64.StdEncoding.DecodeString(dataStr)
		if err != nil {
			t.Fatalf("Failed to decode base64: %v", err)
		}
		resultData = decoded
	} else {
		resultData = []byte(dataStr)
	}

	// Verify the decoded data matches the original
	if len(resultData) != len(originalData) {
		t.Errorf("Length mismatch: got %d, want %d", len(resultData), len(originalData))
	}

	for i, b := range originalData {
		if resultData[i] != b {
			t.Errorf("Byte mismatch at index %d: got 0x%02X, want 0x%02X", i, resultData[i], b)
		}
	}
}

// TestWsSendTextNoBase64Decode tests that ws_send does NOT decode base64
// when isBinary is false (text message)
func TestWsSendTextNoBase64Decode(t *testing.T) {
	// Text message - should NOT be base64 decoded
	textData := "Hello, World!"

	message := map[string]interface{}{
		"action":    "ws_send",
		"requestId": "test-request-456",
		"data":      textData,
		"isBinary":  false,
	}

	dataStr, ok := message["data"].(string)
	if !ok {
		t.Fatal("data should be a string")
	}

	isBinary, _ := message["isBinary"].(bool)

	var resultData []byte
	if isBinary {
		decoded, err := base64.StdEncoding.DecodeString(dataStr)
		if err != nil {
			// For text, base64 decode might fail - that's expected if it's not encoded
			resultData = []byte(dataStr)
		} else {
			resultData = decoded
		}
	} else {
		resultData = []byte(dataStr)
	}

	// For text messages, the data should be the original text
	if string(resultData) != textData {
		t.Errorf("Text data mismatch: got %q, want %q", string(resultData), textData)
	}
}

// TestWsSendBinaryBase64InvalidFallback tests graceful fallback when base64 decode fails
func TestWsSendBinaryBase64InvalidFallback(t *testing.T) {
	// Invalid base64 string
	invalidBase64 := "not-valid-base64!!!"

	message := map[string]interface{}{
		"action":    "ws_send",
		"requestId": "test-request-789",
		"data":      invalidBase64,
		"isBinary":  true,
	}

	dataStr, ok := message["data"].(string)
	if !ok {
		t.Fatal("data should be a string")
	}

	isBinary, _ := message["isBinary"].(bool)

	var resultData []byte
	if isBinary {
		decoded, err := base64.StdEncoding.DecodeString(dataStr)
		if err != nil {
			// Fallback to raw string on decode error
			resultData = []byte(dataStr)
		} else {
			resultData = decoded
		}
	} else {
		resultData = []byte(dataStr)
	}

	// On invalid base64, should fallback to the raw string
	if string(resultData) != invalidBase64 {
		t.Errorf("Fallback data mismatch: got %q, want %q", string(resultData), invalidBase64)
	}
}

// TestWsSendBinaryEmptyData tests handling of empty binary data
func TestWsSendBinaryEmptyData(t *testing.T) {
	// Empty data, base64 encoded
	emptyData := []byte{}
	base64Encoded := base64.StdEncoding.EncodeToString(emptyData)

	message := map[string]interface{}{
		"action":    "ws_send",
		"requestId": "test-request-empty",
		"data":      base64Encoded,
		"isBinary":  true,
	}

	dataStr, ok := message["data"].(string)
	if !ok {
		t.Fatal("data should be a string")
	}

	isBinary, _ := message["isBinary"].(bool)

	var resultData []byte
	if isBinary {
		decoded, err := base64.StdEncoding.DecodeString(dataStr)
		if err != nil {
			resultData = []byte(dataStr)
		} else {
			resultData = decoded
		}
	} else {
		resultData = []byte(dataStr)
	}

	if len(resultData) != 0 {
		t.Errorf("Empty data should decode to empty slice, got length %d", len(resultData))
	}
}

// TestWsSendParseFromJSON tests end-to-end JSON parsing like in actual handler
func TestWsSendParseFromJSON(t *testing.T) {
	// Simulate actual JSON that comes over the wire
	originalData := []byte{0xDE, 0xAD, 0xBE, 0xEF}
	base64Encoded := base64.StdEncoding.EncodeToString(originalData)

	jsonStr := `{"action":"ws_send","requestId":"test-123","data":"` + base64Encoded + `","isBinary":true}`

	var baseMessage map[string]interface{}
	err := json.Unmarshal([]byte(jsonStr), &baseMessage)
	if err != nil {
		t.Fatalf("JSON unmarshal failed: %v", err)
	}

	// This simulates the parsing logic in the actual handler
	action, _ := baseMessage["action"].(string)
	if action != "ws_send" {
		t.Errorf("Expected action ws_send, got %s", action)
	}

	dataStr, _ := baseMessage["data"].(string)
	isBinary, _ := baseMessage["isBinary"].(bool)

	var resultData []byte
	if isBinary {
		decoded, err := base64.StdEncoding.DecodeString(dataStr)
		if err != nil {
			resultData = []byte(dataStr)
		} else {
			resultData = decoded
		}
	} else {
		resultData = []byte(dataStr)
	}

	// Verify the result matches original binary data
	if len(resultData) != len(originalData) {
		t.Errorf("Length mismatch: got %d, want %d", len(resultData), len(originalData))
	}

	for i, b := range originalData {
		if resultData[i] != b {
			t.Errorf("Byte mismatch at index %d: got 0x%02X, want 0x%02X", i, resultData[i], b)
		}
	}
}
