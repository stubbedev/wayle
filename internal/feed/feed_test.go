package feed

import (
	"context"
	"testing"
)

func TestPublishReachesEverySubscriber(t *testing.T) {
	f := New[int](4)
	a, _ := f.Subscribe()
	b, _ := f.Subscribe()
	f.Publish(7)
	if <-a != 7 || <-b != 7 {
		t.Fatal("a subscriber missed the value")
	}
}

func TestAFullSubscriberDropsInsteadOfBlocking(t *testing.T) {
	f := New[int](1)
	ch, _ := f.Subscribe()
	f.Publish(1)
	f.Publish(2) // must not block on the full buffer
	if v := <-ch; v != 1 {
		t.Errorf("got %d, want the buffered first value", v)
	}
	select {
	case v := <-ch:
		t.Errorf("the dropped value %d arrived", v)
	default:
	}
}

func TestTicksCoalesce(t *testing.T) {
	f := NewTick()
	ch, _ := f.Subscribe()
	Notify(f)
	Notify(f)
	<-ch
	select {
	case <-ch:
		t.Error("two pending ticks did not coalesce")
	default:
	}
}

func TestStopClosesAndUnregisters(t *testing.T) {
	f := NewTick()
	ch, stop := f.Subscribe()
	stop()
	stop() // idempotent
	if _, open := <-ch; open {
		t.Error("the channel is still open after stop")
	}
	if f.Len() != 0 {
		t.Errorf("subscribers = %d after stop", f.Len())
	}
	Notify(f) // publishing past a stopped reader is harmless
}

func TestCloseEndsEveryReaderAndLaterSubscriptions(t *testing.T) {
	f := NewTick()
	ch, _ := f.Subscribe()
	f.Close()
	if _, open := <-ch; open {
		t.Error("Close left a channel open")
	}
	late, _ := f.Subscribe()
	if _, open := <-late; open {
		t.Error("a subscription after Close is open")
	}
}

func TestTheZeroValueIsACoalescingFeed(t *testing.T) {
	var f Tick
	ch, stop := f.Subscribe()
	defer stop()
	Notify(&f)
	Notify(&f)
	<-ch
	select {
	case <-ch:
		t.Error("the zero feed did not coalesce")
	default:
	}
}

func TestSubscribeContextClosesWithTheContext(t *testing.T) {
	var f Tick
	ctx, cancel := context.WithCancel(context.Background())
	ch := f.SubscribeContext(ctx)
	cancel()
	for range ch { // ends once the channel closes
	}
	if f.Len() != 0 {
		t.Errorf("subscribers = %d after the context ended", f.Len())
	}
}
