package auth

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// SyncState reloads the in-memory authentication and whitelist state from the database.
func SyncState(db *gorm.DB) error {
	if err := LoadCredentialsFromDB(db); err != nil {
		return fmt.Errorf("load credentials: %w", err)
	}
	if err := LoadWhitelistFromDB(db); err != nil {
		return fmt.Errorf("load whitelist: %w", err)
	}
	return nil
}

// StartStateReloader periodically refreshes the in-memory auth state until ctx is canceled.
func StartStateReloader(ctx context.Context, db *gorm.DB, interval time.Duration, onError func(error)) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := SyncState(db); err != nil && onError != nil {
					onError(err)
				}
			}
		}
	}()
}
