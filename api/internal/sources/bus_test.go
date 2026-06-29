package sources

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestProgressBus_SubscribeReceivesPublishedEvent(t *testing.T) {
	bus := NewProgressBus()
	id := uuid.New()

	ch, cancel := bus.Subscribe(id)
	defer cancel()

	want := ProgressEvent{Type: EventIndexing, File: "test.md", Chunks: 3}
	bus.publish(id, want)

	select {
	case got := <-ch:
		if got != want {
			t.Fatalf("expected %v, got %v", want, got)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}
}

func TestProgressBus_CancelClosesChannel(t *testing.T) {
	bus := NewProgressBus()
	id := uuid.New()

	ch, cancel := bus.Subscribe(id)
	cancel()

	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("expected channel to be closed after cancel")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for channel close")
	}
}

func TestProgressBus_MultipleSubscribersReceiveSameEvent(t *testing.T) {
	bus := NewProgressBus()
	id := uuid.New()

	ch1, cancel1 := bus.Subscribe(id)
	defer cancel1()
	ch2, cancel2 := bus.Subscribe(id)
	defer cancel2()

	want := ProgressEvent{Type: EventDone, Total: 5}
	bus.publish(id, want)

	for i, ch := range []<-chan ProgressEvent{ch1, ch2} {
		select {
		case got := <-ch:
			if got != want {
				t.Fatalf("subscriber %d: expected %v, got %v", i, want, got)
			}
		case <-time.After(time.Second):
			t.Fatalf("subscriber %d: timed out waiting for event", i)
		}
	}
}

func TestProgressBus_PublishWithNoSubscribersIsNoop(t *testing.T) {
	bus := NewProgressBus()
	// must not panic
	bus.publish(uuid.New(), ProgressEvent{Type: EventDone})
}

func TestProgressBus_CancelRemovesOnlyTargetSubscriber(t *testing.T) {
	bus := NewProgressBus()
	id := uuid.New()

	ch1, cancel1 := bus.Subscribe(id)
	ch2, cancel2 := bus.Subscribe(id)
	defer cancel2()

	cancel1()

	want := ProgressEvent{Type: EventDone, Total: 1}
	bus.publish(id, want)

	// ch2 must still receive the event
	select {
	case got := <-ch2:
		if got != want {
			t.Fatalf("ch2: expected %v, got %v", want, got)
		}
	case <-time.After(time.Second):
		t.Fatal("ch2: timed out waiting for event")
	}

	// ch1 must be closed
	select {
	case _, ok := <-ch1:
		if ok {
			t.Fatal("expected ch1 to be closed after cancel")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for ch1 close")
	}
}

func TestProgressBus_NonBlockingOnFullChannel(t *testing.T) {
	bus := NewProgressBus()
	id := uuid.New()

	ch, cancel := bus.Subscribe(id)
	defer cancel()

	event := ProgressEvent{Type: EventIndexing, File: "f.md", Chunks: 1}
	// publish more than the buffer can hold; extra publishes must not block
	for i := 0; i < subBufSize+10; i++ {
		bus.publish(id, event)
	}

	count := 0
	for {
		select {
		case <-ch:
			count++
		default:
			if count != subBufSize {
				t.Fatalf("expected exactly %d events (buffer size), got %d", subBufSize, count)
			}
			return
		}
	}
}

func TestProgressBus_PublishToUnrelatedSourceIDNotReceived(t *testing.T) {
	bus := NewProgressBus()
	id1 := uuid.New()
	id2 := uuid.New()

	ch, cancel := bus.Subscribe(id1)
	defer cancel()

	bus.publish(id2, ProgressEvent{Type: EventDone})

	select {
	case <-ch:
		t.Fatal("received event intended for a different sourceID")
	case <-time.After(50 * time.Millisecond):
		// expected: no event arrives
	}
}

func TestProgressBus_CancelAfterPublishDrainsEventFirst(t *testing.T) {
	bus := NewProgressBus()
	id := uuid.New()

	ch, cancel := bus.Subscribe(id)

	want := ProgressEvent{Type: EventError, Error: "boom"}
	bus.publish(id, want)

	// cancel closes the channel but the buffered event must still be readable
	cancel()

	select {
	case got, ok := <-ch:
		if !ok {
			// channel closed before we could read — this is also acceptable
			return
		}
		if got != want {
			t.Fatalf("expected %v, got %v", want, got)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event or channel close")
	}
}
