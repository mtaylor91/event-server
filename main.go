package main

import (
	"crypto/ed25519"
	"encoding/binary"
	"fmt"
	"net/http"
	"sync"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	log "github.com/sirupsen/logrus"
)

type EventType string

const (
	EventTypeEnterRoom EventType = "enter_room"
	EventTypeLeaveRoom EventType = "leave_room"
)

type Event struct {
	Sender    uuid.UUID
	Room      uuid.UUID
	EventType EventType
	Payload   []byte
}

type Manager struct {
	lock     sync.RWMutex
	clients  map[uuid.UUID]*Client
	rooms    map[uuid.UUID]*Room
	upgrader websocket.Upgrader
}

type Client struct {
	manager *Manager
	lock    sync.RWMutex
	uuid    uuid.UUID
	key     ed25519.PublicKey
	conn    *websocket.Conn
	send    chan []byte
	rooms   []*Room
}

type Room struct {
	manager *Manager
	lock    sync.RWMutex
	uuid    uuid.UUID
	clients []*Client
}

type ClientHello struct {
	UUID      uuid.UUID
	PublicKey ed25519.PublicKey
}

func NewManager() *Manager {
	return &Manager{
		lock:    sync.RWMutex{},
		clients: make(map[uuid.UUID]*Client),
		rooms:   make(map[uuid.UUID]*Room),
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

func (m *Manager) handleDisconnect(client *Client) {
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
}

func (c *Client) handleEnterRoom(event *Event) {
	room := c.manager.GetOrCreateRoom(event.Room)

	room.lock.Lock()
	defer room.lock.Unlock()
	room.clients = append(room.clients, c)

	c.lock.Lock()
	defer c.lock.Unlock()
	c.rooms = append(c.rooms, room)
}

func (c *Client) handleLeaveRoom(event *Event) {
	room := c.manager.GetOrCreateRoom(event.Room)

	room.lock.Lock()
	defer room.lock.Unlock()
	for i, client := range room.clients {
		if client.uuid == event.Sender {
			room.clients = append(room.clients[:i], room.clients[i+1:]...)
			break
		}
	}

	c.lock.Lock()
	defer c.lock.Unlock()
	for i, room := range c.rooms {
		if room.uuid == event.Room {
			c.rooms = append(c.rooms[:i], c.rooms[i+1:]...)
			break
		}
	}

	if len(room.clients) == 0 {
		c.manager.lock.Lock()
		delete(c.manager.rooms, room.uuid)
		c.manager.lock.Unlock()
	}
}

func (c *Client) handleMessage(message []byte) error {
	event, err := decodeEvent(c.key, message)
	if err != nil {
		return fmt.Errorf("failed to decode event: %w", err)
	}

	switch event.EventType {
	case EventTypeEnterRoom:
		c.handleEnterRoom(event)
	case EventTypeLeaveRoom:
		c.handleLeaveRoom(event)
	default:
		// Broadcast the message to all clients in the room
		room := c.manager.GetRoom(event.Room)
		if room == nil {
			break
		}

		room.lock.RLock()
		defer room.lock.RUnlock()
		for _, client := range room.clients {
			client.send <- message
		}
	}

	return nil
}

func (c *Client) read() {
	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			log.Error("Failed to read message: ", err)
			break
		}

		if err := c.handleMessage(message); err != nil {
			log.Error("Failed to handle message: ", err)
			break
		}
	}

	close(c.send)
}

func (c *Client) write() {
	for {
		message := <-c.send
		if message == nil {
			break
		}

		err := c.conn.WriteMessage(websocket.BinaryMessage, message)
		if err != nil {
			log.Error("Failed to write message: ", err)
			break
		}
	}

	c.conn.Close()
	c.manager.handleDisconnect(c)

	log.Info("Client disconnected: ", c.uuid)
}

func (m *Manager) healthHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}

func (m *Manager) socketHandler(w http.ResponseWriter, r *http.Request) {
	log.Info("New connection")

	// Set the response headers
	w.Header().Set("Cache-Control", "no-cache")

	// Upgrade the connection to a websocket connection
	conn, err := m.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Error("Failed to upgrade connection: ", err)
		return
	}

	log.Info("Upgraded connection")

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

func decodeClientHello(message []byte) (*ClientHello, error) {
	// Make sure the message is 48 bytes
	if len(message) != 48 {
		return nil, fmt.Errorf("client hello message is not 48 bytes")
	}

	// Decode the UUID
	uuid, err := uuid.FromBytes(message[:16])
	if err != nil {
		return nil, fmt.Errorf("failed to decode uuid: %w", err)
	}

	// Decode the public key
	publicKey := message[16:48]

	return &ClientHello{
		UUID:      uuid,
		PublicKey: publicKey,
	}, nil
}

func decodeEvent(key ed25519.PublicKey, message []byte) (*Event, error) {
	// Decode the sender
	sender, err := uuid.FromBytes(message[0:16])
	if err != nil {
		return nil, fmt.Errorf("failed to decode sender: %w", err)
	}

	// Decode the signature
	signature := message[16:80]

	// Verify the signature
	if !ed25519.Verify(key, message[80:], signature) {
		return nil, fmt.Errorf("failed to verify signature")
	}

	// Decode the room
	room, err := uuid.FromBytes(message[80:96])
	if err != nil {
		return nil, fmt.Errorf("failed to decode room: %w", err)
	}

	// Decode the event type length
	eventTypeLength := binary.BigEndian.Uint16(message[96:98])

	// Make sure the message is at least 98 + eventTypeLength bytes
	if len(message) < 98+int(eventTypeLength) {
		return nil, fmt.Errorf("message is too short")
	}

	// Decode the event type
	eventType := EventType(message[98 : 98+eventTypeLength])

	// Decode the payload
	payload := message[98+eventTypeLength:]

	return &Event{
		Sender:    sender,
		Room:      room,
		EventType: eventType,
		Payload:   payload,
	}, nil
}

func main() {
	manager := NewManager()
	router := mux.NewRouter()
	router.HandleFunc("/api/v1/health", manager.healthHandler)
	router.HandleFunc("/api/v1/socket", manager.socketHandler)
	fmt.Println("Starting server on port 8080...")
	err := http.ListenAndServe(":8080", router)
	if err != nil {
		log.Fatal("ListenAndServe: ", err)
	}
}
