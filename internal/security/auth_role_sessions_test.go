package security

import (
	"testing"
	"time"
)

func TestRevokeRoleSessionsMatchesSnapshotAndPreservesOthers(t *testing.T) {
	a := NewAuthManager(12)
	for _, session := range []Session{
		{Token: "member-first", UserID: "member", Roles: []string{"viewer", "changed"}},
		{Token: "member-second", UserID: "member", Roles: []string{"changed"}},
		{Token: "other", UserID: "other", Roles: []string{"viewer"}},
		{Token: "admin", UserID: "admin", Roles: []string{"admin"}},
	} {
		session.ExpiresAt = time.Now().Add(time.Hour)
		a.sessions[tokenDigest(session.Token)] = session
	}
	a.RevokeRoleSessions(" ")
	if len(a.sessions) != 4 {
		t.Fatal("blank role revoked sessions")
	}
	a.RevokeRoleSessions(" changed ")
	for _, token := range []string{"member-first", "member-second"} {
		if _, ok := a.ValidateToken(token); ok {
			t.Fatalf("affected session %s remains valid", token)
		}
	}
	for _, token := range []string{"other", "admin"} {
		if _, ok := a.ValidateToken(token); !ok {
			t.Fatalf("unrelated session %s revoked", token)
		}
	}
}

func TestPublishSessionRejectsSnapshotResolvedAcrossRevocation(t *testing.T) {
	for _, kind := range []string{"role", "user", "all"} {
		t.Run(kind, func(t *testing.T) {
			a := NewAuthManager(12)
			version := a.accessVersion
			// A login has resolved its snapshot but has not yet published it.
			snapshot := Session{Token: "pending", UserID: "member", Roles: []string{"changed"}, ExpiresAt: time.Now().Add(time.Hour)}
			switch kind {
			case "role":
				a.RevokeRoleSessions("changed")
			case "user":
				a.RevokeUserSessions("member")
			case "all":
				a.RevokeAllSessions()
			}
			if a.publishSession(snapshot, version) {
				t.Fatal("stale authorization snapshot published after revocation")
			}
			if _, ok := a.ValidateToken(snapshot.Token); ok {
				t.Fatal("rejected snapshot is visible to authentication")
			}
			if !a.publishSession(snapshot, a.accessVersion) {
				t.Fatal("freshly resolved snapshot rejected")
			}
		})
	}
}
