// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simplex

import (
	"io"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest"
)

func TestMonitorDoubleClose(t *testing.T) {
	start := time.Now()
	mon := NewMonitor(start, makeLogger(t))
	require.True(t, mon.shouldRun())
	mon.Close()
	require.False(t, mon.shouldRun())
	mon.Close()
	require.False(t, mon.shouldRun())
}

func TestMonitorPrematureCancelTask(t *testing.T) {
	start := time.Now()

	ticked := make(chan struct{})

	mon := NewMonitor(start, makeLogger(t))
	defer mon.Close()
	mon.logger.(*testLogger).intercept(func(entry zapcore.Entry) error {
		if entry.Message == "Ticked" {
			ticked <- struct{}{}
		}
		return nil
	})

	t.Run("Cancelled future task does not fire", func(t *testing.T) {
		panic := func() {
			panic("test failed")
		}

		mon.FutureTask(time.Hour, panic)
		mon.CancelFutureTask()

		mon.AdvanceTime(start.Add(time.Hour))

		select {
		case <-ticked:
		case <-time.After(time.Minute):
			require.FailNow(t, "timed out waiting on tick")
		}
	})

	t.Run("Non-Cancelled future task fires", func(t *testing.T) {
		finish := make(chan struct{})

		mon.FutureTask(time.Hour, func() {
			close(finish)
		})

		mon.AdvanceTime(start.Add(time.Hour * 2))

		<-ticked
		<-finish
	})

	t.Run("Cancelled task does not fire", func(t *testing.T) {
		// Occupy the monitor goroutine, otherwise it can dequeue and start the task below
		// before we cancel it, and cancelling cannot stop a task that already started.
		busy := make(chan struct{})
		released := make(chan struct{})

		mon.RunTask(func() {
			close(busy)
			<-released
		})
		<-busy

		finish := make(chan struct{})

		mon.RunTask(func() {
			<-finish
			close(finish) // Test should panic if we have a double close
		})

		mon.CancelTask()
		close(released)

		close(finish)
	})
}

func TestMonitorAsyncWaitFor(t *testing.T) {
	start := time.Now()
	mon := NewMonitor(start, makeLogger(t))
	defer mon.Close()

	var wg sync.WaitGroup
	wg.Add(1)
	mon.RunTask(wg.Done)
	wg.Wait()
}

func TestMonitorAsyncWaitUntilWithWaitFor(t *testing.T) {
	start := time.Now()
	mon := NewMonitor(start, makeLogger(t))
	defer mon.Close()

	var wg sync.WaitGroup
	wg.Add(1)
	mon.FutureTask(10*time.Millisecond, wg.Done)
	mon.RunTask(func() {
		mon.AdvanceTime(start.Add(10 * time.Millisecond))
	})
	wg.Wait()
}

func TestMonitorAsyncWaitForWithNestedWaitUntil(t *testing.T) {
	start := time.Now()
	mon := NewMonitor(start, makeLogger(t))
	defer mon.Close()

	var wg sync.WaitGroup
	wg.Add(1)
	// FutureTask must be scheduled before AdvanceTime, otherwise the deadline
	// is computed from the advanced clock and the tick never satisfies it.
	mon.RunTask(func() {
		mon.FutureTask(10*time.Millisecond, wg.Done)
		mon.AdvanceTime(start.Add(10 * time.Millisecond))
	})
	wg.Wait()
}

type testLogger struct {
	*zap.Logger
}

func (tl *testLogger) Trace(msg string, fields ...zap.Field) {
	tl.Log(zapcore.DebugLevel, msg, fields...)
}

func (tl *testLogger) Verbo(msg string, fields ...zap.Field) {
	tl.Log(zapcore.DebugLevel, msg, fields...)
}

func (t *testLogger) intercept(hook func(entry zapcore.Entry) error) {
	logger := t.WithOptions(zap.Hooks(hook))
	t.Logger = logger
}

func makeLogger(t *testing.T) *testLogger {
	// Output goes through t.Log (shown only if the test fails) at INFO and above.
	// A discarding core keeps the level at debug so that hooks registered via intercept observe debug entries.
	logger := zaptest.NewLogger(t, zaptest.Level(zapcore.InfoLevel)).WithOptions(zap.WrapCore(func(core zapcore.Core) zapcore.Core {
		discard := zapcore.NewCore(zapcore.NewConsoleEncoder(zap.NewDevelopmentEncoderConfig()), zapcore.AddSync(io.Discard), zapcore.DebugLevel)
		return zapcore.NewTee(core, discard)
	}))
	return &testLogger{Logger: logger}
}
