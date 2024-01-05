package pkg

type EventType string

const (
	EventTypeEnterRoom            EventType = "enter_room"
	EventTypeLeaveRoom                      = "leave_room"
	EventTypePublishToTopic                 = "publish_to_topic"
	EventTypeSubscribeToTopic               = "subscribe_to_topic"
	EventTypeUnsubscribeFromTopic           = "unsubscribe_from_topic"
)
