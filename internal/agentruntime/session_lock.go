package agentruntime

import (
	"context"
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"sync"
)

// ConversationStorageKey keeps every session/child scope for a conversation under one cleanup boundary.
func ConversationStorageKey(conversationID string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(conversationID)))
}

type sessionLock struct {
	token chan struct{}
	refs  int
}

var sessionLockMu sync.Mutex
var sessionLocks = map[string]*sessionLock{}

func acquireSession(ctx context.Context, dir string) (func(), error) {
	if dir == "" {
		return func() {}, nil
	}
	key, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	sessionLockMu.Lock()
	lock := sessionLocks[key]
	if lock == nil {
		lock = &sessionLock{token: make(chan struct{}, 1)}
		sessionLocks[key] = lock
	}
	lock.refs++
	sessionLockMu.Unlock()
	remove := func() {
		sessionLockMu.Lock()
		defer sessionLockMu.Unlock()
		lock.refs--
		if lock.refs == 0 {
			delete(sessionLocks, key)
		}
	}
	select {
	case lock.token <- struct{}{}:
		return func() { <-lock.token; remove() }, nil
	case <-ctx.Done():
		remove()
		return nil, ctx.Err()
	}
}
