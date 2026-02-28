// Copyright 2026 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package hub

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/ptone/scion-agent/pkg/store"
)

// EventHandler processes a one-shot scheduled event of a specific type.
type EventHandler func(ctx context.Context, evt store.ScheduledEvent) error

// Scheduler manages recurring and one-shot timers within the Hub server.
// A single root ticker fires every 1 minute and drives all registered
// recurring handlers based on their configured interval.
//
// One-shot timers are persisted in the database and scheduled in memory
// via time.AfterFunc. On startup, expired timers fire immediately; future
// timers are scheduled for their fire_at time.
//
// All recurring handlers must be registered via RegisterRecurring before
// Start is called. RegisterRecurring is not safe for concurrent use.
type Scheduler struct {
	// Store for persisting one-shot events
	store store.Store

	// Root ticker interval
	tickInterval time.Duration

	// Recurring handlers
	recurring []RecurringHandler

	// Event type handlers for one-shot events
	eventHandlers map[string]EventHandler

	// Tick counter (monotonically increasing)
	tickCount uint64

	// One-shot timers (in-memory)
	mu     sync.Mutex
	timers map[string]*scheduledTimer

	// Lifecycle
	stopCh chan struct{}
	wg     sync.WaitGroup
}

// RecurringHandler defines a periodic task driven by the root ticker.
type RecurringHandler struct {
	Name     string                    // Human-readable name for logging
	Interval int                       // Run every N ticks (must be >= 1)
	Fn       func(ctx context.Context) // The work to perform
}

// scheduledTimer wraps a time.Timer with metadata for one-shot events.
type scheduledTimer struct {
	ID     string
	Timer  *time.Timer
	FireAt time.Time
	Cancel context.CancelFunc
}

// NewScheduler creates a new Scheduler with a 1-minute root ticker interval.
func NewScheduler(st store.Store) *Scheduler {
	return &Scheduler{
		store:         st,
		tickInterval:  1 * time.Minute,
		timers:        make(map[string]*scheduledTimer),
		eventHandlers: make(map[string]EventHandler),
		stopCh:        make(chan struct{}),
	}
}

// RegisterEventHandler registers a handler for a specific event type.
// Must be called before Start(). Not safe for concurrent use.
func (s *Scheduler) RegisterEventHandler(eventType string, handler EventHandler) {
	s.eventHandlers[eventType] = handler
}

// RegisterRecurring registers a recurring handler that runs every intervalMinutes
// minutes. All handlers must be registered before Start is called.
//
// Tick-Zero Behavior: All recurring handlers run immediately on startup (tick 0)
// because 0 % N == 0 for any interval N. This is intentional.
func (s *Scheduler) RegisterRecurring(name string, intervalMinutes int, fn func(ctx context.Context)) {
	if intervalMinutes < 1 {
		intervalMinutes = 1
	}
	s.recurring = append(s.recurring, RecurringHandler{
		Name:     name,
		Interval: intervalMinutes,
		Fn:       fn,
	})
}

// Start begins the root ticker loop and runs eligible handlers immediately
// on startup (tick 0). The provided context is used as the parent for handler
// invocations. Before starting the ticker, persisted one-shot timers are
// loaded from the database.
func (s *Scheduler) Start(ctx context.Context) {
	// Load and schedule persisted one-shot timers
	s.loadPersistedTimers(ctx)

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()

		ticker := time.NewTicker(s.tickInterval)
		defer ticker.Stop()

		// Run eligible handlers immediately on startup (tick 0).
		// All handlers fire at tick 0 because 0 % N == 0 for any interval.
		s.runRecurringHandlers(ctx)

		for {
			select {
			case <-ctx.Done():
				return
			case <-s.stopCh:
				return
			case <-ticker.C:
				s.tickCount++
				s.runRecurringHandlers(ctx)
			}
		}
	}()
}

// Stop signals the scheduler to stop, cancels all pending one-shot timers,
// and waits for the root ticker goroutine to exit. In-flight handler
// goroutines are not tracked; they will be cancelled via the parent context
// when the server shuts down.
func (s *Scheduler) Stop() {
	close(s.stopCh)

	// Cancel all one-shot timers
	s.mu.Lock()
	for _, st := range s.timers {
		st.Timer.Stop()
		if st.Cancel != nil {
			st.Cancel()
		}
	}
	s.timers = make(map[string]*scheduledTimer)
	s.mu.Unlock()

	s.wg.Wait()
}

// runRecurringHandlers invokes all handlers whose interval divides the current
// tick count. Each handler runs in its own goroutine with a timeout context.
func (s *Scheduler) runRecurringHandlers(ctx context.Context) {
	for _, h := range s.recurring {
		if s.tickCount%uint64(h.Interval) == 0 {
			handler := h // capture loop variable
			go func() {
				handlerCtx, cancel := context.WithTimeout(ctx, 55*time.Second)
				defer cancel()

				start := time.Now()
				slog.Debug("Scheduler: running recurring handler", "name", handler.Name, "tick", s.tickCount)

				func() {
					defer func() {
						if r := recover(); r != nil {
							slog.Error("Scheduler: recurring handler panicked",
								"name", handler.Name, "panic", r)
						}
					}()
					handler.Fn(handlerCtx)
				}()

				slog.Debug("Scheduler: recurring handler completed",
					"name", handler.Name, "duration", time.Since(start))
			}()
		}
	}
}

