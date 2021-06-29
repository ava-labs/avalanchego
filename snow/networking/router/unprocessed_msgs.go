// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package router

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/networking/tracker"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/timer"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/prometheus/client_golang/prometheus"
)

var _ unprocessedMsgs = &unprocessedMsgsImpl{}

type unprocessedMsgs interface {
	// Add an unprocessed message
	Push(message)
	// Get and remove the unprocessed message that should
	// be processed next. Must never be called when Len() == 0.
	Pop() message
	// Returns the number of unprocessed messages
	Len() int
}

func newUnprocessedMsgs(
	log logging.Logger,
	vdrs validators.Set,
	cpuTracker tracker.TimeTracker,
	metricsNamespace string,
	metricsRegisterer prometheus.Registerer,
) (unprocessedMsgs, error) {
	u := &unprocessedMsgsImpl{
		log:                   log,
		vdrs:                  vdrs,
		cpuTracker:            cpuTracker,
		nodeToUnprocessedMsgs: make(map[ids.ShortID]int),
	}
	return u, u.metrics.initialize(metricsNamespace, metricsRegisterer)
}

// Implements unprocessedMsgs.
// Not safe for concurrent access.
// TODO: Use a better data structure for this.
// We can do something better than pushing to the back
// of a queue.
type unprocessedMsgsImpl struct {
	log     logging.Logger
	metrics unprocessedMsgsMetrics
	// Node ID --> Messages this node has in [msgs]
	nodeToUnprocessedMsgs map[ids.ShortID]int
	// Unprocessed messages
	msgs []message
	// Validator set for the chain associated with this
	vdrs validators.Set
	// Tracks CPU utilization of each node
	cpuTracker tracker.TimeTracker
	// Useful for faking time in tests
	clock timer.Clock
}

func (u *unprocessedMsgsImpl) Push(msg message) {
	u.msgs = append(u.msgs, msg)
	u.nodeToUnprocessedMsgs[msg.nodeID]++
	u.metrics.nodesWithUnprocessedMsgs.Set(float64(len(u.nodeToUnprocessedMsgs)))
	u.metrics.len.Inc()
}

// Must never be called when [u.Len()] == 0.
// FIFO, but skip over messages whose senders whose messages
// have caused us to use excessive CPU recently.
func (u *unprocessedMsgsImpl) Pop() message {
	n := len(u.msgs)
	i := 0
	for {
		if i == n {
			u.log.Warn("canPop is false for all %d unprocessed messages", n)
		}
		msg := u.msgs[0]
		// See if it's OK to process [msg] next
		if u.canPop(&msg) || i == n { // i should never == n but handle anyway as a fail-safe
			if len(u.msgs) == 1 {
				u.msgs = nil // Give back memory if possible
			} else {
				u.msgs = u.msgs[1:]
			}
			u.nodeToUnprocessedMsgs[msg.nodeID]--
			if u.nodeToUnprocessedMsgs[msg.nodeID] == 0 {
				delete(u.nodeToUnprocessedMsgs, msg.nodeID)
			}
			u.metrics.nodesWithUnprocessedMsgs.Set(float64(len(u.nodeToUnprocessedMsgs)))
			u.metrics.len.Dec()
			return msg
		}
		// [msg.nodeID] is causing excessive CPU usage.
		// Push [msg] to back of [u.msgs] and handle it later.
		u.msgs = append(u.msgs, msg)
		u.msgs = u.msgs[1:]
		i++
		u.metrics.numExcessiveCPU.Inc()
	}
}

func (u *unprocessedMsgsImpl) Len() int {
	return len(u.msgs)
}

// canPop will return true for at least one message in [u.msgs]
func (u *unprocessedMsgsImpl) canPop(msg *message) bool {
	// Every node has some allowed CPU allocation depending on
	// the number of nodes with unprocessed messages.
	baseMaxCPU := 1 / float64(len(u.nodeToUnprocessedMsgs))
	weight, isVdr := u.vdrs.GetWeight(msg.nodeID)
	if !isVdr {
		weight = 0
	}
	// The sum of validator weights should never be 0, but handle
	// that case for completeness here to avoid divide by 0.
	portionWeight := float64(0)
	totalVdrsWeight := u.vdrs.Weight()
	if totalVdrsWeight != 0 {
		portionWeight = float64(weight) / float64(totalVdrsWeight)
	}
	// Validators are allowed to use more CPU. More weight --> more CPU use allowed.
	recentCPUUtilized := u.cpuTracker.Utilization(msg.nodeID, u.clock.Time())
	maxCPU := baseMaxCPU + (1.0-baseMaxCPU)*portionWeight
	return recentCPUUtilized <= maxCPU
}

type unprocessedMsgsMetrics struct {
	len                      prometheus.Gauge
	nodesWithUnprocessedMsgs prometheus.Gauge
	numExcessiveCPU          prometheus.Counter
}

func (m *unprocessedMsgsMetrics) initialize(
	metricsNamespace string,
	metricsRegisterer prometheus.Registerer,
) error {
	namespace := fmt.Sprintf("%s_%s", metricsNamespace, "unprocessed_msgs")
	m.len = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "len",
		Help:      "Messages ready to be processed",
	})
	m.nodesWithUnprocessedMsgs = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "nodes",
		Help:      "Nodes from which there are at least 1 message ready to be processed",
	})
	m.numExcessiveCPU = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "excessive_cpu",
		Help:      "Times we deferred handling a message from a node because the node was using excessive CPU",
	})
	errs := wrappers.Errs{}
	errs.Add(metricsRegisterer.Register(m.len))
	errs.Add(metricsRegisterer.Register(m.nodesWithUnprocessedMsgs))
	errs.Add(metricsRegisterer.Register(m.numExcessiveCPU))
	return errs.Err
}
