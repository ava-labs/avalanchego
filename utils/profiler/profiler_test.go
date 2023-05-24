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
	dir := t.TempDir()

	p := New(dir)

	// Test Start and Stop CPU Profiler
	err := p.StartCPUProfiler()
	require.NoError(t, err)

	err = p.StopCPUProfiler()
	require.NoError(t, err)

	_, err = os.Stat(filepath.Join(dir, cpuProfileFile))
	require.NoError(t, err)

	// Test Stop CPU Profiler without it running
	err = p.StopCPUProfiler()
	require.Error(t, err)

	// Test Memory Profiler
	err = p.MemoryProfile()
	require.NoError(t, err)

	_, err = os.Stat(filepath.Join(dir, memProfileFile))
	require.NoError(t, err)

	// Test Lock Profiler
	err = p.LockProfile()
	require.NoError(t, err)

	_, err = os.Stat(filepath.Join(dir, lockProfileFile))
	require.NoError(t, err)
}
