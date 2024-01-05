package pkg

type EventType string

const (
	EventTypeEnterRoom            EventType = "enter_room"
	EventTypeLeaveRoom                      = "leave_room"
	EventTypeSubscribeToTopic               = "subscribe_to_topic"
	EventTypeUnsubscribeFromTopic           = "unsubscribe_from_topic"
)
