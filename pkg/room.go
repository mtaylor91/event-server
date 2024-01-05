package pkg

import (
	"sync"

	"github.com/google/uuid"
)

type Room struct {
	manager *Manager
	lock    sync.RWMutex
	uuid    uuid.UUID
	clients []*Client
}
