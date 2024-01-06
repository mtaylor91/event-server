package pkg

import (
	"crypto/ed25519"
	"sync"

	"github.com/google/uuid"
)

type Client struct {
	manager  *Manager
	lock     sync.RWMutex
	uuid     uuid.UUID
	key      ed25519.PublicKey
	sessions []*Session
}
