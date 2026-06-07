package mmbot

import (
	"context"
	"errors"
	"sync"
	"time"
)

type handlerJob struct {
	handler Handler
	ctx     *Context
}

type scheduler struct {
	ctx    context.Context
	cancel context.CancelFunc

	policy QueuePolicy
	slots  chan struct{}
	sem    chan struct{}

	mu      sync.Mutex
	stopped bool
	queues  map[string][]handlerJob
	running map[string]bool
	wg      sync.WaitGroup

	run func(handlerJob)
}

func newScheduler(parent context.Context, config Config, run func(handlerJob)) *scheduler {
	ctx, cancel := context.WithCancel(parent)
	return &scheduler{
		ctx:     ctx,
		cancel:  cancel,
		policy:  config.QueuePolicy,
		slots:   make(chan struct{}, config.HandlerQueueSize),
		sem:     make(chan struct{}, config.HandlerConcurrency),
		queues:  make(map[string][]handlerJob),
		running: make(map[string]bool),
		run:     run,
	}
}

func (s *scheduler) Context() context.Context { return s.ctx }

func (s *scheduler) Submit(ctx context.Context, channelID string, job handlerJob) error {
	if channelID == "" {
		channelID = "_"
	}

	switch s.policy {
	case QueuePolicyDropNewest:
		select {
		case s.slots <- struct{}{}:
		default:
			return ErrHandlerQueueFull
		}
	default:
		select {
		case s.slots <- struct{}{}:
		case <-ctx.Done():
			return ctx.Err()
		case <-s.ctx.Done():
			return s.ctx.Err()
		}
	}

	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		<-s.slots
		return context.Canceled
	}
	s.queues[channelID] = append(s.queues[channelID], job)
	if !s.running[channelID] {
		s.running[channelID] = true
		s.wg.Add(1)
		go s.runChannel(channelID)
	}
	s.mu.Unlock()
	return nil
}

func (s *scheduler) runChannel(channelID string) {
	defer s.wg.Done()

	for {
		select {
		case s.sem <- struct{}{}:
		case <-s.ctx.Done():
			s.discardChannel(channelID)
			return
		}

		s.mu.Lock()
		if s.stopped || len(s.queues[channelID]) == 0 {
			delete(s.running, channelID)
			s.mu.Unlock()
			<-s.sem
			return
		}
		job := s.queues[channelID][0]
		s.queues[channelID] = s.queues[channelID][1:]
		s.mu.Unlock()

		s.run(job)
		<-s.sem
		<-s.slots
	}
}

func (s *scheduler) discardChannel(channelID string) {
	s.mu.Lock()
	discarded := len(s.queues[channelID])
	delete(s.queues, channelID)
	delete(s.running, channelID)
	s.mu.Unlock()
	for range discarded {
		<-s.slots
	}
}

func (s *scheduler) Shutdown(timeout time.Duration) error {
	s.cancel()

	s.mu.Lock()
	s.stopped = true
	discarded := 0
	for channelID, queue := range s.queues {
		discarded += len(queue)
		delete(s.queues, channelID)
	}
	s.mu.Unlock()
	for range discarded {
		<-s.slots
	}

	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-done:
		return nil
	case <-timer.C:
		return ErrShutdownTimeout
	}
}

func isSubmitCancellation(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}
