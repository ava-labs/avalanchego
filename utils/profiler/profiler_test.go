// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package profiler

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestProfiler(t *testing.T) {
	dir := t.TempDir()

	p := New(dir)

	// Test Start and Stop CPU Profiler
	err := p.StartCPUProfiler()
	assert.NoError(t, err)

	err = p.StopCPUProfiler()
	assert.NoError(t, err)

	_, err = os.Stat(filepath.Join(dir, cpuProfileFile))
	assert.NoError(t, err)

	// Test Stop CPU Profiler without it running
	err = p.StopCPUProfiler()
	assert.Error(t, err)

	// Test Memory Profiler
	err = p.MemoryProfile()
	assert.NoError(t, err)

	_, err = os.Stat(filepath.Join(dir, memProfileFile))
	assert.NoError(t, err)

	// Test Lock Profiler
	err = p.LockProfile()
	assert.NoError(t, err)

	_, err = os.Stat(filepath.Join(dir, lockProfileFile))
	assert.NoError(t, err)
}
