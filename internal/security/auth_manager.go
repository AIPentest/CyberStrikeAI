package security

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"cyberstrike-ai/internal/database"

	"github.com/google/uuid"
)

// Predefined errors for authentication operations.
var (
	ErrInvalidPassword = errors.New("invalid password")
)

// Session represents an authenticated user session.
type Session struct {
	Token            string
	ExpiresAt        time.Time
	UserID           string
	Username         string
	DisplayName      string
	Roles            []string
	Permissions      map[string]bool
	PermissionScopes map[string]string
	Scope            string
}

// AuthManager manages password-based authentication and session lifecycle.
type AuthManager struct {
	sessionDuration time.Duration
	db              *database.DB

	mu       sync.RWMutex
	sessions map[string]Session
	// Changes when cached authorization is revoked, including while a login
	// is resolving access outside mu.
	accessVersion uint64
}

// NewAuthManager creates a new AuthManager instance.
func NewAuthManager(sessionDurationHours int) *AuthManager {
	if sessionDurationHours <= 0 {
		sessionDurationHours = 12
	}

	return &AuthManager{
		sessionDuration: time.Duration(sessionDurationHours) * time.Hour,
		sessions:        make(map[string]Session),
	}
}

// AttachRBACStore enables multi-user RBAC authentication. When no users exist yet,
// it bootstraps the built-in admin account and returns the generated initial password.
func (a *AuthManager) AttachRBACStore(db *database.DB) (generatedAdminPassword string, err error) {
	if db == nil {
		return "", errors.New("database is required for authentication")
	}

	needsAdminPassword, err := db.RBACNeedsAdminPassword()
	if err != nil {
		return "", err
	}

	adminPasswordHash := ""
	if needsAdminPassword {
		generatedAdminPassword, err = GenerateStrongPassword(24)
		if err != nil {
			return "", err
		}
		adminPasswordHash, err = HashPassword(generatedAdminPassword)
		if err != nil {
			return "", err
		}
	}

	if err := db.BootstrapRBAC(adminPasswordHash, PermissionCatalog); err != nil {
		return "", err
	}

	a.mu.Lock()
	a.db = db
	a.mu.Unlock()
	return generatedAdminPassword, nil
}

// Authenticate validates the password and creates a new session.
func (a *AuthManager) Authenticate(username, password string) (string, time.Time, error) {
	for {
		a.mu.RLock()
		version := a.accessVersion
		a.mu.RUnlock()
		session, err := a.authenticateSession(username, password)
		if err != nil {
			return "", time.Time{}, err
		}
		if a.publishSession(session, version) {
			return session.Token, session.ExpiresAt, nil
		}
	}
}

// publishSession rejects a snapshot resolved across an authorization revocation.
func (a *AuthManager) publishSession(session Session, version uint64) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.accessVersion != version {
		return false
	}
	a.sessions[session.Token] = session
	return true
}

func (a *AuthManager) authenticateSession(username, password string) (Session, error) {
	token := uuid.NewString()
	expiresAt := time.Now().Add(a.sessionDuration)

	a.mu.RLock()
	db := a.db
	a.mu.RUnlock()
	if db == nil {
		return Session{}, errors.New("authentication store is not configured")
	}

	username = strings.TrimSpace(strings.ToLower(username))
	if username == "" {
		username = "admin"
	}
	user, err := db.GetRBACUserByUsername(username)
	if err != nil {
		if err == sql.ErrNoRows {
			return Session{}, ErrInvalidPassword
		}
		return Session{}, err
	}
	if !user.Enabled || !VerifyPasswordHash(password, user.PasswordHash) {
		return Session{}, ErrInvalidPassword
	}
	access, err := db.ResolveRBACAccess(user.ID)
	if err != nil {
		return Session{}, err
	}
	roleIDs := make([]string, 0, len(access.Roles))
	for _, role := range access.Roles {
		roleIDs = append(roleIDs, role.ID)
	}
	return Session{
		Token:            token,
		ExpiresAt:        expiresAt,
		UserID:           user.ID,
		Username:         user.Username,
		DisplayName:      user.DisplayName,
		Roles:            roleIDs,
		Permissions:      access.Permissions,
		PermissionScopes: access.PermissionScopes,
		Scope:            access.Scope,
	}, nil
}

func (s Session) ScopeFor(permission string) string {
	if scope := strings.TrimSpace(s.PermissionScopes[strings.TrimSpace(permission)]); scope != "" {
		return scope
	}
	return strings.TrimSpace(s.Scope)
}

// ValidateToken checks whether the provided token is still valid.
func (a *AuthManager) ValidateToken(token string) (Session, bool) {
	if strings.TrimSpace(token) == "" {
		return Session{}, false
	}

	a.mu.RLock()
	session, ok := a.sessions[token]
	a.mu.RUnlock()
	if !ok {
		return Session{}, false
	}

	if time.Now().After(session.ExpiresAt) {
		a.mu.Lock()
		delete(a.sessions, token)
		a.mu.Unlock()
		return Session{}, false
	}

	return session, true
}

// CheckPassword verifies whether the provided password matches the current password.
func (a *AuthManager) CheckPassword(password string) bool {
	return a.CheckUserPassword("admin", password)
}

