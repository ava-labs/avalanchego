// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package resource

import (
	"math"
	"sync"
	"time"

	"github.com/shirou/gopsutil/process"

	"github.com/ava-labs/avalanchego/utils/storage"
)

var (
	lnHalf = math.Log(.5)

	_ Manager = (*manager)(nil)
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

	// returns number of bytes available in the db volume
	AvailableDiskBytes() uint64
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

type manager struct {
	processesLock sync.Mutex
	processes     map[int]*proc

	usageLock sync.RWMutex
	cpuUsage  float64
	// [readUsage] is the number of bytes/second read from disk recently.
	readUsage float64
	// [writeUsage] is the number of bytes/second written to disk recently.
	writeUsage float64

	availableDiskBytes uint64

	closeOnce sync.Once
	onClose   chan struct{}
}

func NewManager(diskPath string, frequency, cpuHalflife, diskHalflife time.Duration) Manager {
	m := &manager{
		processes:          make(map[int]*proc),
		onClose:            make(chan struct{}),
		availableDiskBytes: math.MaxUint64,
	}
	go m.update(diskPath, frequency, cpuHalflife, diskHalflife)
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

func (m *manager) AvailableDiskBytes() uint64 {
	m.usageLock.RLock()
	defer m.usageLock.RUnlock()

	return m.availableDiskBytes
}

func (m *manager) TrackProcess(pid int) {
	p, err := process.NewProcess(int32(pid))
	if err != nil {
		return
	}

	process := &proc{p: p}

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

func (m *manager) update(diskPath string, frequency, cpuHalflife, diskHalflife time.Duration) {
	ticker := time.NewTicker(frequency)
	defer ticker.Stop()

	newCPUWeight, oldCPUWeight := getSampleWeights(frequency, cpuHalflife)
	newDiskWeight, oldDiskWeight := getSampleWeights(frequency, diskHalflife)

	frequencyInSeconds := frequency.Seconds()
	for {
		currentCPUUsage, currentReadUsage, currentWriteUsage := m.getActiveUsage(frequencyInSeconds)
		currentScaledCPUUsage := newCPUWeight * currentCPUUsage
		currentScaledReadUsage := newDiskWeight * currentReadUsage
		currentScaledWriteUsage := newDiskWeight * currentWriteUsage

		availableBytes, getBytesErr := storage.AvailableBytes(diskPath)

		m.usageLock.Lock()
		m.cpuUsage = oldCPUWeight*m.cpuUsage + currentScaledCPUUsage
		m.readUsage = oldDiskWeight*m.readUsage + currentScaledReadUsage
		m.writeUsage = oldDiskWeight*m.writeUsage + currentScaledWriteUsage

		if getBytesErr == nil {
			m.availableDiskBytes = availableBytes
		}

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
func (m *manager) getActiveUsage(secondsSinceLastUpdate float64) (float64, float64, float64) {
	m.processesLock.Lock()
	defer m.processesLock.Unlock()

	var (
		totalCPU   float64
		totalRead  float64
		totalWrite float64
	)
	for _, p := range m.processes {
		cpu, read, write := p.getActiveUsage(secondsSinceLastUpdate)
		totalCPU += cpu
		totalRead += read
		totalWrite += write
	}

	return totalCPU, totalRead, totalWrite
}

type proc struct {
	p *process.Process

	initialized bool

	// [lastTotalCPU] is the most recent measurement of total CPU usage.
	lastTotalCPU float64

	// [lastReadBytes] is the most recent measurement of total disk bytes read.
	lastReadBytes uint64
	// [lastWriteBytes] is the most recent measurement of total disk bytes
	// written.
	lastWriteBytes uint64
}

func (p *proc) getActiveUsage(secondsSinceLastUpdate float64) (float64, float64, float64) {
	// If there is an error tracking the CPU/disk utilization of a process,
	// assume that the utilization is 0.
	times, err := p.p.Times()
	if err != nil {
		return 0, 0, 0
	}

	io, err := p.p.IOCounters()
	if err != nil {
		return 0, 0, 0
	}

	var (
		cpu   float64
		read  float64
		write float64
	)
	totalCPU := times.Total()
	if p.initialized {
		if totalCPU > p.lastTotalCPU {
			newCPU := totalCPU - p.lastTotalCPU
			cpu = newCPU / secondsSinceLastUpdate
		}
		if io.ReadBytes > p.lastReadBytes {
			newRead := io.ReadBytes - p.lastReadBytes
			read = float64(newRead) / secondsSinceLastUpdate
		}
		if io.WriteBytes > p.lastWriteBytes {
			newWrite := io.WriteBytes - p.lastWriteBytes
			write = float64(newWrite) / secondsSinceLastUpdate
		}
	}

	p.initialized = true
	p.lastTotalCPU = totalCPU
	p.lastReadBytes = io.ReadBytes
	p.lastWriteBytes = io.WriteBytes

	return cpu, read, write
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
