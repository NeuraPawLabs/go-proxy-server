package auth

import (
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	"gorm.io/gorm"

	"github.com/apeming/go-proxy-server/internal/logger"
	"github.com/apeming/go-proxy-server/internal/models"
)

type Credentials map[string][]byte

// credentialsMap wraps credentials for atomic storage
type credentialsMap struct {
	data Credentials
}

var (
	// Use atomic.Value for lock-free reads in high-concurrency scenarios
	credentialsAtomic atomic.Value // stores *credentialsMap
	// Mutex only needed for write operations (periodic reload and manual add/delete)
	credWriteLock sync.Mutex
)

func init() {
	// Initialize atomic values with empty maps wrapped in structs
	credentialsAtomic.Store(&credentialsMap{data: make(Credentials)})
}

// LoadCredentialsFromDB loads user credentials from database
func LoadCredentialsFromDB(db *gorm.DB) error {
	var users []models.User

	err := db.Find(&users).Error

	if err != nil {
		return err
	}

	tempCred := make(Credentials)

	for _, user := range users {
		// Username should be globally unique due to database constraint
		// If duplicate found, it indicates data corruption
		if _, exists := tempCred[user.Username]; exists {
			return fmt.Errorf("data corruption: duplicate username '%s' found in database", user.Username)
		}
		tempCred[user.Username] = user.Password
	}

	// Atomic store - no read lock needed, lock-free reads continue to work
	credWriteLock.Lock()
	credentialsAtomic.Store(&credentialsMap{data: tempCred})
	credWriteLock.Unlock()

	return nil
}

// AddUser adds a new user to the database
func AddUser(db *gorm.DB, ip, username, password string) error {
	// Validate password strength
	if err := validatePasswordStrength(password); err != nil {
		return err
	}

	hashedPassword, err := HashPassword([]byte(password))
	if err != nil {
		return err
	}

	user := models.User{
		IP:       ip,
		Username: username,
		Password: hashedPassword,
	}

	// Directly insert and rely on database unique constraint
	// This prevents race conditions in concurrent scenarios
	err = db.Create(&user).Error
	if err != nil {
		// Check if error is due to unique constraint violation
		if strings.Contains(err.Error(), "UNIQUE constraint failed") ||
			strings.Contains(err.Error(), "duplicate key") {
			return fmt.Errorf("Username '%s' already exists", username)
		}
		return err
	}

	// Update the userCredentials map by re-syncing from the database
	// If reload fails, rollback the database insertion to maintain consistency
	if err := LoadCredentialsFromDB(db); err != nil {
		// Rollback: delete the just-inserted record
		db.Unscoped().Where("username = ?", username).Delete(&models.User{})
		return fmt.Errorf("failed to reload credentials after insertion: %w", err)
	}

	return nil
}

// validatePasswordStrength checks if the password meets minimum security requirements
func validatePasswordStrength(password string) error {
	if len(password) < 8 {
		return fmt.Errorf("password must be at least 8 characters long")
	}
	if len(password) > 128 {
		return fmt.Errorf("password must not exceed 128 characters")
	}

	// Check for at least one letter and one number
	hasLetter := false
	hasDigit := false
	for _, char := range password {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') {
			hasLetter = true
		}
		if char >= '0' && char <= '9' {
			hasDigit = true
		}
		if hasLetter && hasDigit {
			break
		}
	}

	if !hasLetter {
		return fmt.Errorf("password must contain at least one letter")
	}
	if !hasDigit {
		return fmt.Errorf("password must contain at least one digit")
	}

	return nil
}

// DeleteUser deletes a user from the database
func DeleteUser(db *gorm.DB, username string) error {
	// Use Unscoped to permanently delete the record (hard delete)
	// Username is globally unique, so we only need to check username
	err := db.Unscoped().Where("username = ?", username).Delete(&models.User{}).Error
	if err != nil {
		return err
	}

	// Update the userCredentials map by re-syncing from the database
	if err := LoadCredentialsFromDB(db); err != nil {
		logger.Error("Failed to reload credentials after deletion: %v", err)
		return err
	}

	return nil
}

// ListUsers lists all users from the database
func ListUsers(db *gorm.DB) error {
	return ListUsersToWriter(db, os.Stdout)
}

// ListUsersToWriter writes the user list to the provided writer.
func ListUsersToWriter(db *gorm.DB, w io.Writer) error {
	var users []models.User
	err := db.Find(&users).Error
	if err != nil {
		logger.Error("Failed to list users: %v", err)
		return err
	}

	usernames := make([]string, 0, len(users))
	for _, user := range users {
		usernames = append(usernames, user.Username)
	}
	slices.Sort(usernames)

	fmt.Fprintln(w, "Username")
	fmt.Fprintln(w, "----------")

	for _, username := range usernames {
		fmt.Fprintln(w, username)
	}

	return nil
}

// getCredentials returns the current credentials map (for internal use)
func getCredentials() Credentials {
	creds := credentialsAtomic.Load().(*credentialsMap)
	return creds.data
}
