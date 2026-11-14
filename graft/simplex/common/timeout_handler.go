// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"maps"
	"sync"
	"time"

	"go.uber.org/zap"
)

type timeoutRunner[T comparable] func(ids []T)

type TimeoutHandler[T comparable] struct {
	// helpful for logging
	name string
	// how often to run through the tasks
	runInterval time.Duration
	// function to run tasks
	taskRunner timeoutRunner[T]
	lock       sync.Mutex
	ticks      chan time.Time
	close      chan struct{}
	// maps id to a task
	tasks map[T]struct{}
	now   time.Time

	log     Logger
	running sync.WaitGroup
}

// NewTimeoutHandler returns a TimeoutHandler and starts a new goroutine that
// listens for ticks and executes TimeoutTasks.
func NewTimeoutHandler[T comparable](log Logger, name string, startTime time.Time, runInterval time.Duration, taskRunner timeoutRunner[T]) *TimeoutHandler[T] {
	t := &TimeoutHandler[T]{
		name:        name,
		now:         startTime,
		tasks:       make(map[T]struct{}),
		ticks:       make(chan time.Time, 1),
		close:       make(chan struct{}),
		runInterval: runInterval,
		taskRunner:  taskRunner,
		log:         log,
	}

	t.running.Add(1)
	go t.run(startTime)

	return t
}

func (t *TimeoutHandler[T]) run(startTime time.Time) {
	defer t.running.Done()

	lastTickTime := startTime

	for t.shouldRun() {
		select {
		case now := <-t.ticks:
			if now.Sub(lastTickTime) < t.runInterval {
				continue
			}
			lastTickTime = now

			// update the current time
			t.lock.Lock()
			t.now = now
			t.lock.Unlock()

			t.maybeRunTasks()
		case <-t.close:
			return
		}
	}
}

func (t *TimeoutHandler[T]) maybeRunTasks() {
	t.lock.Lock()
	ids := make([]T, 0, len(t.tasks))

	for id := range t.tasks {
		ids = append(ids, id)
	}
	t.lock.Unlock()

	if len(ids) == 0 {
		return
	}

	t.log.Debug("Running task ids", zap.Any("task ids", ids), zap.String("name", t.name))
	t.taskRunner(ids)
}

func (t *TimeoutHandler[T]) shouldRun() bool {
	select {
	case <-t.close:
		return false
	default:
		return true
	}
}

func (t *TimeoutHandler[T]) Tick(now time.Time) {
	select {
	case t.ticks <- now:
		t.lock.Lock()
		t.now = now
		t.lock.Unlock()
	default:
		t.log.Debug("Dropping tick in timeouthandler", zap.String("name", t.name))
	}
}

func (t *TimeoutHandler[T]) AddTask(id T) {
	t.lock.Lock()
	defer t.lock.Unlock()

	t.tasks[id] = struct{}{}
	t.log.Debug("Adding timeout task", zap.Any("id", id), zap.String("name", t.name))
}

func (t *TimeoutHandler[T]) RemoveTask(ID T) {
	t.lock.Lock()
	defer t.lock.Unlock()

	if _, ok := t.tasks[ID]; !ok {
		return
	}

	t.log.Debug("Removing timeout task", zap.Any("id", ID), zap.String("name", t.name))
	delete(t.tasks, ID)
}

func (t *TimeoutHandler[T]) Empty() bool {
	t.lock.Lock()
	defer t.lock.Unlock()

	return len(t.tasks) == 0
}

func (t *TimeoutHandler[T]) RemoveOldTasks(shouldRemove func(id T, _ struct{}) bool) {
	t.lock.Lock()
	defer t.lock.Unlock()

	maps.DeleteFunc(t.tasks, shouldRemove)
}

func (t *TimeoutHandler[T]) Close() {
	defer t.running.Wait()
	select {
	case <-t.close:
		return
	default:
		close(t.close)
	}
}
