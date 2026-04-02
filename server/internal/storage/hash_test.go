package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

// TestHashConsistency_NewVsSum 验证 sha256.New().Write() 和 sha256.Sum256() 产生相同结果
func TestHashConsistency_NewVsSum(t *testing.T) {
	testCases := []string{
		"<html><body>simple page</body></html>",
		"",
		strings.Repeat("a", 10*1024*1024), // 10MB
		"<html><body>中文内容 日本語 한국어</body></html>",
		"<html>\x00\x01\x02binary data</html>",
	}

	for i, input := range testCases {
		// 旧方式：sha256.Sum256
		sumResult := sha256.Sum256([]byte(input))
		hashOld := hex.EncodeToString(sumResult[:])

		// 新方式：sha256.New() + Write
		hasher := sha256.New()
		hasher.Write([]byte(input))
		hashNew := hex.EncodeToString(hasher.Sum(nil))

		if hashOld != hashNew {
			t.Errorf("case %d: hash mismatch\n  sha256.Sum256:  %s\n  sha256.New():   %s", i, hashOld, hashNew)
		}
	}
}

// TestHashConsistency_Deterministic 验证相同输入多次哈希结果一致
func TestHashConsistency_Deterministic(t *testing.T) {
	input := "<html><body>Test content for hash determinism</body></html>"

	var hashes [10]string
	for i := 0; i < 10; i++ {
		hasher := sha256.New()
		hasher.Write([]byte(input))
		hashes[i] = hex.EncodeToString(hasher.Sum(nil))
	}

	for i := 1; i < 10; i++ {
		if hashes[i] != hashes[0] {
			t.Errorf("hash %d differs: %s vs %s", i, hashes[i], hashes[0])
		}
	}
}

// TestHashConsistency_DifferentInputs 验证不同输入产生不同哈希
func TestHashConsistency_DifferentInputs(t *testing.T) {
	inputs := []string{
		"<html><body>Version 1</body></html>",
		"<html><body>Version 2</body></html>",
		"<html><body>Version 1</body></html> ", // 多一个空格
	}

	hashes := make(map[string]string)
	for _, input := range inputs {
		hasher := sha256.New()
		hasher.Write([]byte(input))
		hash := hex.EncodeToString(hasher.Sum(nil))

		if prev, exists := hashes[hash]; exists {
			t.Errorf("hash collision: %q and %q both produce %s", prev, input, hash)
		}
		hashes[hash] = input
	}
}
