package aimux

import "sync"

type Authenticator struct {
	mu          sync.RWMutex
	tokenToUser map[string]string
}

func NewAuthenticator(users []User) *Authenticator {
	a := &Authenticator{
		tokenToUser: make(map[string]string, len(users)),
	}
	a.Update(users)
	return a
}

func (a *Authenticator) Update(users []User) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.tokenToUser = make(map[string]string, len(users))
	for _, user := range users {
		a.tokenToUser[user.Token] = user.Name
	}
}

func (a *Authenticator) HasUsers() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return len(a.tokenToUser) > 0
}

func (a *Authenticator) Authenticate(token string) (string, bool) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	name, ok := a.tokenToUser[token]
	return name, ok
}
