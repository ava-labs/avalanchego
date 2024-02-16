// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package profiler

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProfiler(t *testing.T) {
	require := require.New(t)

	dir := t.TempDir()

	p := New(dir)

	// Test Start and Stop CPU Profiler
	err := p.StartCPUProfiler()
	require.NoError(err)

	err = p.StopCPUProfiler()
	require.NoError(err)

	_, err = os.Stat(filepath.Join(dir, cpuProfileFile))
	require.NoError(err)

	// Test Stop CPU Profiler without it running
	err = p.StopCPUProfiler()
	require.ErrorIs(err, errCPUProfilerNotRunning)

	// Test Memory Profiler
	err = p.MemoryProfile()
	require.NoError(err)

	_, err = os.Stat(filepath.Join(dir, memProfileFile))
	require.NoError(err)

	// Test Lock Profiler
	err = p.LockProfile()
	require.NoError(err)

	_, err = os.Stat(filepath.Join(dir, lockProfileFile))
	require.NoError(err)
}