// =============================================================================
// One-Shot Timer Methods
// =============================================================================

// loadPersistedTimers loads all pending events from the database on startup.
// Events whose fire_at is in the past are executed immediately with status
// "expired". Future events are scheduled in memory.
func (s *Scheduler) loadPersistedTimers(ctx context.Context) {
	if s.store == nil {
		return
	}

	events, err := s.store.ListPendingScheduledEvents(ctx)
	if err != nil {
		slog.Error("Scheduler: failed to load pending events", "error", err)
		return
	}

	now := time.Now()
	var expiredCount, scheduledCount int

	for _, evt := range events {
		if evt.FireAt.Before(now) || evt.FireAt.Equal(now) {
			// Expired while Hub was down — execute immediately
			expiredCount++
			go s.fireEvent(ctx, evt, true)
		} else {
			// Schedule for the future
			scheduledCount++
			s.scheduleTimer(ctx, evt)
		}
	}

	if expiredCount > 0 || scheduledCount > 0 {
		slog.Info("Scheduler: loaded persisted events",
			"expired", expiredCount, "scheduled", scheduledCount)
	}
}

// scheduleTimer creates a time.AfterFunc timer for the given event and tracks
// it in the in-memory timer map.
func (s *Scheduler) scheduleTimer(ctx context.Context, evt store.ScheduledEvent) {
	delay := time.Until(evt.FireAt)
	if delay < 0 {
		delay = 0
	}

	timerCtx, cancel := context.WithCancel(ctx)

	timer := time.AfterFunc(delay, func() {
		defer cancel()
		s.fireEvent(timerCtx, evt, false)
		s.mu.Lock()
		delete(s.timers, evt.ID)
		s.mu.Unlock()
	})

	s.mu.Lock()
	s.timers[evt.ID] = &scheduledTimer{
		ID:     evt.ID,
		Timer:  timer,
		FireAt: evt.FireAt,
		Cancel: cancel,
	}
	s.mu.Unlock()
}

// fireEvent executes the event handler with panic recovery and updates the
// database status. wasExpired indicates the timer was past its fire_at when
// loaded on startup.
func (s *Scheduler) fireEvent(ctx context.Context, evt store.ScheduledEvent, wasExpired bool) {
	handlerCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	status := store.ScheduledEventFired
	if wasExpired {
		status = store.ScheduledEventExpired
	}

	var errMsg string
	func() {
		defer func() {
			if r := recover(); r != nil {
				errMsg = fmt.Sprintf("handler panicked: %v", r)
				slog.Error("Scheduler: event handler panicked",
					"eventID", evt.ID, "type", evt.EventType, "panic", r)
			}
		}()

		if err := s.executeEvent(handlerCtx, evt); err != nil {
			errMsg = err.Error()
			slog.Warn("Scheduler: event handler failed",
				"eventID", evt.ID, "type", evt.EventType, "error", err)
		} else {
			slog.Info("Scheduler: event fired",
				"eventID", evt.ID, "type", evt.EventType, "wasExpired", wasExpired)
		}
	}()

	now := time.Now()
	if s.store != nil {
		_ = s.store.UpdateScheduledEventStatus(ctx, evt.ID, status, &now, errMsg)
	}
}

// executeEvent dispatches the event to the appropriate handler based on its
// EventType. Unknown event types return an error.
func (s *Scheduler) executeEvent(ctx context.Context, evt store.ScheduledEvent) error {
	handler, ok := s.eventHandlers[evt.EventType]
	if !ok {
		return fmt.Errorf("unknown event type: %s", evt.EventType)
	}
	return handler(ctx, evt)
}

// ScheduleEvent creates a new one-shot scheduled event. The event is persisted
// to the database first, then scheduled in memory.
func (s *Scheduler) ScheduleEvent(ctx context.Context, evt store.ScheduledEvent) error {
	if s.store == nil {
		return fmt.Errorf("scheduler has no store configured")
	}

	// Persist to database first
	if err := s.store.CreateScheduledEvent(ctx, &evt); err != nil {
		return err
	}

	// Schedule in memory
	s.scheduleTimer(ctx, evt)

	slog.Info("Scheduler: event scheduled",
		"eventID", evt.ID, "type", evt.EventType, "fireAt", evt.FireAt)
	return nil
}

// CancelEvent cancels a pending scheduled event. The in-memory timer is
// stopped and the database record is marked as cancelled.
func (s *Scheduler) CancelEvent(ctx context.Context, id string) error {
	s.mu.Lock()
	if st, ok := s.timers[id]; ok {
		st.Timer.Stop()
		if st.Cancel != nil {
			st.Cancel()
		}
		delete(s.timers, id)
	}
	s.mu.Unlock()

	if s.store == nil {
		return fmt.Errorf("scheduler has no store configured")
	}

	return s.store.CancelScheduledEvent(ctx, id)
}
