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
	rooms    map[uuid.UUID]*Room
	topics   map[uuid.UUID]*Topic
	upgrader websocket.Upgrader
}

func NewManager() *Manager {
	return &Manager{
		lock:    sync.RWMutex{},
		clients: make(map[uuid.UUID]*Client),
		rooms:   make(map[uuid.UUID]*Room),
		topics:  make(map[uuid.UUID]*Topic),
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			CheckOrigin:     func(r *http.Request) bool { return true },
		},
	}
}

func (m *Manager) NewClient(
	clientHello *ClientHello,
	conn *websocket.Conn,
) (*Client, error) {
	m.lock.Lock()
	defer m.lock.Unlock()
	EventServerClientsGauge.Inc()
	if _, ok := m.clients[clientHello.UUID]; ok {
		return nil, fmt.Errorf("client already exists")
	} else {
		c := &Client{
			manager: m,
			lock:    sync.RWMutex{},
			uuid:    clientHello.UUID,
			key:     clientHello.PublicKey,
			conn:    conn,
			send:    make(chan []byte, 256),
			rooms:   make([]*Room, 0),
			topics:  make([]*Topic, 0),
		}
		m.clients[clientHello.UUID] = c
		return c, nil
	}
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
		manager: m,
		lock:    sync.RWMutex{},
		uuid:    uuid,
		clients: make([]*Client, 0),
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

func (m *Manager) DeleteClient(client *Client) {
	m.lock.Lock()
	defer m.lock.Unlock()

	c, ok := m.clients[client.uuid]
	if !ok {
		return
	}

	delete(m.clients, client.uuid)

	for _, room := range c.rooms {
		room.lock.Lock()
		for i, client := range room.clients {
			if client.uuid == c.uuid {
				room.clients = append(
					room.clients[:i], room.clients[i+1:]...)
				break
			}
		}
		room.lock.Unlock()
	}

	for _, topic := range c.topics {
		topic.lock.Lock()
		topic.consumer = nil
		close(topic.queue)
		topic.queue = make(chan []byte, 256)
		topic.lock.Unlock()
	}

	EventServerClientsGauge.Dec()
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

	// Read the client hello message
	_, message, err := conn.ReadMessage()
	if err != nil {
		log.Error("Failed to read client hello message: ", err)
		conn.Close()
		return
	}

	// Decode the client hello message
	clientHello, err := decodeClientHello(message)
	if err != nil {
		log.Error("Failed to decode client hello message: ", err)
		conn.Close()
		return
	}

	// Register our new client
	client, err := m.NewClient(clientHello, conn)
	if err != nil {
		log.Error("Failed to register client: ", err)
		conn.Close()
		return
	}

	// Log that we have a new client
	log.Info("New client: ", client.uuid)

	// Start listening for messages from the client
	go client.read()

	// Start listening for messages from the manager
	go client.write()
}
