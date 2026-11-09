// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package profiler

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProfiler(t *testing.T) {
	require := require.New(t)

	dir := t.TempDir()

	p := New(dir)

	// Test Start and Stop CPU Profiler
	require.NoError(p.StartCPUProfiler())

	require.NoError(p.StopCPUProfiler())
	require.Zero(runtime.SetMutexProfileFraction(-1))

	_, err := os.Stat(filepath.Join(dir, cpuProfileFile))
	require.NoError(err)

	// Test Stop CPU Profiler without it running
	err = p.StopCPUProfiler()
	require.ErrorIs(err, errCPUProfilerNotRunning)

	// Test Memory Profiler
	require.NoError(p.MemoryProfile())

	_, err = os.Stat(filepath.Join(dir, memProfileFile))
	require.NoError(err)

	// Test Lock Profiler
	require.NoError(p.LockProfile())

	_, err = os.Stat(filepath.Join(dir, lockProfileFile))
	require.NoError(err)
}

func TestStartCPUProfilerMutexProfileFraction(t *testing.T) {
	original := runtime.SetMutexProfileFraction(-1)
	t.Cleanup(func() { runtime.SetMutexProfileFraction(original) })

	tests := []struct {
		name     string
		profiler *profiler
		expected int
	}{
		{
			name:     "New records all events",
			profiler: New(t.TempDir()).(*profiler),
			expected: 1,
		},
		{
			name:     "disabled",
			profiler: newProfiler(t.TempDir(), 0),
			expected: 0,
		},
		{
			name:     "sampled",
			profiler: newProfiler(t.TempDir(), 7),
			expected: 7,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			require.NoError(tt.profiler.StartCPUProfiler())
			require.Equal(tt.expected, runtime.SetMutexProfileFraction(-1))
			require.NoError(tt.profiler.StopCPUProfiler())
		})
	}
}
