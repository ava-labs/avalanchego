package router

import (
	"errors"
	"sync"
	"time"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow/validators"
	"github.com/ava-labs/gecko/utils/logging"
	"github.com/prometheus/client_golang/prometheus"
)

var (
	errNoMessages = errors.New("No messages remaining on queue")
)

type MessageQueue interface {
	PopMessage() (message, error)    // Pop the next message from the queue
	PushMessage(message) bool        // Push a message to the queue
	UtilizeCPU(ids.ShortID, float64) // Registers consumption of CPU time
	EndInterval()                    // Register end of an interval of real time
	Shutdown()
}

// Implements MessageQueue using a multi-level queue of FIFO queues
type multiLevelQueue struct {
	lock sync.Mutex

	validators validators.Set
	throttler  Throttler

	// Tracks total CPU consumption
	intervalConsumption, tierConsumption, cpuInterval float64

	bufferSize, pendingMessages, currentTier int

	queues        []singleLevelQueue
	cpuRanges     []float64 // CPU Utilization ranges that should be attributed to a corresponding queue
	cpuAllotments []float64 // Allotments of CPU time per cycle that should be spent on each level of queue

	semaChan chan struct{}

	log     logging.Logger
	metrics *metrics
}

// Create MultilevelQueue and counting semaphore for signaling when messages are available
// to read from the queue. The length of consumptionRanges and consumptionAllotments
// defines the range of priorities for the multi-level queue and the amount of time to
// spend on each level. Their length must be the same.
func NewMultiLevelQueue(
	vdrs validators.Set,
	log logging.Logger,
	metrics *metrics,
	consumptionRanges []float64,
	consumptionAllotments []float64,
	bufferSize int,
	cpuInterval float64,
	stakerPortion float64,
) (MessageQueue, chan struct{}) {
	semaChan := make(chan struct{}, bufferSize)
	singleLevelSize := bufferSize / len(consumptionRanges)
	throttler := NewEWMAThrottler(vdrs, uint32(bufferSize), stakerPortion, cpuInterval, log)
	queues := make([]singleLevelQueue, len(consumptionRanges))
	for index := 0; index < len(queues); index++ {
		gauge, histogram, err := metrics.registerTierStatistics(index)
		// An error should only occur while registering (not creating) the gauge and histogram
		// so if there is a non-nil error, it is safe to log the error and proceed as normal.
		if err != nil {
			log.Error("Failed to register metrics for tier %d of message queue", index)
		}
		queues[index] = singleLevelQueue{
			msgs:        make(chan message, singleLevelSize),
			pending:     gauge,
			waitingTime: histogram,
		}
	}

	return &multiLevelQueue{
		validators:    vdrs,
		throttler:     throttler,
		queues:        queues,
		cpuRanges:     consumptionRanges,
		cpuAllotments: consumptionAllotments,
		cpuInterval:   cpuInterval,
		log:           log,
		metrics:       metrics,
		bufferSize:    bufferSize,
		semaChan:      semaChan,
	}, semaChan
}

// PushMessage attempts to add a message to the queue and
// increments the counting semaphore if successful.
func (ml *multiLevelQueue) PushMessage(msg message) bool {
	ml.lock.Lock()
	defer ml.lock.Unlock()

	// If the message queue is already full, skip iterating
	// through the queue levels to return false
	if ml.pendingMessages >= ml.bufferSize {
		ml.log.Debug("Dropped message due to a full message queue with %d messages", ml.pendingMessages)
		return false
	}
	// If the message was added successfully, increment the counting sema
	// and notify the throttler of the pending message
	if ml.pushMessage(msg) {
		ml.pendingMessages++
		ml.throttler.AddMessage(msg.validatorID)
		select {
		case ml.semaChan <- struct{}{}:
		default:
			ml.log.Error("Sempahore channel was full after pushing message to the message queue")
		}
		ml.metrics.pending.Inc()
		return true
	} else {
		ml.log.Debug("Dropped message during push: %s", msg)
		ml.metrics.pending.Dec()
		ml.metrics.dropped.Inc()
		return false
	}
}