// CheckUserPassword verifies whether the provided password matches a user.
func (a *AuthManager) CheckUserPassword(username, password string) bool {
	a.mu.RLock()
	db := a.db
	a.mu.RUnlock()
	if db == nil {
		return false
	}
	user, err := db.GetRBACUserByUsername(username)
	if err != nil {
		return false
	}
	return VerifyPasswordHash(password, user.PasswordHash)
}

func (a *AuthManager) UpdateUserPassword(userID, password string) error {
	password = strings.TrimSpace(password)
	if password == "" {
		return errors.New("auth password must be configured")
	}
	hash, err := HashPassword(password)
	if err != nil {
		return err
	}
	a.mu.RLock()
	db := a.db
	a.mu.RUnlock()
	if db == nil {
		return errors.New("authentication store is not configured")
	}
	if err := db.UpdateRBACUserPassword(userID, hash); err != nil {
		return err
	}
	a.mu.Lock()
	a.accessVersion++
	for token, session := range a.sessions {
		if session.UserID == userID {
			delete(a.sessions, token)
		}
	}
	a.mu.Unlock()
	return nil
}

// RevokeToken invalidates the specified token.
func (a *AuthManager) RevokeToken(token string) {
	if strings.TrimSpace(token) == "" {
		return
	}

	a.mu.Lock()
	delete(a.sessions, token)
	a.mu.Unlock()
}

func (a *AuthManager) RevokeUserSessions(userID string) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return
	}
	a.mu.Lock()
	a.accessVersion++
	for token, session := range a.sessions {
		if session.UserID == userID {
			delete(a.sessions, token)
		}
	}
	a.mu.Unlock()
}

// RevokeRoleSessions invalidates only snapshots containing the changed role.
// Matching the session snapshot also works after the role has been deleted.
func (a *AuthManager) RevokeRoleSessions(roleID string) {
	roleID = strings.TrimSpace(roleID)
	if roleID == "" {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.accessVersion++
	for token, session := range a.sessions {
		for _, id := range session.Roles {
			if id == roleID {
				delete(a.sessions, token)
				break
			}
		}
	}
}

func (a *AuthManager) RevokeAllSessions() {
	a.mu.Lock()
	a.accessVersion++
	a.sessions = make(map[string]Session)
	a.mu.Unlock()
}

// SessionDurationHours returns the configured session duration in hours.
func (a *AuthManager) SessionDurationHours() int {
	return int(a.sessionDuration / time.Hour)
}

func allPermissions() map[string]bool {
	out := make(map[string]bool, len(PermissionCatalog))
	for key := range PermissionCatalog {
		out[key] = true
	}
	return out
}

// AuthenticateOIDC resolves a provisioned external account and issues an existing local session.
// A stable issuer+subject fingerprint is stored in the existing password_hash column.
// Local accounts and built-in admin can never be linked by username alone.
func (a *AuthManager) AuthenticateOIDC(username, displayName, issuer, subject string) (string, time.Time, error) {
	username = strings.TrimSpace(strings.ToLower(username))
	if username == "" || len(username) > 64 || username == "admin" || strings.ContainsAny(username, "\x00\r\n") || issuer == "" || subject == "" {
		return "", time.Time{}, errors.New("invalid OIDC identity")
	}
	fingerprint := sha256.Sum256([]byte(issuer + "\x00" + subject))
	marker := "!oidc:" + hex.EncodeToString(fingerprint[:])
	a.mu.RLock()
	db := a.db
	a.mu.RUnlock()
	if db == nil {
		return "", time.Time{}, errors.New("RBAC store unavailable")
	}
	user, err := db.GetRBACUserByUsername(username)
	if err == sql.ErrNoRows {
		user, err = db.CreateRBACUser(username, displayName, marker, true, []string{database.RBACSystemRoleViewer})
		if err != nil {
			user, err = db.GetRBACUserByUsername(username)
			if err != nil {
				return "", time.Time{}, err
			}
		}
	} else if err != nil {
		return "", time.Time{}, err
	}
	if !user.Enabled || user.IsBuiltin || user.PasswordHash != marker {
		return "", time.Time{}, fmt.Errorf("OIDC identity conflicts with existing user")
	}
	if strings.TrimSpace(displayName) != "" && user.DisplayName != displayName {
		if err = db.UpdateRBACUser(user.ID, displayName, nil, nil); err != nil {
			return "", time.Time{}, err
		}
		user.DisplayName = displayName
	}
	for {
		a.mu.RLock()
		version := a.accessVersion
		a.mu.RUnlock()
		access, err := db.ResolveRBACAccess(user.ID)
		if err != nil {
			return "", time.Time{}, err
		}
		roles := make([]string, 0, len(access.Roles))
		for _, role := range access.Roles {
			roles = append(roles, role.ID)
		}
		token := uuid.NewString()
		expiry := time.Now().Add(a.sessionDuration)
		session := Session{Token: token, ExpiresAt: expiry, UserID: user.ID, Username: user.Username, DisplayName: user.DisplayName, Roles: roles, Permissions: access.Permissions, PermissionScopes: access.PermissionScopes, Scope: access.Scope}
		if a.publishSession(session, version) {
			return token, expiry, nil
		}
	}
}
