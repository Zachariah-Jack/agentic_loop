package activity

import (
	"sync"
	"time"
)

const DefaultHistoryLimit = 256

type Event struct {
	Type     string         `json:"type"`
	Event    string         `json:"event"`
	Sequence int64          `json:"sequence"`
	At       time.Time      `json:"at"`
	Payload  map[string]any `json:"payload,omitempty"`
}

type Publisher interface {
	Publish(event string, payload map[string]any) Event
}

type Broker struct {
	mu           sync.Mutex
	nextSequence int64
	historyLimit int
	history      []Event
	subscribers  map[int]chan Event
	nextID       int
}

func NewBroker(historyLimit int) *Broker {
	if historyLimit <= 0 {
		historyLimit = DefaultHistoryLimit
	}
	return &Broker{
		historyLimit: historyLimit,
		subscribers:  make(map[int]chan Event),
	}
}

func (b *Broker) Publish(event string, payload map[string]any) Event {
	if b == nil {
		return Event{}
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	b.nextSequence++
	emitted := Event{
		Type:     "event",
		Event:    event,
		Sequence: b.nextSequence,
		At:       time.Now().UTC(),
		Payload:  payload,
	}

	b.history = append(b.history, emitted)
	if len(b.history) > b.historyLimit {
		b.history = append([]Event(nil), b.history[len(b.history)-b.historyLimit:]...)
	}

	for _, subscriber := range b.subscribers {
		select {
		case subscriber <- emitted:
		default:
		}
	}

	return emitted
}

func (b *Broker) Subscribe(fromSequence int64) (<-chan Event, func()) {
	if b == nil {
		ch := make(chan Event)
		close(ch)
		return ch, func() {}
	}

	ch := make(chan Event, 64)

	b.mu.Lock()
	for _, event := range b.history {
		if event.Sequence > fromSequence {
			ch <- event
		}
	}
	id := b.nextID
	b.nextID++
	b.subscribers[id] = ch
	b.mu.Unlock()

	cancel := func() {
		b.mu.Lock()
		subscriber, ok := b.subscribers[id]
		if ok {
			delete(b.subscribers, id)
		}
		b.mu.Unlock()
		if ok {
			close(subscriber)
		}
	}

	return ch, cancel
}
