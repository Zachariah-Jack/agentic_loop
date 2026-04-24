package activity

import (
	"testing"
)

func TestBrokerReplaysHistoryAndStreamsLiveEvents(t *testing.T) {
	t.Parallel()

	broker := NewBroker(8)
	broker.Publish("run_started", map[string]any{"run_id": "run_1"})

	stream, cancel := broker.Subscribe(0)
	defer cancel()

	first := <-stream
	if first.Event != "run_started" {
		t.Fatalf("first.Event = %q, want run_started", first.Event)
	}

	broker.Publish("safe_point_reached", map[string]any{"run_id": "run_1"})
	second := <-stream
	if second.Event != "safe_point_reached" {
		t.Fatalf("second.Event = %q, want safe_point_reached", second.Event)
	}
}
