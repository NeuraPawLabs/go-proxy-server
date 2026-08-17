package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

const (
	adminArgon2idVersion    = 19
	adminArgon2idMemoryKiB  = 64 * 1024
	adminArgon2idIterations = 3
	adminArgon2idThreads    = 2
	adminArgon2idSaltLength = 16
	adminArgon2idKeyLength  = 32
)

// HashAdminPassword creates an Argon2id hash for the low-frequency Web admin login.
func HashAdminPassword(password []byte) ([]byte, error) {
	salt := make([]byte, adminArgon2idSaltLength)
	if _, err := rand.Read(salt); err != nil {
		return nil, fmt.Errorf("generate admin password salt: %w", err)
	}

	digest := argon2.IDKey(password, salt, adminArgon2idIterations, adminArgon2idMemoryKiB, adminArgon2idThreads, adminArgon2idKeyLength)
	return []byte(fmt.Sprintf(
		"$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		adminArgon2idVersion,
		adminArgon2idMemoryKiB,
		adminArgon2idIterations,
		adminArgon2idThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(digest),
	)), nil
}

// VerifyAdminPasswordHash accepts the current Argon2id format and the legacy
// proxy-password format so successful administrator logins can migrate it.
func VerifyAdminPasswordHash(storedHash, password []byte) (valid, needsUpgrade bool) {
	if strings.HasPrefix(string(storedHash), "$sha256$") {
		valid = verifyHash(storedHash, password)
		return valid, valid
	}

	parts := strings.Split(string(storedHash), "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" ||
		parts[2] != fmt.Sprintf("v=%d", adminArgon2idVersion) ||
		parts[3] != fmt.Sprintf("m=%d,t=%d,p=%d", adminArgon2idMemoryKiB, adminArgon2idIterations, adminArgon2idThreads) {
		return false, false
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) != adminArgon2idSaltLength {
		return false, false
	}
	expectedDigest, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(expectedDigest) != adminArgon2idKeyLength {
		return false, false
	}
	actualDigest := argon2.IDKey(password, salt, adminArgon2idIterations, adminArgon2idMemoryKiB, adminArgon2idThreads, adminArgon2idKeyLength)
	return subtle.ConstantTimeCompare(actualDigest, expectedDigest) == 1, false
}
