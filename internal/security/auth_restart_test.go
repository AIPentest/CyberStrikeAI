package security

import (
	"bytes"
	"path/filepath"
	"testing"
	"time"

	"cyberstrike-ai/internal/database"

	"go.uber.org/zap"
)

func TestRestartHandoffRejectsExpiredAndChangedAccounts(t *testing.T) {
	db, err := database.NewDB(filepath.Join(t.TempDir(), "auth.db"), zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	old := NewAuthManager(12)
	password, err := old.AttachRBACStore(db)
	if err != nil {
		t.Fatal(err)
	}
	token, _, err := old.Authenticate("admin", password)
	if err != nil {
		t.Fatal(err)
	}
	digest := tokenDigest("expired-token")
	expired := old.sessions[tokenDigest(token)]
	expired.ExpiresAt = time.Now().Add(-time.Second)
	old.sessions[digest] = expired

	var handoff []byte
	if err := old.HandoffSessions(func(payload []byte) error {
		handoff = payload
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(handoff) == 0 || bytes.Contains(handoff, []byte(token)) {
		t.Fatal("handoff must contain only the digest of the active token")
	}
	next := NewAuthManager(12)
	if _, err := next.AttachRBACStore(db); err != nil {
		t.Fatal(err)
	}
	if err := next.RestoreRestartSessions(bytes.NewReader(handoff)); err != nil {
		t.Fatal(err)
	}
	if len(next.sessions) != 1 {
		t.Fatalf("restored %d sessions, want exactly the unexpired session", len(next.sessions))
	}

	// Simulate a password change committed to the store before revocation reached the old map.
	if err := old.UpdateUserPassword("admin", "changed-test-password"); err != nil {
		t.Fatal(err)
	}
	if err := next.RestoreRestartSessions(bytes.NewReader(handoff)); err != nil {
		t.Fatal(err)
	}
	if _, ok := next.ValidateToken(token); ok {
		t.Fatal("restored a session issued before the account changed")
	}
}
