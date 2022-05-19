// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package resource

import (
	"math"
	"sync"
	"time"

	"github.com/shirou/gopsutil/process"
)

var (
	lnHalf = math.Log(.5)

	_ Manager = &manager{}
)

type CPUUser interface {
	// CPUUsage returns the number of CPU cores of usage this user has attributed
	// to it.
	//
	// For example, if this user is reporting a process's CPU utilization and
	// that process is currently using 150% CPU (i.e. one and a half cores of
	// compute) then the return value will be 1.5.
	CPUUsage() float64
}

type DiskUser interface {
	// DiskUsage returns the number of bytes per second read from/written to
	// disk recently.
	DiskUsage() (read float64, write float64)
}

type User interface {
	CPUUser
	DiskUser
}

type ProcessTracker interface {
	// TrackProcess adds [pid] to the list of processes that this tracker is
	// currently managing. Duplicate requests are dropped.
	TrackProcess(pid int)

	// UntrackProcess removes [pid] from the list of processes that this tracker
	// is currently managing. Untracking a currently untracked [pid] is a noop.
	UntrackProcess(pid int)
}

type Manager interface {
	User
	ProcessTracker

	// Shutdown allocated resources and stop tracking all processes.
	Shutdown()
}

type proc struct {
	p *process.Process

	// [lastUpdateTime] is the time that CPU/IO measurements were most recently
	// taken.
	lastUpdateTime time.Time
	// [lastReadBytes] is the most recent measurement of total disk bytes read.
	lastReadBytes uint64
	// [lastWriteBytes] is the most recent measurement of total disk bytes
	// written.
	lastWriteBytes uint64
}

type manager struct {
	processesLock sync.Mutex
	processes     map[int]*proc

	usageLock sync.RWMutex
	cpuUsage  float64
	// [readUsage] is the number of bytes/second read from disk recently.
	readUsage float64
	// [writeUsage] is the number of bytes/second written to disk recently.
	writeUsage float64

	closeOnce sync.Once
	onClose   chan struct{}
}

func NewManager(frequency, halflife time.Duration) Manager {
	m := &manager{
		processes: make(map[int]*proc),
		onClose:   make(chan struct{}),
	}
	go m.update(frequency, halflife)
	return m
}

func (m *manager) CPUUsage() float64 {
	m.usageLock.RLock()
	defer m.usageLock.RUnlock()

	return m.cpuUsage
}

func (m *manager) DiskUsage() (float64, float64) {
	m.usageLock.RLock()
	defer m.usageLock.RUnlock()

	return m.readUsage, m.writeUsage
}

func (m *manager) TrackProcess(pid int) {
	p, err := process.NewProcess(int32(pid))
	if err != nil {
		return
	}

	process := &proc{
		p:              p,
		lastUpdateTime: time.Now(),
	}

	m.processesLock.Lock()
	m.processes[pid] = process
	m.processesLock.Unlock()
}

func (m *manager) UntrackProcess(pid int) {
	m.processesLock.Lock()
	delete(m.processes, pid)
	m.processesLock.Unlock()
}

func (m *manager) Shutdown() {
	m.closeOnce.Do(func() {
		close(m.onClose)
	})
}

func (m *manager) update(frequency, halflife time.Duration) {
	ticker := time.NewTicker(frequency)
	defer ticker.Stop()

	newWeight, oldWeight := getSampleWeights(frequency, halflife)

	for {
		currentCPUUsage, currentReadUsage, currentWriteUsage := m.getActiveUsage()
		currentScaledCPUUsage := newWeight * currentCPUUsage
		currentScaledReadUsage := newWeight * currentReadUsage
		currentScaledWriteUsage := newWeight * currentWriteUsage

		m.usageLock.Lock()
		m.cpuUsage = oldWeight*m.cpuUsage + currentScaledCPUUsage
		m.readUsage = oldWeight*m.readUsage + currentScaledReadUsage
		m.writeUsage = oldWeight*m.writeUsage + currentScaledWriteUsage
		m.usageLock.Unlock()

		select {
		case <-ticker.C:
		case <-m.onClose:
			return
		}
	}
}

// Returns:
// 1. Current CPU usage by all processes.
// 2. Current bytes/sec read from disk by all processes.
// 3. Current bytes/sec written to disk by all processes.
func (m *manager) getActiveUsage() (float64, float64, float64) {
	now := time.Now()

	m.processesLock.Lock()
	defer m.processesLock.Unlock()

	var (
		usage float64
		read  float64
		write float64
	)
	for _, p := range m.processes {
		// If there is an error tracking the CPU/disk utilization of a process,
		// assume that the utilization is 0.
		cpu, err := p.p.Percent(0)
		if err == nil {
			usage += cpu
		}
		io, err := p.p.IOCounters()
		if err != nil {
			continue
		}

		secondsSinceLastUpdate := now.Sub(p.lastUpdateTime).Seconds()
		if secondsSinceLastUpdate > 0 {
			if io.ReadBytes > p.lastReadBytes {
				newRead := io.ReadBytes - p.lastReadBytes
				read += float64(newRead) / secondsSinceLastUpdate
			}
			if io.WriteBytes > p.lastWriteBytes {
				newWrite := io.WriteBytes - p.lastWriteBytes
				write += float64(newWrite) / secondsSinceLastUpdate
			}
		}

		p.lastUpdateTime = now
		p.lastReadBytes = io.ReadBytes
		p.lastWriteBytes = io.ReadBytes
	}

	return usage / 100, read, write
}

// getSampleWeights converts the frequency of CPU sampling and the halflife of
// the CPU sample's usefulness into weights to scale the newly sampled point and
// previously samples.
func getSampleWeights(frequency, halflife time.Duration) (float64, float64) {
	halflifeInSamples := float64(halflife) / float64(frequency)
	oldWeight := math.Exp(lnHalf / halflifeInSamples)
	newWeight := 1 - oldWeight
	return newWeight, oldWeight
}
