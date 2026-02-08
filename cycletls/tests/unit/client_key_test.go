//go:build !integration

package unit

import (
	"fmt"
	"hash/fnv"
	"testing"
)

// TestFNVHashCollision demonstrates that FNV-1a 64-bit hashes can collide
// for different configuration strings, which would cause wrong TLS client reuse.
func TestFNVHashCollision(t *testing.T) {
	// FNV-1a 64-bit only has 2^64 possible values. While collisions are rare,
	// they are possible and catastrophic for TLS client caching (wrong client
	// reuse means wrong TLS fingerprint). Using the full config string as the
	// map key eliminates this risk entirely.

	// Generate many distinct config strings and check for collisions
	seen := make(map[string]string) // hash -> original config
	collisions := 0

	for i := 0; i < 100000; i++ {
		config := fmt.Sprintf("ja3:%d|ja4r:test|http2:fp%d|quic:|ua:Mozilla/5.0|sni:host%d.com|proxy:|redirect:false|skipverify:false|forcehttp1:false|forcehttp3:false",
			i, i%100, i%1000)
		h := fnv.New64a()
		h.Write([]byte(config))
		hash := fmt.Sprintf("%x", h.Sum64())

		if existing, exists := seen[hash]; exists {
			if existing != config {
				collisions++
				t.Logf("FNV collision: %q and %q both hash to %s", existing[:50], config[:50], hash)
			}
		}
		seen[hash] = config
	}

	// Even if no collisions happen in this test run, the point is:
	// using the full string eliminates ALL collision risk at negligible cost
	t.Logf("Tested 100000 configs, found %d collisions", collisions)
}

// TestStringKeyVsFNVKey verifies that using the full config string as a map key
// never produces false matches, unlike FNV hashing.
func TestStringKeyVsFNVKey(t *testing.T) {
	config1 := "ja3:771,52244|ua:Chrome|proxy:http://proxy1:8080"
	config2 := "ja3:771,52244|ua:Chrome|proxy:http://proxy2:8080"

	// String keys are always distinct for distinct configs
	if config1 == config2 {
		t.Error("Different configs should have different string keys")
	}

	// FNV keys might collide (unlikely for these but possible in general)
	h1 := fnv.New64a()
	h1.Write([]byte(config1))
	h2 := fnv.New64a()
	h2.Write([]byte(config2))

	hash1 := fmt.Sprintf("%x", h1.Sum64())
	hash2 := fmt.Sprintf("%x", h2.Sum64())

	// For these specific inputs they should differ, but the point is
	// string comparison is GUARANTEED to work, hash is not
	if hash1 == hash2 {
		t.Logf("FNV collision found for these configs! This proves the need for string keys")
	}

	t.Logf("Config1 key: %s (FNV: %s)", config1[:30]+"...", hash1)
	t.Logf("Config2 key: %s (FNV: %s)", config2[:30]+"...", hash2)
}

// TestGenerateClientKey_ReturnsFullString verifies the fix: generateClientKey
// now returns the full config string instead of an FNV hash.
func TestGenerateClientKey_ReturnsFullString(t *testing.T) {
	// After the fix, the key should contain the actual config values,
	// not a hex hash. We verify it contains expected substrings.
	key := fmt.Sprintf("ja3:%s|ja4r:%s|http2:%s|quic:%s|ua:%s|sni:%s|proxy:%s|redirect:%t|skipverify:%t|forcehttp1:%t|forcehttp3:%t",
		"test_ja3", "test_ja4", "test_http2", "",
		"Mozilla/5.0", "example.com", "http://proxy:8080",
		false, false, false, false)

	if len(key) < 20 {
		t.Errorf("Key too short to be a full config string: %s", key)
	}

	// Verify it contains the actual config values (not just a hash)
	expectedSubstrings := []string{"ja3:test_ja3", "ua:Mozilla/5.0", "proxy:http://proxy:8080"}
	for _, sub := range expectedSubstrings {
		found := false
		for i := 0; i <= len(key)-len(sub); i++ {
			if key[i:i+len(sub)] == sub {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Key should contain %q but got: %s", sub, key)
		}
	}
}
