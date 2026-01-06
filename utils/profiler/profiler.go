// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package profiler

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"

	"github.com/ava-labs/avalanchego/utils/perms"
)

const (
	// Name of file that CPU profile is written to when StartCPUProfiler called
	cpuProfileFile = "cpu.profile"
	// Name of file that memory profile is written to when MemoryProfile called
	memProfileFile = "mem.profile"
	// Name of file that lock profile is written to
	lockProfileFile = "lock.profile"
)

var (
	_ Profiler = (*profiler)(nil)

	errCPUProfilerRunning    = errors.New("cpu profiler already running")
	errCPUProfilerNotRunning = errors.New("cpu profiler doesn't exist")
)

// Profiler provides helper methods for measuring the current performance of
// this process
type Profiler interface {
	// StartCPUProfiler starts measuring the cpu utilization of this process
	StartCPUProfiler() error

	// StopCPUProfiler stops measuring the cpu utilization of this process
	StopCPUProfiler() error

	// MemoryProfile dumps the current memory utilization of this process
	MemoryProfile() error

	// LockProfile dumps the current lock statistics of this process
	LockProfile() error
}

type profiler struct {
	dir,
	cpuProfileName,
	memProfileName,
	lockProfileName string

	cpuProfileFile *os.File
}

func New(dir string) Profiler {
	return newProfiler(dir)
}

func newProfiler(dir string) *profiler {
	return &profiler{
		dir:             dir,
		cpuProfileName:  filepath.Join(dir, cpuProfileFile),
		memProfileName:  filepath.Join(dir, memProfileFile),
		lockProfileName: filepath.Join(dir, lockProfileFile),
	}
}

func (p *profiler) StartCPUProfiler() error {
	if p.cpuProfileFile != nil {
		return errCPUProfilerRunning
	}

	if err := os.MkdirAll(p.dir, perms.ReadWriteExecute); err != nil {
		return err
	}
	file, err := perms.Create(p.cpuProfileName, perms.ReadWrite)
	if err != nil {
		return err
	}
	if err := pprof.StartCPUProfile(file); err != nil {
		_ = file.Close() // Return the original error
		return err
	}
	runtime.SetMutexProfileFraction(1)

	p.cpuProfileFile = file
	return nil
}

func (p *profiler) StopCPUProfiler() error {
	if p.cpuProfileFile == nil {
		return errCPUProfilerNotRunning
	}

	pprof.StopCPUProfile()
	err := p.cpuProfileFile.Close()
	p.cpuProfileFile = nil
	return err
}

func (p *profiler) MemoryProfile() error {
	if err := os.MkdirAll(p.dir, perms.ReadWriteExecute); err != nil {
		return err
	}
	file, err := perms.Create(p.memProfileName, perms.ReadWrite)
	if err != nil {
		return err
	}
	runtime.GC() // get up-to-date statistics
	if err := pprof.WriteHeapProfile(file); err != nil {
		_ = file.Close() // Return the original error
		return err
	}
	return file.Close()
}

func (p *profiler) LockProfile() error {
	if err := os.MkdirAll(p.dir, perms.ReadWriteExecute); err != nil {
		return err
	}
	file, err := perms.Create(p.lockProfileName, perms.ReadWrite)
	if err != nil {
		return err
	}

	profile := pprof.Lookup("mutex")
	if err := profile.WriteTo(file, 1); err != nil {
		_ = file.Close() // Return the original error
		return err
	}
	return file.Close()
}
