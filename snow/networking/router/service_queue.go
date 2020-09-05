// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package router

import (
	"errors"
	"sync"
	"time"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow"
	"github.com/ava-labs/gecko/snow/networking/tracker"
	"github.com/ava-labs/gecko/snow/validators"
	"github.com/prometheus/client_golang/prometheus"
)

var (
	errNoMessages = errors.New("no messages remaining on queue")
)

type messageQueue interface {
	PopMessage() (message, error)                                // Pop the next message from the queue
	PushMessage(message) bool                                    // Push a message to the queue
	UtilizeCPU(ids.ShortID, time.Time, time.Time, time.Duration) // Registers consumption of CPU time
	EndInterval(time.Time)                                       // Register end of an interval of real time
	Shutdown()
}

// Implements MessageQueue using a multi-level queue of FIFO queues
type multiLevelQueue struct {
	lock sync.Mutex

	validators validators.Set

	// Tracks total CPU consumption
	intervalConsumption, tierConsumption time.Duration

	currentTier int

	resourceManager ResourceManager

	// CPU based prioritization
	cpuTracker    tracker.TimeTracker
	queues        []singleLevelQueue
	cpuRanges     []float64       // CPU Utilization ranges that should be attributed to a corresponding queue
	cpuAllotments []time.Duration // Allotments of CPU time per cycle that should be spent on each level of queue
	cpuPortion    float64

	// Message throttling
	msgTracker                                   tracker.CountingTracker
	bufferSize, pendingMessages                  int
	msgPoolSize, stakerReservedMessages          uint32
	pendingPoolMessages, maxNonStakerPendingMsgs uint32
	msgPortion                                   float64

	semaChan chan struct{}

	ctx     *snow.Context
	metrics *metrics
}

// newMultiLevelQueue creates a new MultilevelQueue and counting semaphore for signaling when messages are available
// to read from the queue. The length of consumptionRanges and consumptionAllotments
// defines the range of priorities for the multi-level queue and the amount of time to
// spend on each level. Their length must be the same.
func newMultiLevelQueue(
	vdrs validators.Set,
	ctx *snow.Context,
	metrics *metrics,
	cpuTracker tracker.TimeTracker,
	consumptionRanges []float64,
	consumptionAllotments []time.Duration,
	bufferSize int,
	maxNonStakerPendingMsgs uint32,
	msgPortion,
	cpuPortion float64,
) (messageQueue, chan struct{}) {
	semaChan := make(chan struct{}, bufferSize)
	singleLevelSize := bufferSize / len(consumptionRanges)
	msgTracker := tracker.NewMessageTracker()
	resourceManager := NewResourceManager(vdrs, ctx.Log, msgTracker, cpuTracker, uint32(bufferSize), maxNonStakerPendingMsgs, msgPortion, cpuPortion)
	queues := make([]singleLevelQueue, len(consumptionRanges))
	for index := 0; index < len(queues); index++ {
		gauge, histogram, err := metrics.registerTierStatistics(index)
		// An error should only occur while registering (not creating) the gauge and histogram
		// so if there is a non-nil error, it is safe to log the error and proceed as normal.
		if err != nil {
			ctx.Log.Error("Failed to register metrics for tier %d of message queue", index)
		}
		queues[index] = singleLevelQueue{
			msgs:        make(chan message, singleLevelSize),
			pending:     gauge,
			waitingTime: histogram,
		}
	}

	stakerReservedMessages := uint32(float64(bufferSize) * msgPortion)
	msgPoolSize := uint32(bufferSize) - stakerReservedMessages

	return &multiLevelQueue{
		validators:              vdrs,
		cpuTracker:              cpuTracker,
		msgTracker:              msgTracker,
		resourceManager:         resourceManager,
		queues:                  queues,
		cpuRanges:               consumptionRanges,
		cpuAllotments:           consumptionAllotments,
		cpuPortion:              cpuPortion,
		maxNonStakerPendingMsgs: maxNonStakerPendingMsgs,
		msgPortion:              msgPortion,
		msgPoolSize:             msgPoolSize,
		stakerReservedMessages:  stakerReservedMessages,
		ctx:                     ctx,
		metrics:                 metrics,
		bufferSize:              bufferSize,
		semaChan:                semaChan,
	}, semaChan
}

// PushMessage attempts to add a message to the queue and
// increments the counting semaphore if successful.
func (ml *multiLevelQueue) PushMessage(msg message) bool {
	ml.lock.Lock()
	defer ml.lock.Unlock()

	return ml.pushMessage(msg)
}

// PopMessage attempts to read the next message from the queue
func (ml *multiLevelQueue) PopMessage() (message, error) {
	ml.lock.Lock()
	defer ml.lock.Unlock()

	msg, err := ml.popMessage()
	if err == nil {
		ml.pendingMessages--
		ml.msgTracker.Remove(msg.validatorID)
		msg.Done()
		ml.metrics.pending.Dec()
	}
	return msg, err
}

