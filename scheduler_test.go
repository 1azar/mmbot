package mmbot

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestSchedulerPreservesChannelOrder(t *testing.T) {
	t.Parallel()

	config := schedulerConfig()
	var mu sync.Mutex
	var values []int
	done := make(chan struct{}, 3)
	scheduler := newScheduler(context.Background(), config, func(job handlerJob) {
		value := job.ctx.Args()[0]
		if value == "1" {
			time.Sleep(10 * time.Millisecond)
		}
		mu.Lock()
		values = append(values, int(value[0]-'0'))
		mu.Unlock()
		done <- struct{}{}
	})
	for i := 1; i <= 3; i++ {
		ctx := NewContext(context.Background(), nil, ContextInput{Args: []string{itoa(i)}})
		if err := scheduler.Submit(context.Background(), "channel", handlerJob{ctx: ctx}); err != nil {
			t.Fatal(err)
		}
	}
	for range 3 {
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("jobs did not complete")
		}
	}
	if err := scheduler.Shutdown(time.Second); err != nil {
		t.Fatal(err)
	}
	want := []int{1, 2, 3}
	for i := range want {
		if values[i] != want[i] {
			t.Fatalf("order = %#v", values)
		}
	}
}

func TestSchedulerRunsChannelsConcurrently(t *testing.T) {
	t.Parallel()

	config := schedulerConfig()
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	scheduler := newScheduler(context.Background(), config, func(handlerJob) {
		started <- struct{}{}
		<-release
	})
	if err := scheduler.Submit(context.Background(), "a", handlerJob{}); err != nil {
		t.Fatal(err)
	}
	if err := scheduler.Submit(context.Background(), "b", handlerJob{}); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("channels did not run concurrently")
		}
	}
	close(release)
	if err := scheduler.Shutdown(time.Second); err != nil {
		t.Fatal(err)
	}
}

func TestSchedulerDropAndBlockPolicies(t *testing.T) {
	t.Parallel()

	config := schedulerConfig()
	config.HandlerQueueSize = 1
	config.QueuePolicy = QueuePolicyDropNewest
	release := make(chan struct{})
	scheduler := newScheduler(context.Background(), config, func(handlerJob) { <-release })
	if err := scheduler.Submit(context.Background(), "a", handlerJob{}); err != nil {
		t.Fatal(err)
	}
	if err := scheduler.Submit(context.Background(), "b", handlerJob{}); !errors.Is(err, ErrHandlerQueueFull) {
		t.Fatalf("expected full queue, got %v", err)
	}
	close(release)
	if err := scheduler.Shutdown(time.Second); err != nil {
		t.Fatal(err)
	}

	config.QueuePolicy = QueuePolicyBlock
	started := make(chan struct{})
	scheduler = newScheduler(context.Background(), config, func(handlerJob) {
		close(started)
		<-time.After(time.Second)
	})
	if err := scheduler.Submit(context.Background(), "a", handlerJob{}); err != nil {
		t.Fatal(err)
	}
	<-started
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := scheduler.Submit(ctx, "b", handlerJob{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}
	if err := scheduler.Shutdown(time.Millisecond); !errors.Is(err, ErrShutdownTimeout) {
		t.Fatalf("expected shutdown timeout, got %v", err)
	}
}

func TestSchedulerConcurrencyLimit(t *testing.T) {
	t.Parallel()

	config := schedulerConfig()
	config.HandlerConcurrency = 2
	var active atomic.Int32
	var maximum atomic.Int32
	release := make(chan struct{})
	scheduler := newScheduler(context.Background(), config, func(handlerJob) {
		current := active.Add(1)
		for {
			old := maximum.Load()
			if current <= old || maximum.CompareAndSwap(old, current) {
				break
			}
		}
		<-release
		active.Add(-1)
	})
	for i := 0; i < 4; i++ {
		if err := scheduler.Submit(context.Background(), itoa(i), handlerJob{}); err != nil {
			t.Fatal(err)
		}
	}
	time.Sleep(20 * time.Millisecond)
	if maximum.Load() != 2 {
		t.Fatalf("maximum concurrency = %d", maximum.Load())
	}
	close(release)
	if err := scheduler.Shutdown(time.Second); err != nil {
		t.Fatal(err)
	}
}

func schedulerConfig() Config {
	return Config{
		HandlerConcurrency: 4,
		HandlerQueueSize:   16,
		QueuePolicy:        QueuePolicyBlock,
	}
}
