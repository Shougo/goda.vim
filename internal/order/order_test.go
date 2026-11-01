package order

import (
	"context"
	"math/rand/v2"
	"slices"
	"sync"
	"testing"
	"time"
)

// Notifications are handled in order, however long each takes.
func TestNotificationsInOrder(t *testing.T) {
	s := New()
	ctx := context.Background()
	var mu sync.Mutex
	var got []int
	var wg sync.WaitGroup
	for i := range 50 {
		f := s.Notification(ctx, func() {
			time.Sleep(time.Duration(rand.IntN(200)) * time.Microsecond)
			mu.Lock()
			got = append(got, i)
			mu.Unlock()
		})
		wg.Go(f)
	}
	wg.Wait()
	want := make([]int, 50)
	for i := range want {
		want[i] = i
	}
	if !slices.Equal(got, want) {
		t.Errorf("got %v", got)
	}
}

// A request waits for the notifications before it, not for those after it;
// requests run side by side.
func TestRequestAfterNotifications(t *testing.T) {
	s := New()
	ctx := context.Background()
	release := make(chan struct{})
	var notified sync.WaitGroup
	notified.Add(1)
	n := s.Notification(ctx, func() { <-release; notified.Done() })
	var order []string
	var mu sync.Mutex
	add := func(x string) { mu.Lock(); order = append(order, x); mu.Unlock() }
	r1 := s.Request(ctx, func() { add("r1") })
	later := s.Notification(ctx, func() { add("n2") })
	r2done := make(chan struct{})
	// Not ordered after r1: runs while r1 waits for n.
	go s.Request(ctx, func() {})()
	go func() { n() }()
	go func() { r1(); close(r2done) }()
	go later()
	time.Sleep(10 * time.Millisecond)
	mu.Lock()
	if len(order) != 0 {
		t.Errorf("ran before the notification: %v", order)
	}
	mu.Unlock()
	close(release)
	notified.Wait()
	<-r2done
	time.Sleep(10 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if len(order) != 2 || !slices.Contains(order, "r1") || !slices.Contains(order, "n2") {
		t.Errorf("order %v", order)
	}
}

// Closing: nothing waits.
func TestCanceled(t *testing.T) {
	s := New()
	ctx, cancel := context.WithCancel(context.Background())
	s.Notification(ctx, func() {}) // never run: the next one would wait for it
	ran := make(chan struct{})
	go s.Request(ctx, func() { close(ran) })()
	cancel()
	select {
	case <-ran:
	case <-time.After(time.Second):
		t.Fatal("waited")
	}
}
