package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"strings"
)

var (
	// Dummy hash for timing attack protection (pre-computed SHA-256 hash)
	// Format: $sha256$salt$hash
	dummyHash = []byte("$sha256$0000000000000000000000000000000000000000000000000000000000000000$e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855")
)

// generateSalt generates a random 32-byte salt
func generateSalt() ([]byte, error) {
	salt := make([]byte, 32)
	if _, err := rand.Read(salt); err != nil {
		return nil, fmt.Errorf("failed to generate salt: %w", err)
	}
	return salt, nil
}

// hashPasswordWithSalt creates SHA-256 hash with salt
// Format: $sha256$<hex-salt>$<hex-hash>
func hashPasswordWithSalt(password, salt []byte) []byte {
	// Combine salt + password and hash
	h := sha256.New()
	h.Write(salt)
	h.Write(password)
	hash := h.Sum(nil)

	// Format: $sha256$salt$hash (all in hex)
	saltHex := hex.EncodeToString(salt)
	hashHex := hex.EncodeToString(hash)
	return []byte(fmt.Sprintf("$sha256$%s$%s", saltHex, hashHex))
}

// HashPassword creates a new password hash with random salt
func HashPassword(password []byte) ([]byte, error) {
	salt, err := generateSalt()
	if err != nil {
		return nil, err
	}
	return hashPasswordWithSalt(password, salt), nil
}

// VerifyCredentials verifies username and password against stored credentials
// Uses constant-time comparison to prevent timing attacks
func VerifyCredentials(username string, password []byte) error {
	// Lock-free read using atomic.Value - no type assertion overhead
	creds := getCredentials()
	expectedPasswordHash, ok := creds[username]

	// To prevent timing attacks, always perform hash comparison
	// even if username doesn't exist. Use the dummy hash.
	if !ok {
		// Use the dummy hash to ensure consistent timing
		verifyHash(dummyHash, password)
		return fmt.Errorf("invalid credentials")
	}

	// Verify the password hash
	if !verifyHash(expectedPasswordHash, password) {
		return fmt.Errorf("invalid credentials")
	}

	return nil
}

// VerifyPasswordHash verifies a password against a stored hash string.
// This is intended for other internal authentication use cases, such as
// the local Web admin password, while keeping the same hash format.
func VerifyPasswordHash(storedHash, password []byte) bool {
	return verifyHash(storedHash, password)
}

// verifyHash verifies password against stored hash
// Returns true if password matches, false otherwise
// Uses constant-time comparison to prevent timing attacks
func verifyHash(storedHash, password []byte) bool {
	// Parse stored hash: $sha256$salt$hash
	parts := strings.Split(string(storedHash), "$")
	if len(parts) != 4 || parts[0] != "" || parts[1] != "sha256" {
		// Invalid format, return false (constant time)
		return subtle.ConstantTimeCompare([]byte{0}, []byte{1}) == 1
	}

	saltHex := parts[2]
	expectedHashHex := parts[3]

	// Decode salt
	salt, err := hex.DecodeString(saltHex)
	if err != nil {
		return subtle.ConstantTimeCompare([]byte{0}, []byte{1}) == 1
	}

	// Compute hash with provided password
	h := sha256.New()
	h.Write(salt)
	h.Write(password)
	computedHash := h.Sum(nil)
	computedHashHex := hex.EncodeToString(computedHash)

	// Constant-time comparison
	return subtle.ConstantTimeCompare([]byte(computedHashHex), []byte(expectedHashHex)) == 1
}
