package pkg

import (
	"crypto/ed25519"
	"encoding/binary"
	"fmt"

	"github.com/google/uuid"
)

const MinMessageLength = ed25519.SignatureSize + (16 * 2) + 2

type Message struct {
	Source       uuid.UUID
	Destination  uuid.UUID
	EventType    EventType
	EventPayload []byte
}

func decodeMessage(key ed25519.PublicKey, message []byte) (*Message, error) {
	// Make sure the message is at least 98 bytes
	if len(message) < MinMessageLength {
		return nil, fmt.Errorf("message is too short")
	}

	// Decode the signature
	signature := message[0:ed25519.SignatureSize]

	// Verify the signature
	if !ed25519.Verify(key, message[ed25519.SignatureSize:], signature) {
		return nil, fmt.Errorf("failed to verify signature")
	}

	// Decode the source
	offset := ed25519.SignatureSize
	length := 16
	source, err := uuid.FromBytes(message[offset : offset+length])
	offset += length
	if err != nil {
		return nil, fmt.Errorf("failed to decode source: %w", err)
	}

	// Decode the destination
	destination, err := uuid.FromBytes(message[offset : offset+length])
	offset += length
	if err != nil {
		return nil, fmt.Errorf("failed to decode destination: %w", err)
	}

	// Decode the message type length
	length = 2
	eventTypeLength := binary.BigEndian.Uint16(message[offset : offset+length])
	offset += length

	// Make sure the message is at least MinMessageLength + eventTypeLength bytes
	if len(message) < MinMessageLength+int(eventTypeLength) {
		return nil, fmt.Errorf("message is too short")
	}

	// Decode the message type
	eventType := EventType(message[offset : offset+int(eventTypeLength)])
	offset += int(eventTypeLength)

	// Decode the payload
	eventPayload := message[offset:]

	return &Message{
		Source:       source,
		Destination:  destination,
		EventType:    eventType,
		EventPayload: eventPayload,
	}, nil
}
