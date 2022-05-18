// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cpu

import (
	"math"
	"sync"
	"time"

	"github.com/ava-labs/avalanchego/utils/logging"
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

type proc struct {
	p                          *process.Process
	lastPolled                 time.Time
	lastTotalSecondsProcessing float64
}

type manager struct {
	log           logging.Logger
	processesLock sync.Mutex
	processes     map[int]*proc

	usageLock sync.RWMutex
	usage     float64

	closeOnce sync.Once
	onClose   chan struct{}
}

func NewManager(log logging.Logger, frequency, halflife time.Duration) Manager {
	m := &manager{
		log:       log,
		processes: make(map[int]*proc),
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

	creationTimeMS, err := p.CreateTime()
	if err != nil {
		return
	}

	proc := &proc{
		p:                          p,
		lastPolled:                 time.Unix(creationTimeMS/int64(time.Millisecond), 0),
		lastTotalSecondsProcessing: 0,
	}
	m.processesLock.Lock()
	m.processes[pid] = proc
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

		m.log.Info("Updated CPU estimate to %f", m.Usage())

		select {
		case <-ticker.C:
		case <-m.onClose:
			return
		}
	}
}

func (m *manager) getCurrentUsage() float64 {
	now := time.Now()

	m.processesLock.Lock()
	defer m.processesLock.Unlock()

	var usage float64
	for pid, p := range m.processes {
		// If there is an error tracking the CPU utilization of a process,
		// assume that the utilization is 0.
		times, err := p.p.Times()
		if err != nil {
			continue
		}

		m.log.Info("Updating CPU usage of %d : total = %f : stats = %s", pid, times.Total(), times)

		currentTotalSecondsProcessing := times.Total()
		totalSinceLastPolled := currentTotalSecondsProcessing - p.lastTotalSecondsProcessing
		p.lastTotalSecondsProcessing = currentTotalSecondsProcessing

		secondsSinceLastPolled := now.Sub(p.lastPolled).Seconds()
		p.lastPolled = now

		if secondsSinceLastPolled <= 0 || totalSinceLastPolled <= 0 {
			continue
		}

		usage += totalSinceLastPolled / secondsSinceLastPolled
	}
	return usage
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
