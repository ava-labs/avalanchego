// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tracker

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/linked"
	"github.com/ava-labs/avalanchego/utils/math/meter"
	"github.com/ava-labs/avalanchego/utils/resource"
)

const epsilon = 1e-9

var _ ResourceTracker = (*resourceTracker)(nil)

type Tracker interface {
	// Returns the current usage for the given node.
	Usage(nodeID ids.NodeID, now time.Time) float64
	// Returns the current usage by all nodes.
	TotalUsage() float64
	// Returns the duration between [now] and when the usage of [nodeID] reaches
	// [value], assuming that the node uses no more resources.
	// If the node's usage isn't known, or is already <= [value], returns the
	// zero duration.
	TimeUntilUsage(nodeID ids.NodeID, now time.Time, value float64) time.Duration
}

type DiskTracker interface {
	Tracker
	AvailableDiskBytes() uint64
	AvailableDiskPercentage() uint64
}

// ResourceTracker is an interface for tracking peers' usage of resources
type ResourceTracker interface {
	CPUTracker() Tracker
	DiskTracker() DiskTracker
	// Registers that the given node started processing at the given time.
	StartProcessing(ids.NodeID, time.Time)
	// Registers that the given node stopped processing at the given time.
	StopProcessing(ids.NodeID, time.Time)
}

type cpuResourceTracker struct {
	t *resourceTracker
}

func (t *cpuResourceTracker) Usage(nodeID ids.NodeID, now time.Time) float64 {
	rt := t.t
	rt.lock.Lock()
	defer rt.lock.Unlock()

	realCPUUsage := rt.resources.CPUUsage()
	rt.metrics.cpuMetric.Set(realCPUUsage)

	measuredProcessingTime := rt.processingMeter.Read(now)
	rt.metrics.processingTimeMetric.Set(measuredProcessingTime)

	if measuredProcessingTime == 0 {
		return 0
	}

	m, exists := rt.meters.Get(nodeID)
	if !exists {
		return 0
	}

	portionUsageByNode := m.Read(now) / measuredProcessingTime
	return realCPUUsage * portionUsageByNode
}

func (t *cpuResourceTracker) TotalUsage() float64 {
	return t.t.resources.CPUUsage()
}

func (t *cpuResourceTracker) TimeUntilUsage(nodeID ids.NodeID, now time.Time, value float64) time.Duration {
	rt := t.t
	rt.lock.Lock()
	defer rt.lock.Unlock()

	rt.prune(now)

	m, exists := rt.meters.Get(nodeID)
	if !exists {
		return 0
	}

	measuredProcessingTime := rt.processingMeter.Read(now)
	rt.metrics.processingTimeMetric.Set(measuredProcessingTime)

	if measuredProcessingTime == 0 {
		return 0
	}

	realCPUUsage := rt.resources.CPUUsage()
	rt.metrics.cpuMetric.Set(realCPUUsage)

	if realCPUUsage == 0 {
		return 0
	}

	scale := realCPUUsage / measuredProcessingTime
	return m.TimeUntil(now, value/scale)
}

type diskResourceTracker struct {
	t *resourceTracker
}

func (t *diskResourceTracker) Usage(nodeID ids.NodeID, now time.Time) float64 {
	rt := t.t
	rt.lock.Lock()
	defer rt.lock.Unlock()

	// [realWriteUsage] is only used for metrics.
	realReadUsage, realWriteUsage := rt.resources.DiskUsage()
	rt.metrics.diskReadsMetric.Set(realReadUsage)
	rt.metrics.diskWritesMetric.Set(realWriteUsage)

	measuredProcessingTime := rt.processingMeter.Read(now)
	rt.metrics.processingTimeMetric.Set(measuredProcessingTime)

	if measuredProcessingTime == 0 {
		return 0
	}

	m, exists := rt.meters.Get(nodeID)
	if !exists {
		return 0
	}

	portionUsageByNode := m.Read(now) / measuredProcessingTime
	return realReadUsage * portionUsageByNode
}

func (t *diskResourceTracker) AvailableDiskBytes() uint64 {
	rt := t.t
	rt.lock.Lock()
	defer rt.lock.Unlock()

	bytesAvailable := rt.resources.AvailableDiskBytes()
	rt.metrics.diskSpaceAvailable.Set(float64(bytesAvailable))
	return bytesAvailable
}

func (t *diskResourceTracker) AvailableDiskPercentage() uint64 {
	rt := t.t
	rt.lock.Lock()
	defer rt.lock.Unlock()

	percentageAvailable := rt.resources.AvailableDiskPercentage()
	rt.metrics.diskPercentageAvailable.Set(float64(percentageAvailable))
	return percentageAvailable
}

func (t *diskResourceTracker) TotalUsage() float64 {
	realReadUsage, _ := t.t.resources.DiskUsage()
	return realReadUsage
}

