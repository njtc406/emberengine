package serializer

type JsonMessage struct {
	TypeName string
	Json     string
}

type (
	// Ping is message sent by the actor system to probe an actor is started.
	Ping struct{}

	// Pong is response for ping.
	Pong struct{}
)
