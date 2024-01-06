package pkg

import (
	"sync"

	"github.com/google/uuid"
)

type Topic struct {
	manager  *Manager
	lock     sync.RWMutex
	uuid     uuid.UUID
	consumer *Session
	queue    chan []byte
}