// UtilizeCPU registers that [duration] was spent processing a message
// from [vdr]
func (ml *multiLevelQueue) UtilizeCPU(vdr ids.ShortID, startTime, endTime time.Time, duration time.Duration) {
	ml.lock.Lock()
	defer ml.lock.Unlock()

	ml.cpuTracker.UtilizeTime(vdr, startTime, endTime)
	ml.intervalConsumption += duration
	ml.tierConsumption += duration
	if ml.tierConsumption > ml.cpuAllotments[ml.currentTier] {
		ml.tierConsumption = 0
		ml.currentTier++
		ml.currentTier %= len(ml.queues)
	}
}

// EndInterval marks the end of a regular interval of CPU time
// at [currentTime]
func (ml *multiLevelQueue) EndInterval(currentTime time.Time) {
	ml.lock.Lock()
	defer ml.lock.Unlock()

	ml.cpuTracker.EndInterval(currentTime)
	ml.metrics.cpu.Observe(float64(ml.intervalConsumption.Milliseconds()))
	ml.intervalConsumption = 0
}

// Shutdown closes the sema channel
// After Shutdown is called, PushMessage must never be called on multiLevelQueue again
func (ml *multiLevelQueue) Shutdown() {
	ml.lock.Lock()
	defer ml.lock.Unlock()

	close(ml.semaChan)
}

// popMessage grabs a message from the current queue. If none is
// available it loops downwards through the queues to find one.
// If a message no longer belongs on the queue where it's found,
// it attempts to push it down to the correct queue.
// Assumes the lock is held
func (ml *multiLevelQueue) popMessage() (message, error) {
	startTier := ml.currentTier

	for {
		select {
		case msg := <-ml.queues[ml.currentTier].msgs:
			ml.queues[ml.currentTier].pending.Dec()
			ml.queues[ml.currentTier].waitingTime.Observe(float64(time.Since(msg.received)))

			// Check where messages from this validator currently belong
			correctIndex := ml.getPriorityIndex(msg.validatorID)

			// If the message is at least the priority of the current tier
			// or this message comes from the lowest priority queue
			// return the message.
			if correctIndex <= ml.currentTier || ml.currentTier >= len(ml.queues)-1 {
				return msg, nil
			}

			// If the message belongs on a different queue, attempt to push
			// the message down to a lower queue if possible.
			if !ml.waterfallMessage(msg, correctIndex) {
				return msg, nil
			}

			// If waterfalling the message was successful, there is a message on
			// the correct queue below the current tier.
			startTier = ml.currentTier
		default:
			ml.tierConsumption = 0
			ml.currentTier++
			ml.currentTier %= len(ml.queues)
			if ml.currentTier == startTier {
				return message{}, errNoMessages
			}
		}
	}
}

// pushMessage adds a message to the appropriate level (or lower)
// Assumes the lock is held
func (ml *multiLevelQueue) pushMessage(msg message) bool {
	// If the message queue is already full, skip iterating
	// through the queue levels to return false
	if ml.pendingMessages >= ml.bufferSize {
		ml.ctx.Log.Debug("Dropped message due to a full message queue with %d messages", ml.pendingMessages)
		ml.metrics.dropped.Inc()
		return false
	}

	validatorID := msg.validatorID
	if validatorID.IsZero() {
		ml.ctx.Log.Warn("Dropping message due to invalid validatorID")
		return false
	}

	success := ml.resourceManager.TakeMessage(&msg)
	if !success {
		ml.metrics.dropped.Inc()
		ml.metrics.throttled.Inc()
		return false
	}

	// Place the message on the correct queue
	if !ml.placeMessage(msg) {
		ml.ctx.Log.Verbo("Dropped message during push: %s", msg)
		ml.metrics.dropped.Inc()
		msg.Done()
		return false
	}

	ml.pendingMessages++
	select {
	case ml.semaChan <- struct{}{}:
	default:
		ml.ctx.Log.Error("Sempahore channel was full after pushing message to the message queue")
	}
	ml.metrics.pending.Inc()
	return true
}

// placeMessage finds the correct index of a message and attempts to place the
// message at that queue or lower
func (ml *multiLevelQueue) placeMessage(msg message) bool {
	// Find the highest index this message could be placed on and waterfall
	// the message to lower queues if the higher ones are full
	queueIndex := ml.getPriorityIndex(msg.validatorID)
	return ml.waterfallMessage(msg, queueIndex)
}

// waterfallMessage attempts to add the message to the queue at [queueIndex]
// If that queue is full, it attempts to move the message to a lower queue
// until it is forced to drop the message.
func (ml *multiLevelQueue) waterfallMessage(msg message, queueIndex int) bool {
	for queueIndex < len(ml.queues) {
		select {
		case ml.queues[queueIndex].msgs <- msg:
			ml.queues[queueIndex].pending.Inc()
			return true
		default:
			queueIndex++
		}
	}
	return false
}

// indexUtilization returns the highest priority queue that [utilization]
// falls below the cutoff point for
func (ml *multiLevelQueue) getPriorityIndex(validatorID ids.ShortID) int {
	utilization := ml.resourceManager.Utilization(validatorID)
	for i := 0; i < len(ml.cpuRanges); i++ {
		if utilization <= ml.cpuRanges[i] {
			return i
		}
	}

	// If the CPU utilization is greater than even the lowest priority CPU range
	// return the index of the bottom queue
	return len(ml.cpuRanges) - 1
}

type singleLevelQueue struct {
	msgs chan message

	pending     prometheus.Gauge
	waitingTime prometheus.Histogram
}
