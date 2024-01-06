package pkg

import (
	"crypto/ed25519"
	"fmt"
	"sync"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	log "github.com/sirupsen/logrus"
)

const SessionHelloSize = ed25519.PublicKeySize + (16 * 2)

type Session struct {
	manager *Manager
	client  *Client
	lock    sync.RWMutex
	uuid    uuid.UUID
	conn    *websocket.Conn
	send    chan []byte
	rooms   []*Room
	topics  []*Topic
}

type SessionHello struct {
	ClientPublicKey ed25519.PublicKey
	ClientUUID      uuid.UUID
	SessionUUID     uuid.UUID
}

func (s *Session) handleEnterRoom(message *Message) {
	room := s.manager.GetOrCreateRoom(message.Destination)

	room.lock.Lock()
	defer room.lock.Unlock()
	room.sessions = append(room.sessions, s)

	s.lock.Lock()
	defer s.lock.Unlock()
	s.rooms = append(s.rooms, room)
}

func (s *Session) handleLeaveRoom(message *Message) {
	room := s.manager.GetOrCreateRoom(message.Destination)

	room.lock.Lock()
	defer room.lock.Unlock()
	for i, session := range room.sessions {
		if session.uuid == message.Source {
			room.sessions = append(room.sessions[:i], room.sessions[i+1:]...)
			break
		}
	}

	s.lock.Lock()
	defer s.lock.Unlock()
	for i, room := range s.rooms {
		if room.uuid == message.Destination {
			s.rooms = append(s.rooms[:i], s.rooms[i+1:]...)
			break
		}
	}

	if len(room.sessions) == 0 {
		s.manager.lock.Lock()
		delete(s.manager.rooms, room.uuid)
		s.manager.lock.Unlock()
	}
}

func (s *Session) handleSubscribeToTopic(message *Message) {
	topic := s.manager.GetOrCreateTopic(message.Destination)
	topic.lock.Lock()
	defer topic.lock.Unlock()
	topic.consumer = s

	s.lock.Lock()
	defer s.lock.Unlock()
	s.topics = append(s.topics, topic)

	go func() {
		for messageData := range topic.queue {
			s.send <- messageData
		}
	}()
}

func (s *Session) handleUnsubscribeFromTopic(message *Message) {
	s.lock.Lock()
	defer s.lock.Unlock()
	for i, topic := range s.topics {
		if topic.uuid == message.Destination {
			s.topics = append(s.topics[:i], s.topics[i+1:]...)
			break
		}
	}

	topic := s.manager.GetTopic(message.Destination)
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

func (s *Session) handleMessage(messageData []byte) error {
	message, err := decodeMessage(s.client.key, messageData)
	if err != nil {
		return fmt.Errorf("failed to decode message: %w", err)
	}

	switch message.EventType {
	case EventTypeEnterRoom:
		s.handleEnterRoom(message)
	case EventTypeLeaveRoom:
		s.handleLeaveRoom(message)
	case EventTypeSubscribeToTopic:
		s.handleSubscribeToTopic(message)
	case EventTypeUnsubscribeFromTopic:
		s.handleUnsubscribeFromTopic(message)
	default:
		log.WithFields(log.Fields{
			"source":      message.Source,
			"destination": message.Destination,
			"event_type":  message.EventType,
		}).Info("Received message")

		// Broadcast the message to all sessions in the room
		room := s.manager.GetRoom(message.Destination)
		if room != nil {
			room.lock.RLock()
			defer room.lock.RUnlock()
			for _, session := range room.sessions {
				session.send <- messageData
			}
		}

		// Send the message to the topic consumer
		topic := s.manager.GetTopic(message.Destination)
		if topic != nil {
			topic.lock.RLock()
			defer topic.lock.RUnlock()
			topic.queue <- messageData
		}
	}

	return nil
}

func (s *Session) read() {
	defer close(s.send)

	for {
		_, message, err := s.conn.ReadMessage()
		if err != nil && !websocket.IsCloseError(err,
			websocket.CloseNormalClosure,
			websocket.CloseGoingAway) {
			log.Error("Failed to read message: ", err)
		}

		if err != nil {
			break
		}

		err = s.handleMessage(message)
		if err != nil && !websocket.IsCloseError(err,
			websocket.CloseNormalClosure) {
			log.Error("Failed to handle message: ", err)
			break
		}
	}
}

func (s *Session) write() {
	for {
		message := <-s.send
		if message == nil {
			break
		}

		err := s.conn.WriteMessage(websocket.BinaryMessage, message)
		if err != nil {
			log.Error("Failed to write message: ", err)
			break
		}
	}
}

func decodeSessionHello(message []byte) (*SessionHello, error) {
	var err error

	if len(message) < SessionHelloSize {
		return nil, fmt.Errorf("message too small")
	}

	var hello SessionHello

	offset := 0
	length := ed25519.PublicKeySize
	hello.ClientPublicKey = message[offset : offset+length]
	offset += length

	length = 16
	hello.ClientUUID, err = uuid.FromBytes(message[offset : offset+length])
	if err != nil {
		return nil, err
	}
	offset += length
	hello.SessionUUID, err = uuid.FromBytes(message[offset : offset+length])
	if err != nil {
		return nil, err
	}

	return &hello, nil
}
