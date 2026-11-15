package security

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"time"

	"cyberstrike-ai/internal/database"
)

type restartSession struct {
	Digest        sessionDigest
	UserID        string
	ExpiresAt     time.Time
	UserUpdatedAt time.Time
}

// HandoffSessions runs the update exec with the session map locked. A logout or
// revocation cannot complete in the old process after its token is handed off.
// The payload contains token digests only, and is passed through a private descriptor.
func (a *AuthManager) HandoffSessions(exec func([]byte) error) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	now := time.Now()
	var active []restartSession
	for digest, s := range a.sessions {
		if s.ExpiresAt.After(now) {
			active = append(active, restartSession{digest, s.UserID, s.ExpiresAt, s.userUpdatedAt})
		}
	}
	if len(active) == 0 {
		return exec(nil)
	}
	payload, err := json.Marshal(active)
	if err != nil {
		return err
	}
	return exec(payload)
}

// RestoreRestartSessions is called before the replacement starts serving.
// Rebuild access from the current RBAC store and keep the original expiration.
func (a *AuthManager) RestoreRestartSessions(r io.Reader) error {
	var saved []restartSession
	if err := json.NewDecoder(r).Decode(&saved); err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.db == nil {
		return errors.New("authentication store is not configured")
	}
	accessByUser := make(map[string]*database.RBACAccess)
	restored := make(map[sessionDigest]Session, len(saved))
	now := time.Now()
	for _, entry := range saved {
		if !entry.ExpiresAt.After(now) {
			continue
		}
		access, loaded := accessByUser[entry.UserID]
		if !loaded {
			var err error
			access, err = a.db.ResolveRBACAccess(entry.UserID)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			accessByUser[entry.UserID] = access
		}
		if access == nil || !access.User.Enabled || !access.User.UpdatedAt.Equal(entry.UserUpdatedAt) {
			continue
		}
		restored[entry.Digest] = sessionForAccess(access, entry.ExpiresAt)
	}
	a.sessions = restored
	return nil
}