// PopMessage attempts to read the next message from the queue
func (ml *multiLevelQueue) PopMessage() (message, error) {
	ml.lock.Lock()
	defer ml.lock.Unlock()

	msg, err := ml.popMessage()
	if err == nil {
		ml.pendingMessages--
		ml.throttler.RemoveMessage(msg.validatorID)
		ml.metrics.pending.Dec()
	}
	return msg, err
}

// UtilizeCPU...
func (ml *multiLevelQueue) UtilizeCPU(vdr ids.ShortID, time float64) {
	ml.lock.Lock()
	defer ml.lock.Unlock()

	ml.throttler.UtilizeCPU(vdr, time)
	ml.intervalConsumption += time
	ml.tierConsumption += time
	if ml.tierConsumption > ml.cpuAllotments[ml.currentTier] {
		ml.tierConsumption = 0
		ml.currentTier++
	}
	if ml.currentTier >= len(ml.queues) {
		ml.currentTier = 0
	}
}

// EndInterval marks the end of a regular interval of CPU time
func (ml *multiLevelQueue) EndInterval() {
	ml.lock.Lock()
	defer ml.lock.Unlock()

	ml.throttler.EndInterval()
	ml.metrics.cpu.Observe(ml.intervalConsumption)
	ml.intervalConsumption = 0
}

// Shutdown closes the sema channel
// After Shutdown is called, PushMessage must never be called on multiLevelQueue again
func (ml *multiLevelQueue) Shutdown() {
	ml.lock.Lock()
	defer ml.lock.Unlock()

	close(ml.semaChan)
}

// popMessage grabs a message from the current queue. If there
// are no pending messages on the current queue then search it
// searches lower level queues before cycling back to the highest
// priority queue and stopping if it reaches the original queue
// it started on.
// Assumes the lock is held
func (ml *multiLevelQueue) popMessage() (message, error) {
	startTier := ml.currentTier
	for {
		select {
		case msg := <-ml.queues[ml.currentTier].msgs:
			ml.queues[ml.currentTier].pending.Dec()
			ml.queues[ml.currentTier].waitingTime.Observe(float64(time.Since(msg.received)))
			return msg, nil
		default:
			ml.currentTier++
			ml.tierConsumption = 0
			if ml.currentTier >= len(ml.queues) {
				ml.currentTier = 0
			}
			if ml.currentTier == startTier {
				return message{}, errNoMessages
			}
		}
	}
}

// pushMessage adds a message to the appropriate level (or lower)
// Assumes the lock is held
func (ml *multiLevelQueue) pushMessage(msg message) bool {
	validatorID := msg.validatorID
	if validatorID.IsZero() {
		ml.log.Warn("Dropping message due to invalid validatorID")
		return false
	}
	cpu, throttle := ml.throttler.GetUtilization(validatorID)
	if throttle {
		ml.log.Debug("Throttled message from validator: %s with CPU Utilization: %f\nmessage: %s", validatorID, cpu, msg.String())
		ml.metrics.throttled.Inc()
		return false
	}

	// Find the index of the appropriate queue for the message
	index := len(ml.cpuRanges)
	for i := 0; i < len(ml.cpuRanges); i++ {
		if cpu <= ml.cpuRanges[i] {
			index = i
			break
		}
	}

	// Attempt to add the message to the appropriate queue
	// If it's full, waterfall the message to a lower queue if possible
	for index < len(ml.queues) {
		select {
		case ml.queues[index].msgs <- msg:
			ml.queues[index].pending.Inc()
			return true
		default:
			index++
		}
	}
	return false
}

type singleLevelQueue struct {
	msgs chan message

	pending     prometheus.Gauge
	waitingTime prometheus.Histogram
}
