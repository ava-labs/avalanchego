// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package resource

import (
	"math"
	"strconv"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/shirou/gopsutil/cpu"
	"github.com/shirou/gopsutil/mem"
	"github.com/shirou/gopsutil/process"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/utils/logging"
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

	// returns percentage free in the db volume
	AvailableDiskPercentage() uint64
}

type MemoryUser interface {
	// returns number of bytes available in system memory
	AvailableMemoryBytes() uint64

	// returns percentage of available memory
	AvailableMemoryPercentage() uint64
}

type User interface {
	CPUUser
	DiskUser
	MemoryUser
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
	log            logging.Logger
	processMetrics *metrics

	processesLock sync.Mutex
	processes     map[int]*proc

	usageLock sync.RWMutex
	cpuUsage  float64
	// [readUsage] is the number of bytes/second read from disk recently.
	readUsage float64
	// [writeUsage] is the number of bytes/second written to disk recently.
	writeUsage float64

	availableDiskBytes uint64

	availableDiskPercent uint64

	availableMemoryBytes uint64

	availableMemoryPercent uint64

	closeOnce sync.Once
	onClose   chan struct{}
}

func NewManager(
	log logging.Logger,
	diskPath string,
	frequency,
	cpuHalflife,
	diskHalflife time.Duration,
	metricsRegisterer prometheus.Registerer,
) (Manager, error) {
	processMetrics, err := newMetrics(metricsRegisterer)
	if err != nil {
		return nil, err
	}

	m := &manager{
		log:                  log,
		processMetrics:       processMetrics,
		processes:            make(map[int]*proc),
		onClose:              make(chan struct{}),
		availableDiskBytes:   math.MaxUint64,
		availableMemoryBytes: math.MaxUint64,
	}

	go m.update(diskPath, frequency, cpuHalflife, diskHalflife)
	return m, nil
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

func (m *manager) AvailableDiskPercentage() uint64 {
	m.usageLock.RLock()
	defer m.usageLock.RUnlock()

	return m.availableDiskPercent
}

func (m *manager) AvailableMemoryBytes() uint64 {
	m.usageLock.RLock()
	defer m.usageLock.RUnlock()

	return m.availableMemoryBytes
}

func (m *manager) AvailableMemoryPercentage() uint64 {
	m.usageLock.RLock()
	defer m.usageLock.RUnlock()

	return m.availableMemoryPercent
}

func (m *manager) TrackProcess(pid int) {
	p, err := process.NewProcess(int32(pid))
	if err != nil {
		return
	}

	process := &proc{
		p:   p,
		log: m.log,
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

		availableBytes, availablePercentage, getBytesErr := storage.AvailableBytes(diskPath)
		if getBytesErr != nil {
			m.log.Verbo("failed to lookup resource",
				zap.String("resource", "system disk"),
				zap.String("path", diskPath),
				zap.Error(getBytesErr),
			)
		}

		virtualMem, getMemErr := mem.VirtualMemory()
		if getMemErr != nil {
			m.log.Verbo("failed to lookup resource",
				zap.String("resource", "system memory"),
				zap.Error(getMemErr),
			)
		}

		m.usageLock.Lock()
		m.cpuUsage = oldCPUWeight*m.cpuUsage + currentScaledCPUUsage
		m.readUsage = oldDiskWeight*m.readUsage + currentScaledReadUsage
		m.writeUsage = oldDiskWeight*m.writeUsage + currentScaledWriteUsage

		if getBytesErr == nil {
			m.availableDiskBytes = availableBytes
			m.availableDiskPercent = availablePercentage
		}

		if getMemErr == nil {
			m.availableMemoryBytes = virtualMem.Available
			m.availableMemoryPercent = virtualMem.Available * 100 / virtualMem.Total
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

		processIDStr := strconv.Itoa(int(p.p.Pid))
		m.processMetrics.numCPUCycles.WithLabelValues(processIDStr).Set(p.lastTotalCPU)
		m.processMetrics.numDiskReads.WithLabelValues(processIDStr).Set(float64(p.numReads))
		m.processMetrics.numDiskReadBytes.WithLabelValues(processIDStr).Set(float64(p.lastReadBytes))
		m.processMetrics.numDiskWrites.WithLabelValues(processIDStr).Set(float64(p.numWrites))
		m.processMetrics.numDiskWritesBytes.WithLabelValues(processIDStr).Set(float64(p.lastWriteBytes))
	}

	return totalCPU, totalRead, totalWrite
}

type proc struct {
	p   *process.Process
	log logging.Logger

	initialized bool

	// [lastTotalCPU] is the most recent measurement of total CPU usage.
	lastTotalCPU float64

	// [numReads] is the total number of disk reads performed.
	numReads uint64
	// [lastReadBytes] is the most recent measurement of total disk bytes read.
	lastReadBytes uint64

	// [numWrites] is the total number of disk writes performed.
	numWrites uint64
	// [lastWriteBytes] is the most recent measurement of total disk bytes
	// written.
	lastWriteBytes uint64
}

func (p *proc) getActiveUsage(secondsSinceLastUpdate float64) (float64, float64, float64) {
	// If there is an error tracking the CPU/disk utilization of a process,
	// assume that the utilization is 0.
	times, err := p.p.Times()
	if err != nil {
		p.log.Verbo("failed to lookup resource",
			zap.String("resource", "process CPU"),
			zap.Int32("pid", p.p.Pid),
			zap.Error(err),
		)
		times = &cpu.TimesStat{}
	}

	// Note: IOCounters is not implemented on macos and therefore always returns
	// an error on macos.
	io, err := p.p.IOCounters()
	if err != nil {
		p.log.Verbo("failed to lookup resource",
			zap.String("resource", "process IO"),
			zap.Int32("pid", p.p.Pid),
			zap.Error(err),
		)
		io = &process.IOCountersStat{}
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
	p.numReads = io.ReadCount
	p.lastReadBytes = io.ReadBytes
	p.numWrites = io.WriteCount
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
