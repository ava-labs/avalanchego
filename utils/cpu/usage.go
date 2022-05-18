// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cpu

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

type User interface {
	// Usage returns the number of CPU cores of usage this user has attributed
	// to it.
	//
	// For example, if this user is reporting a process's CPU utilization and
	// that process is currently using 150% CPU (i.e. one and a half cores of
	// compute) then the return value will be 1.5.
	Usage() float64
}

type ProcessTracker interface {
	// TrackProcess adds [pid] to the list of processes that this tracker is
	// currently managing. Duplicate requests are dropped.
	TrackProcess(pid int)

	// TrackProcess removes [pid] from the list of processes that this tracker
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
	processes     map[int]*process.Process

	usageLock sync.RWMutex
	usage     float64

	closeOnce sync.Once
	onClose   chan struct{}
}

func NewManager(frequency, halflife time.Duration) Manager {
	m := &manager{
		processes: make(map[int]*process.Process),
		onClose:   make(chan struct{}),
	}
	go m.update(frequency, halflife)
	return m
}

func (m *manager) Usage() float64 {
	m.usageLock.RLock()
	defer m.usageLock.RUnlock()

	return m.usage
}

func (m *manager) TrackProcess(pid int) {
	p, err := process.NewProcess(int32(pid))
	if err != nil {
		return
	}

	m.processesLock.Lock()
	m.processes[pid] = p
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
		currentUsage := m.getCurrentUsage()
		currentScaledUsage := newWeight * currentUsage

		m.usageLock.Lock()
		m.usage = oldWeight*m.usage + currentScaledUsage
		m.usageLock.Unlock()

		select {
		case <-ticker.C:
		case <-m.onClose:
			return
		}
	}
}

func (m *manager) getCurrentUsage() float64 {
	m.processesLock.Lock()
	defer m.processesLock.Unlock()

	var usage float64
	for _, p := range m.processes {
		// If there is an error tracking the CPU utilization of a process,
		// assume that the utilization is 0.
		cpu, err := p.Percent(0)
		if err == nil {
			usage += cpu
		}
	}
	return usage / 100
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
