package auth

import (
	"context"
	"errors"
	"sync"
)

// MemoryStore is only for unit tests and standalone demo; production uses PostgresStore.
type MemoryStore struct {
	mu       sync.Mutex
	sessions map[string]Session
}

func NewMemoryStore() *MemoryStore { return &MemoryStore{sessions: make(map[string]Session)} }
func (m *MemoryStore) Create(_ context.Context, s Session) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions[s.FamilyID] = s
	return nil
}
func (m *MemoryStore) FindByTokenHash(
	_ context.Context,
	h [32]byte,
) (Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, s := range m.sessions {
		if s.Revoked {
			continue
		}

		if s.TokenHash == h {
			return s, nil
		}

		if s.OldTokenHash == h {
			return s, nil
		}
	}

	return Session{}, errors.New("not found")
}
func (m *MemoryStore) Save(_ context.Context, s Session) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.sessions[s.FamilyID]; !ok {
		return errors.New("not found")
	}
	m.sessions[s.FamilyID] = s
	return nil
}
func (m *MemoryStore) RevokeFamily(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	if ok {
		s.Revoked = true
		m.sessions[id] = s
	}
	return nil
}
func (m *MemoryStore) RevokeUser(_ context.Context, id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for k, s := range m.sessions {
		if s.Principal.UserID == id {
			s.Revoked = true
			m.sessions[k] = s
		}
	}
	return nil
}
func (m *MemoryStore) CompareAndSwap(
	_ context.Context,
	s Session,
	expectedTokenHash [32]byte,
) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	current, ok := m.sessions[s.FamilyID]
	if !ok || current.Revoked {
		return ErrSessionConflict
	}

	if current.TokenHash != expectedTokenHash {
		return ErrSessionConflict
	}

	m.sessions[s.FamilyID] = s
	return nil
}