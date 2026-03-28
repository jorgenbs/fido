package api

import (
	"encoding/json"
	"testing"
	"time"
)

func TestHub_SubscribeReceivesPublishedEvents(t *testing.T) {
	hub := NewHub()
	ch := hub.Subscribe()
	defer hub.Unsubscribe(ch)

	hub.Publish(Event{Type: "issue:updated", Payload: map[string]any{"id": "issue-1"}})

	select {
	case evt := <-ch:
		if evt.Type != "issue:updated" {
			t.Errorf("Type: got %q, want issue:updated", evt.Type)
		}
		p, _ := json.Marshal(evt.Payload)
		if string(p) == "" {
			t.Error("expected non-empty payload")
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timed out waiting for event")
	}
}

func TestHub_UnsubscribeStopsDelivery(t *testing.T) {
	hub := NewHub()
	ch := hub.Subscribe()
	hub.Unsubscribe(ch)

	hub.Publish(Event{Type: "test", Payload: nil})

	select {
	case <-ch:
		t.Fatal("should not receive after unsubscribe")
	case <-time.After(50 * time.Millisecond):
		// expected
	}
}

func TestHub_MultipleSubscribers(t *testing.T) {
	hub := NewHub()
	ch1 := hub.Subscribe()
	ch2 := hub.Subscribe()
	defer hub.Unsubscribe(ch1)
	defer hub.Unsubscribe(ch2)

	hub.Publish(Event{Type: "scan:complete", Payload: map[string]any{"count": 3}})

	for _, ch := range []<-chan Event{ch1, ch2} {
		select {
		case evt := <-ch:
			if evt.Type != "scan:complete" {
				t.Errorf("Type: got %q, want scan:complete", evt.Type)
			}
		case <-time.After(100 * time.Millisecond):
			t.Fatal("timed out")
		}
	}
}
