package pkg

import (
	"fmt"
	"net/http"
	"sync"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	log "github.com/sirupsen/logrus"
)

type Manager struct {
	lock     sync.RWMutex
	clients  map[uuid.UUID]*Client
	sessions map[uuid.UUID]*Session
	rooms    map[uuid.UUID]*Room
	topics   map[uuid.UUID]*Topic
	upgrader websocket.Upgrader
}

func NewManager() *Manager {
	return &Manager{
		lock:     sync.RWMutex{},
		clients:  make(map[uuid.UUID]*Client),
		sessions: make(map[uuid.UUID]*Session),
		rooms:    make(map[uuid.UUID]*Room),
		topics:   make(map[uuid.UUID]*Topic),
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			CheckOrigin:     func(r *http.Request) bool { return true },
		},
	}
}

func (m *Manager) NewSession(
	sessionHello *SessionHello,
	conn *websocket.Conn,
) (*Session, error) {
	m.lock.Lock()
	defer m.lock.Unlock()

	if _, ok := m.sessions[sessionHello.SessionUUID]; ok {
		return nil, fmt.Errorf("session already exists")
	}

	c, ok := m.clients[sessionHello.ClientUUID]
	if !ok {
		c = &Client{
			manager:  m,
			lock:     sync.RWMutex{},
			uuid:     sessionHello.ClientUUID,
			key:      sessionHello.ClientPublicKey,
			sessions: make([]*Session, 0),
		}

		m.clients[sessionHello.ClientUUID] = c

		EventServerClientsGauge.Inc()
	}

	s := &Session{
		manager: m,
		client:  c,
		lock:    sync.RWMutex{},
		uuid:    sessionHello.SessionUUID,
		conn:    conn,
		send:    make(chan []byte, 256),
		rooms:   make([]*Room, 0),
		topics:  make([]*Topic, 0),
	}

	c.lock.Lock()
	defer c.lock.Unlock()
	c.sessions = append(c.sessions, s)
	m.sessions[s.uuid] = s

	EventServerSessionsGauge.Inc()

	return s, nil
}

func (m *Manager) GetRoom(uuid uuid.UUID) *Room {
	m.lock.RLock()
	defer m.lock.RUnlock()

	return m.rooms[uuid]
}

func (m *Manager) GetOrCreateRoom(uuid uuid.UUID) *Room {
	m.lock.Lock()
	defer m.lock.Unlock()

	if room, ok := m.rooms[uuid]; ok {
		return room
	}

	room := &Room{
		manager:  m,
		lock:     sync.RWMutex{},
		uuid:     uuid,
		sessions: make([]*Session, 0),
	}

	m.rooms[uuid] = room

	return room
}

func (m *Manager) GetTopic(uuid uuid.UUID) *Topic {
	m.lock.RLock()
	defer m.lock.RUnlock()
	return m.topics[uuid]
}

func (m *Manager) GetOrCreateTopic(uuid uuid.UUID) *Topic {
	m.lock.Lock()
	defer m.lock.Unlock()

	if topic, ok := m.topics[uuid]; ok {
		return topic
	}

	topic := &Topic{
		manager:  m,
		lock:     sync.RWMutex{},
		uuid:     uuid,
		consumer: nil,
		queue:    make(chan []byte, 256),
	}

	m.topics[uuid] = topic

	return topic
}

func (m *Manager) DeleteSession(session *Session) {
	m.lock.Lock()
	defer m.lock.Unlock()

	s, ok := m.sessions[session.uuid]
	if !ok {
		return
	}

	delete(m.sessions, session.uuid)

	for _, room := range s.rooms {
		room.lock.Lock()
		for i, session := range room.sessions {
			if session.uuid == s.uuid {
				room.sessions = append(
					room.sessions[:i], room.sessions[i+1:]...)
				break
			}
		}
		room.lock.Unlock()
	}

	for _, topic := range s.topics {
		topic.lock.Lock()
		topic.consumer = nil
		close(topic.queue)
		topic.queue = make(chan []byte, 256)
		topic.lock.Unlock()
	}

	EventServerSessionsGauge.Dec()

	// Remove the session from the client
	s.client.lock.Lock()
	defer s.client.lock.Unlock()
	for i, session := range s.client.sessions {
		if session.uuid == s.uuid {
			s.client.sessions = append(
				s.client.sessions[:i], s.client.sessions[i+1:]...)
			break
		}
	}

	if len(s.client.sessions) == 0 {
		delete(m.clients, s.client.uuid)
		EventServerClientsGauge.Dec()
	}
}

func (m *Manager) HealthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
}

func (m *Manager) SocketHandler(w http.ResponseWriter, r *http.Request) {
	// Set the response headers
	w.Header().Set("Cache-Control", "no-cache")

	// Upgrade the connection to a websocket connection
	conn, err := m.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Error("Failed to upgrade connection: ", err)
		return
	}

	defer conn.Close()

	// Read the session hello message
	_, message, err := conn.ReadMessage()
	if err != nil {
		log.Error("Failed to read session hello message: ", err)
		conn.Close()
		return
	}

	// Decode the session hello message
	sessionHello, err := decodeSessionHello(message)
	if err != nil {
		log.Error("Failed to decode session hello message: ", err)
		conn.Close()
		return
	}

	// Register our new session
	session, err := m.NewSession(sessionHello, conn)
	if err != nil {
		log.Error("Failed to register session: ", err)
		conn.Close()
		return
	}

	defer m.DeleteSession(session)

	logFields := log.Fields{
		"client":  session.client.uuid,
		"session": session.uuid,
	}

	// Log that we have a new session
	log.WithFields(logFields).Info("New session")

	// Start reading messages from the connection
	go session.read()

	// Write messages to the connection
	session.write()

	// Log that we have a closed session
	log.WithFields(logFields).Info("Closed session")
}
