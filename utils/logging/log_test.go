// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package logging

import (
	"bytes"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func TestLog(t *testing.T) {
	log := NewLogger("", NewWrappedCore(Info, Discard, Plain.ConsoleEncoder()))

	recovered := new(bool)
	panicFunc := func() {
		panic("DON'T PANIC!")
	}
	exitFunc := func() {
		*recovered = true
	}
	log.RecoverAndExit(panicFunc, exitFunc)

	require.True(t, *recovered)
}

func TestAmplifyRollbackDifferentLoggers(t *testing.T) {
	var testBuffer bytes.Buffer

	factory := NewFactory(Config{
		RotatingWriterConfig: RotatingWriterConfig{
			Directory: t.TempDir(),
		},
		LoggingAutoAmplificationMaxDuration: 5 * time.Minute,
		LoggingAutoAmplification:            true,
		DisplayLevel:                        Info,
		LogLevel:                            Info,
	})

	defer factory.Close()

	red, err := factory.Make("red")
	require.NoError(t, err)

	blue, err := factory.Make("blue")
	require.NoError(t, err)

	require.NoError(t, factory.SetDisplayLevel("red", Error))
	require.NoError(t, factory.SetDisplayLevel("blue", Trace))

	internalLogger := red.(*log).internalLogger
	red.(*log).internalLogger = internalLogger.WithOptions(zap.Hooks(func(entry zapcore.Entry) error {
		testBuffer.WriteString(entry.Message)
		return nil
	}))

	internalLogger = blue.(*log).internalLogger
	blue.(*log).internalLogger = internalLogger.WithOptions(zap.Hooks(func(entry zapcore.Entry) error {
		testBuffer.WriteString(entry.Message)
		return nil
	}))

	assertDefaultLogLevels := func() {
		testBuffer.Reset()
		defer testBuffer.Reset()

		blue.Debug("blue")
		red.Debug("red")
		require.Empty(t, testBuffer.Bytes())

		blue.Trace("blue")
		red.Trace("red")
		require.Equal(t, "blue", testBuffer.String())

		red.Error("red")
		require.Equal(t, "bluered", testBuffer.String())
	}

	assertDefaultLogLevels()

	factory.Amplify()

	red.Debug("red")
	blue.Debug("blue")
	require.Equal(t, "redblue", testBuffer.String())

	factory.Revert()

	assertDefaultLogLevels()
}

func TestRevertTwiceIsNoOp(t *testing.T) {
	factory := NewFactory(Config{
		RotatingWriterConfig: RotatingWriterConfig{
			Directory: t.TempDir(),
		},
		LoggingAutoAmplificationMaxDuration: 5 * time.Minute,
		LoggingAutoAmplification:            true,
		DisplayLevel:                        Info,
	})
	defer factory.Close()

	logger, err := factory.Make("test")
	require.NoError(t, err)

	var testBuffer bytes.Buffer
	internalLogger := logger.(*log).internalLogger
	logger.(*log).internalLogger = internalLogger.WithOptions(zap.Hooks(func(entry zapcore.Entry) error {
		testBuffer.WriteString(entry.Message)
		return nil
	}))

	t.Run("does not log in debug at first", func(t *testing.T) {
		logger.Debug("test")
		require.Zero(t, testBuffer.Len())
	})

	t.Run("logs in debug when amplified", func(t *testing.T) {
		factory.Amplify()
		logger.Debug("test")
		require.Equal(t, "test", testBuffer.String())
		testBuffer.Reset()
	})

	t.Run("does not log in debug when reverted back to the default", func(t *testing.T) {
		factory.Revert()
		logger.Debug("test")
		require.Zero(t, testBuffer.Len())
		testBuffer.Reset()
	})

	t.Run("Revert the second time is a no-op", func(t *testing.T) {
		logger.SetLevel(Debug) // we set the logging level directly which doesn't change the default level
		factory.Revert()
		logger.Debug("test")
		require.Equal(t, "test", testBuffer.String())
		logger.SetLevel(Info)
		testBuffer.Reset()
	})

	t.Run("Further amplification after a double revert works as expected", func(t *testing.T) {
		factory.Amplify()
		logger.Debug("test")
		require.Equal(t, "test", testBuffer.String())
		testBuffer.Reset()
		factory.Revert()
		logger.Debug("test")
		require.Zero(t, testBuffer.Len())
	})
}

func TestAmplifyRollback(t *testing.T) {
	var testBuffer bytes.Buffer

	factory := NewFactory(Config{
		RotatingWriterConfig: RotatingWriterConfig{
			Directory: t.TempDir(),
		},
		LoggingAutoAmplificationMaxDuration: 5 * time.Minute,
		LoggingAutoAmplification:            true,
		DisplayLevel:                        Info,
	})

	defer factory.Close()

	logger, err := factory.Make("test")
	require.NoError(t, err)

	// Write all logs into our test buffer
	internalLogger := logger.(*log).internalLogger
	logger.(*log).internalLogger = internalLogger.WithOptions(zap.Hooks(func(entry zapcore.Entry) error {
		testBuffer.WriteString(entry.Message)
		return nil
	}))

	t.Run("default info level doesn't log debug", func(t *testing.T) {
		logger.Debug("debug")
		require.Empty(t, testBuffer.Bytes())

		logger.Info("info")
		require.Equal(t, "info", testBuffer.String())
	})

	t.Run("amplified logger logs in debug", func(t *testing.T) {
		testBuffer.Reset()

		factory.Amplify()

		logger.Info("info")
		require.Equal(t, "info", testBuffer.String())

		testBuffer.Reset()

		logger.Debug("debug")
		require.Equal(t, "debug", testBuffer.String())
	})

	t.Run("amplified logger can be reverted to its default", func(t *testing.T) {
		testBuffer.Reset()

		factory.Revert()

		logger.Debug("debug")
		require.Empty(t, testBuffer.Bytes())

		logger.Info("info")
		require.Equal(t, "info", testBuffer.String())
	})

	t.Run("amplified logger logs in debug until exhausted, reverts the logging level but can no longer be amplified", func(t *testing.T) {
		testBuffer.Reset()

		currentTime := time.Now()

		factory.amplificationTracker.now = func() time.Time {
			return currentTime
		}

		factory.Amplify()

		logger.Info("info")
		require.Equal(t, "info", testBuffer.String())

		testBuffer.Reset()

		logger.Debug("debug")
		require.Equal(t, "debug", testBuffer.String())

		currentTime = currentTime.Add(time.Minute * 5)

		testBuffer.Reset()

		logger.Debug("debug")
		require.Equal(t, "debug", testBuffer.String())

		// This amplification actually causes logging to be reverted.
		factory.Amplify()

		testBuffer.Reset()

		logger.Debug("debug")
		require.Empty(t, testBuffer.Bytes())

		testBuffer.Reset()

		factory.Amplify()

		// A successive amplification is ignored.

		logger.Debug("debug")
		require.Empty(t, testBuffer.Bytes())

		// Is only re-activated after Revert() is called.

		factory.Revert()
		factory.Amplify()
		logger.Debug("debug")
		require.Equal(t, "debug", testBuffer.String())
	})
}

func TestAtomicTime(t *testing.T) {
	var at atomicTime

	require.Equal(t, time.Time{}, at.now())
	require.True(t, at.isZero())

	now := time.UnixMilli(1738808290763)
	at.setIfZero(now)
	require.Equal(t, now, at.now())
	require.False(t, at.isZero())

	later := now.Add(time.Second)
	at.setIfZero(later)
	require.Equal(t, now, at.now())
	require.False(t, at.isZero())

	at.setZero()
	require.True(t, at.isZero())
	at.setIfZero(later)
	require.Equal(t, later, at.now())
	require.False(t, at.isZero())
}

func TestAmplificationDurationTracker(t *testing.T) {
	var currentTime time.Time
	getCurrentTime := func() time.Time {
		return currentTime
	}
	amt := amplificationDurationTracker{
		maxDuration: time.Minute * 5,
		now:         getCurrentTime,
	}

	// Initialize current time, as the zero value has special semantics.
	currentTime = time.Now()

	amt.markAmplification()
	require.False(t, amt.isAmplificationDurationExhausted())

	// Ensure amplification isn't exhausted before the configured max duration elapses.
	currentTime = currentTime.Add(time.Minute * 4)
	amt.markAmplification()
	require.False(t, amt.isAmplificationDurationExhausted())

	// Ensure amplification is exhausted once max duration elapses.
	currentTime = currentTime.Add(time.Minute)
	amt.markAmplification()
	require.True(t, amt.isAmplificationDurationExhausted())

	// Ensure amplification is no longer exhausted once we revert the amplification.
	amt.markRevertion()
	require.False(t, amt.isAmplificationDurationExhausted())
}
