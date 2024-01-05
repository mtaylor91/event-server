package pkg

import (
	"crypto/ed25519"
	"fmt"
	"sync"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	log "github.com/sirupsen/logrus"
)

type Client struct {
	manager *Manager
	lock    sync.RWMutex
	uuid    uuid.UUID
	key     ed25519.PublicKey
	conn    *websocket.Conn
	send    chan []byte
	rooms   []*Room
	topics  []*Topic
}

type ClientHello struct {
	UUID      uuid.UUID
	PublicKey ed25519.PublicKey
}

func (c *Client) handleEnterRoom(message *Message) {
	room := c.manager.GetOrCreateRoom(message.Destination)

	room.lock.Lock()
	defer room.lock.Unlock()
	room.clients = append(room.clients, c)

	c.lock.Lock()
	defer c.lock.Unlock()
	c.rooms = append(c.rooms, room)
}

func (c *Client) handleLeaveRoom(message *Message) {
	room := c.manager.GetOrCreateRoom(message.Destination)

	room.lock.Lock()
	defer room.lock.Unlock()
	for i, client := range room.clients {
		if client.uuid == message.Source {
			room.clients = append(room.clients[:i], room.clients[i+1:]...)
			break
		}
	}

	c.lock.Lock()
	defer c.lock.Unlock()
	for i, room := range c.rooms {
		if room.uuid == message.Destination {
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

func (c *Client) handlePublishToTopic(messageData []byte, message *Message) {
	topic := c.manager.GetOrCreateTopic(message.Destination)
	topic.lock.Lock()
	defer topic.lock.Unlock()
	topic.queue <- messageData
}

func (c *Client) handleSubscribeToTopic(message *Message) {
	topic := c.manager.GetOrCreateTopic(message.Destination)
	topic.lock.Lock()
	defer topic.lock.Unlock()
	topic.consumer = c

	c.lock.Lock()
	defer c.lock.Unlock()
	c.topics = append(c.topics, topic)

	go func() {
		for messageData := range topic.queue {
			c.send <- messageData
		}
	}()
}

func (c *Client) handleUnsubscribeFromTopic(message *Message) {
	c.lock.Lock()
	defer c.lock.Unlock()
	for i, topic := range c.topics {
		if topic.uuid == message.Destination {
			c.topics = append(c.topics[:i], c.topics[i+1:]...)
			break
		}
	}

	topic := c.manager.GetTopic(message.Destination)
	if topic == nil {
		return
	}

	topic.lock.Lock()
	defer topic.lock.Unlock()
	topic.consumer = nil

	if len(topic.queue) == 0 {
		close(topic.queue)
		topic.queue = make(chan []byte, 256)
	}
}

func (c *Client) handleMessage(messageData []byte) error {
	message, err := decodeMessage(c.key, messageData)
	if err != nil {
		return fmt.Errorf("failed to decode message: %w", err)
	}

	switch message.EventType {
	case EventTypeEnterRoom:
		c.handleEnterRoom(message)
	case EventTypeLeaveRoom:
		c.handleLeaveRoom(message)
	case EventTypePublishToTopic:
		c.handlePublishToTopic(messageData, message)
	case EventTypeSubscribeToTopic:
		c.handleSubscribeToTopic(message)
	case EventTypeUnsubscribeFromTopic:
		c.handleUnsubscribeFromTopic(message)
	default:
		// Broadcast the message to all clients in the room
		room := c.manager.GetRoom(message.Destination)
		if room != nil {
			room.lock.RLock()
			defer room.lock.RUnlock()
			for _, client := range room.clients {
				client.send <- messageData
			}
		}

		// Send the message to the topic consumer
		topic := c.manager.GetTopic(message.Destination)
		if topic != nil {
			topic.lock.RLock()
			defer topic.lock.RUnlock()
			topic.queue <- messageData
		}
	}

	return nil
}

func (c *Client) read() {
	defer close(c.send)

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
}

func (c *Client) write() {
	defer c.manager.DeleteClient(c)
	defer c.conn.Close()
	defer log.Info("Client disconnected: ", c.uuid)

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
