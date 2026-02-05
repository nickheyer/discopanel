package emit

// For publishing to WebSocket subscribers
type Emitter interface {
	Publish(topic string, payload []byte)
	HasSubscribers(topic string) bool
}

// Any service that accepts an Emitter
type EmittingService interface {
	SetEmitter(e Emitter)
}

// Sets the emitter on every provided service
func SetEmitters(e Emitter, svcs ...EmittingService) {
	for _, svc := range svcs {
		svc.SetEmitter(e)
	}
}
