package pkg

import (
	"crypto/ed25519"
	"encoding/binary"
	"fmt"

	"github.com/google/uuid"
)

type Message struct {
	Source       uuid.UUID
	Destination  uuid.UUID
	EventType    EventType
	EventPayload []byte
}

func decodeMessage(key ed25519.PublicKey, message []byte) (*Message, error) {
	// Decode the source
	source, err := uuid.FromBytes(message[0:16])
	if err != nil {
		return nil, fmt.Errorf("failed to decode source: %w", err)
	}

	// Decode the signature
	signature := message[16:80]

	// Verify the signature
	if !ed25519.Verify(key, message[80:], signature) {
		return nil, fmt.Errorf("failed to verify signature")
	}

	// Decode the destination
	destination, err := uuid.FromBytes(message[80:96])
	if err != nil {
		return nil, fmt.Errorf("failed to decode destination: %w", err)
	}

	// Decode the message type length
	eventTypeLength := binary.BigEndian.Uint16(message[96:98])

	// Make sure the message is at least 98 + eventTypeLength bytes
	if len(message) < 98+int(eventTypeLength) {
		return nil, fmt.Errorf("message is too short")
	}

	// Decode the message type
	eventType := EventType(message[98 : 98+eventTypeLength])

	// Decode the payload
	eventPayload := message[98+eventTypeLength:]

	return &Message{
		Source:       source,
		Destination:  destination,
		EventType:    eventType,
		EventPayload: eventPayload,
	}, nil
}
