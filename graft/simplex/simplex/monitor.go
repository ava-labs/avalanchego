// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simplex

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/ava-labs/simplex/common"
	"go.uber.org/zap"
)

type Monitor struct {
	logger     common.Logger
	close      chan struct{}
	time       atomic.Value
	ticks      chan time.Time
	tasks      chan func()
	cancelTask func()
	futureTask atomic.Value
	running    sync.WaitGroup
}

type futureTask struct {
	deadline time.Time
	f        func()
}

func NewMonitor(startTime time.Time, logger common.Logger) *Monitor {
	m := &Monitor{
		cancelTask: func() {},
		logger:     logger,
		close:      make(chan struct{}),
		tasks:      make(chan func(), 1),
		ticks:      make(chan time.Time, 1),
	}

	m.time.Store(startTime)

	m.running.Add(1)
	go m.run()

	return m
}

func (m *Monitor) AdvanceTime(t time.Time) {
	m.time.Store(t)
	select {
	case m.ticks <- t:
	default:
	}
}

func (m *Monitor) tick(now time.Time, taskID uint64) {
	defer m.logger.Verbo("Ticked", zap.Uint64("taskID", taskID), zap.Time("time", now))
	ft := m.futureTask.Load()
	if ft == nil {
		return
	}

	task := ft.(*futureTask)

	if task.f == nil || task.deadline.IsZero() || now.Before(task.deadline) {
		return
	}

	m.logger.Verbo("Executing f", zap.Uint64("taskID", taskID), zap.Time("deadline", task.deadline))
	task.f()
	m.logger.Verbo("Executed f", zap.Uint64("taskID", taskID), zap.Time("time", now), zap.Time("deadline", task.deadline))

	// clean up future task to mark we have already executed it and to release memory
	m.futureTask.Store(&futureTask{})
}

func (m *Monitor) run() {
	defer m.running.Done()
	var taskID uint64
	for m.shouldRun() {
		select {
		case <-m.close:
			return
		case tick := <-m.ticks:
			m.tick(tick, taskID)
		case f := <-m.tasks:
			m.logger.Verbo("Executing f", zap.Uint64("taskID", taskID))
			f()
			m.logger.Verbo("Task executed", zap.Uint64("taskID", taskID))
		}
		taskID++
	}
}

func (m *Monitor) shouldRun() bool {
	select {
	case <-m.close:
		return false
	default:
		return true
	}
}

func (m *Monitor) Close() {
	defer m.running.Wait()
	select {
	case <-m.close:
		return
	default:
		close(m.close)
	}
}

func (m *Monitor) CancelTask() {
	m.cancelTask()
	select {
	case <-m.tasks:
	default:

	}
}

func (m *Monitor) RunTask(f func()) bool {
	var cancelled atomic.Bool

	m.cancelTask = func() {
		cancelled.Store(true)
	}

	task := func() {
		if cancelled.Load() {
			return
		}
		f()
	}

	select {
	case m.tasks <- task:
		return true
	default:
		m.logger.Warn("Attempted to run a task but capacity was full")
		return false
	}
}

func (m *Monitor) CancelFutureTask() {
	m.futureTask.Store(&futureTask{})
}

func (m *Monitor) FutureTask(timeout time.Duration, f func()) {
	t := m.time.Load()
	now := t.(time.Time)
	deadline := now.Add(timeout)

	m.futureTask.Store(&futureTask{
		f:        f,
		deadline: deadline,
	})

	m.logger.Verbo("Scheduling task", zap.Duration("timeout", timeout), zap.Time("deadline", deadline))
}
