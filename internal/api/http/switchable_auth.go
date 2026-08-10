package apihttp

import (
	"net/http"
	"sync"

	"github.com/l-ra/knowledge-core/internal/auth"
)

// SwitchableAuthenticator allows hot-swapping the active Authenticator
// after Admin UI links OIDC (no process restart).
type SwitchableAuthenticator struct {
	mu      sync.RWMutex
	current Authenticator
}

func NewSwitchableAuthenticator(initial Authenticator) *SwitchableAuthenticator {
	return &SwitchableAuthenticator{current: initial}
}

func (s *SwitchableAuthenticator) Authenticate(r *http.Request) (auth.Subject, error) {
	s.mu.RLock()
	a := s.current
	s.mu.RUnlock()
	return a.Authenticate(r)
}

func (s *SwitchableAuthenticator) Swap(next Authenticator) {
	s.mu.Lock()
	s.current = next
	s.mu.Unlock()
}

func (s *SwitchableAuthenticator) Current() Authenticator {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.current
}