func (t *diskResourceTracker) TimeUntilUsage(nodeID ids.NodeID, now time.Time, value float64) time.Duration {
	rt := t.t
	rt.lock.Lock()
	defer rt.lock.Unlock()

	rt.prune(now)

	m, exists := rt.meters.Get(nodeID)
	if !exists {
		return 0
	}

	measuredProcessingTime := rt.processingMeter.Read(now)
	rt.metrics.processingTimeMetric.Set(measuredProcessingTime)

	if measuredProcessingTime == 0 {
		return 0
	}

	// [realWriteUsage] is only used for metrics.
	realReadUsage, realWriteUsage := rt.resources.DiskUsage()
	rt.metrics.diskReadsMetric.Set(realReadUsage)
	rt.metrics.diskWritesMetric.Set(realWriteUsage)

	if realReadUsage == 0 {
		return 0
	}

	scale := realReadUsage / measuredProcessingTime
	return m.TimeUntil(now, value/scale)
}

type resourceTracker struct {
	lock sync.RWMutex

	resources resource.User
	factory   meter.Factory
	// Tracks total number of current processing requests by all nodes.
	processingMeter meter.Meter
	halflife        time.Duration
	// Each element is a meter that tracks the number of current processing
	// requests by a node. [meters] is ordered by the last time that a meter was
	// utilized. This doesn't necessarily result in the meters being sorted
	// based on their usage. However, in practice the nodes that are not being
	// utilized will move towards the oldest elements where they can be deleted.
	meters  *linked.Hashmap[ids.NodeID, meter.Meter]
	metrics *trackerMetrics
}

func NewResourceTracker(
	reg prometheus.Registerer,
	resources resource.User,
	factory meter.Factory,
	halflife time.Duration,
) (ResourceTracker, error) {
	t := &resourceTracker{
		factory:         factory,
		resources:       resources,
		processingMeter: factory.New(halflife),
		halflife:        halflife,
		meters:          linked.NewHashmap[ids.NodeID, meter.Meter](),
	}
	var err error
	t.metrics, err = newCPUTrackerMetrics(reg)
	if err != nil {
		return nil, fmt.Errorf("initializing resourceTracker metrics errored with: %w", err)
	}
	return t, nil
}

func (rt *resourceTracker) CPUTracker() Tracker {
	return &cpuResourceTracker{t: rt}
}

func (rt *resourceTracker) DiskTracker() DiskTracker {
	return &diskResourceTracker{t: rt}
}

func (rt *resourceTracker) StartProcessing(nodeID ids.NodeID, now time.Time) {
	rt.lock.Lock()
	defer rt.lock.Unlock()

	meter := rt.getMeter(nodeID)
	meter.Inc(now, 1)
	rt.processingMeter.Inc(now, 1)
}

func (rt *resourceTracker) StopProcessing(nodeID ids.NodeID, now time.Time) {
	rt.lock.Lock()
	defer rt.lock.Unlock()

	meter := rt.getMeter(nodeID)
	meter.Dec(now, 1)
	rt.processingMeter.Dec(now, 1)
}

// getMeter returns the meter used to measure CPU time spent processing
// messages from [nodeID].
// assumes [rt.lock] is held.
func (rt *resourceTracker) getMeter(nodeID ids.NodeID) meter.Meter {
	m, exists := rt.meters.Get(nodeID)
	if exists {
		return m
	}

	newMeter := rt.factory.New(rt.halflife)
	rt.meters.Put(nodeID, newMeter)
	return newMeter
}

// prune attempts to remove meters that currently show a value less than
// [epsilon].
//
// Because [rt.meters] isn't guaranteed to be sorted by their values, this
// doesn't guarantee that all meters showing less than [epsilon] are removed.
func (rt *resourceTracker) prune(now time.Time) {
	for {
		oldest, meter, exists := rt.meters.Oldest()
		if !exists {
			return
		}

		if meter.Read(now) > epsilon {
			return
		}

		rt.meters.Delete(oldest)
	}
}

type trackerMetrics struct {
	processingTimeMetric    prometheus.Gauge
	cpuMetric               prometheus.Gauge
	diskReadsMetric         prometheus.Gauge
	diskWritesMetric        prometheus.Gauge
	diskSpaceAvailable      prometheus.Gauge
	diskPercentageAvailable prometheus.Gauge
}

func newCPUTrackerMetrics(reg prometheus.Registerer) (*trackerMetrics, error) {
	m := &trackerMetrics{
		processingTimeMetric: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "processing_time",
			Help: "Tracked processing time over all nodes. Value expected to be in [0, number of CPU cores], but can go higher due to IO bound processes and thread multiplexing",
		}),
		cpuMetric: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "cpu_usage",
			Help: "CPU usage tracked by the resource manager. Value should be in [0, number of CPU cores]",
		}),
		diskReadsMetric: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "disk_reads",
			Help: "Disk reads (bytes/sec) tracked by the resource manager",
		}),
		diskWritesMetric: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "disk_writes",
			Help: "Disk writes (bytes/sec) tracked by the resource manager",
		}),
		diskSpaceAvailable: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "disk_available_space",
			Help: "Available space remaining (bytes) on the database volume",
		}),
		diskPercentageAvailable: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "disk_available_percentage",
			Help: "Percentage of database volume available",
		}),
	}
	err := errors.Join(
		reg.Register(m.processingTimeMetric),
		reg.Register(m.cpuMetric),
		reg.Register(m.diskReadsMetric),
		reg.Register(m.diskWritesMetric),
		reg.Register(m.diskSpaceAvailable),
		reg.Register(m.diskPercentageAvailable),
	)
	return m, err
}
