package auth

import (
	"fmt"
	"io"
	"net"
	"os"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	"gorm.io/gorm"

	"github.com/apeming/go-proxy-server/internal/logger"
	"github.com/apeming/go-proxy-server/internal/models"
)

// whitelistMap wraps whitelist for atomic storage
type whitelistMap struct {
	data map[string]bool
}

var (
	// Use atomic.Value for lock-free reads in high-concurrency scenarios
	ipWhitelistAtomic atomic.Value // stores *whitelistMap
	// Mutex only needed for write operations (periodic reload and manual add/delete)
	whitelistWriteLock sync.Mutex
)

func init() {
	// Initialize atomic values with empty maps wrapped in structs
	ipWhitelistAtomic.Store(&whitelistMap{data: make(map[string]bool)})
}

// CheckIPWhitelist checks if a client IP is in the whitelist
func CheckIPWhitelist(clientIP string) bool {
	// Lock-free read using atomic.Value - no type assertion overhead
	whitelist := ipWhitelistAtomic.Load().(*whitelistMap)
	return whitelist.data[clientIP]
}

// isValidIP validates if a string is a valid IP address
func isValidIP(ip string) bool {
	return net.ParseIP(ip) != nil
}

// LoadWhitelistFromDB loads IP whitelist from database
func LoadWhitelistFromDB(db *gorm.DB) error {
	var whitelist []models.Whitelist

	err := db.Find(&whitelist).Error
	if err != nil {
		return err
	}

	tempWhitelist := make(map[string]bool)
	for _, item := range whitelist {
		tempWhitelist[item.IP] = true
	}

	// Atomic store - no read lock needed, lock-free reads continue to work
	whitelistWriteLock.Lock()
	ipWhitelistAtomic.Store(&whitelistMap{data: tempWhitelist})
	whitelistWriteLock.Unlock()

	return nil
}

// AddIPToWhitelist adds an IP address to the whitelist
func AddIPToWhitelist(db *gorm.DB, ip string) error {
	if !isValidIP(ip) {
		return fmt.Errorf("invalid ip")
	}

	// Directly insert and rely on database unique constraint
	// This prevents race conditions in concurrent scenarios
	whitelist := models.Whitelist{IP: ip}
	err := db.Create(&whitelist).Error
	if err != nil {
		// Check if error is due to unique constraint violation
		if strings.Contains(err.Error(), "UNIQUE constraint failed") ||
			strings.Contains(err.Error(), "duplicate key") {
			return fmt.Errorf("IP already in whitelist")
		}
		return err
	}

	// Reload whitelist from database
	// If reload fails, rollback the database insertion to maintain consistency
	if err := LoadWhitelistFromDB(db); err != nil {
		// Rollback: delete the just-inserted record
		db.Unscoped().Where("ip = ?", ip).Delete(&models.Whitelist{})
		return fmt.Errorf("failed to reload whitelist after insertion: %w", err)
	}

	return nil
}

// DeleteIPFromWhitelist removes an IP address from the whitelist
func DeleteIPFromWhitelist(db *gorm.DB, ip string) error {
	// Use Unscoped to permanently delete the record (hard delete)
	err := db.Unscoped().Where("ip = ?", ip).Delete(&models.Whitelist{}).Error
	if err != nil {
		return err
	}

	// Reload whitelist from database
	if err := LoadWhitelistFromDB(db); err != nil {
		logger.Error("Failed to reload whitelist after deletion: %v", err)
		return err
	}

	return nil
}

// GetWhitelistIPs returns all IP addresses in the whitelist
func GetWhitelistIPs() []string {
	// Lock-free read using atomic.Value - no type assertion overhead
	whitelist := ipWhitelistAtomic.Load().(*whitelistMap)

	ips := make([]string, 0, len(whitelist.data))
	for ip := range whitelist.data {
		ips = append(ips, ip)
	}
	slices.Sort(ips)
	return ips
}

// ListWhitelist prints all whitelisted IP addresses.
func ListWhitelist(db *gorm.DB) error {
	if err := LoadWhitelistFromDB(db); err != nil {
		logger.Error("Failed to load whitelist: %v", err)
		return err
	}
	return WriteWhitelist(os.Stdout)
}

// WriteWhitelist writes the current whitelist to the provided writer.
func WriteWhitelist(w io.Writer) error {
	if w == nil {
		return fmt.Errorf("writer is nil")
	}

	ips := GetWhitelistIPs()
	fmt.Fprintln(w, "IP")
	fmt.Fprintln(w, "----------")
	for _, ip := range ips {
		fmt.Fprintln(w, ip)
	}
	return nil
}
