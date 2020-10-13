// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package router

import (
	"math"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/networking/tracker"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/uptime"
)

// returns a new multi-level queue that will never throttle or prioritize
func setupMultiLevelQueue(t *testing.T, bufferSize int) (messageQueue, chan struct{}) {
	metrics := &metrics{}
	if err := metrics.Initialize("", prometheus.NewRegistry()); err != nil {
		t.Fatal(err)
	}
	consumptionRanges := []float64{
		0.5,
		0.75,
		1.5,
		math.MaxFloat64,
	}

	cpuInterval := defaultCPUInterval
	// Defines the percentage of CPU time allotted to processing messages
	// from the bucket at the corresponding index.
	consumptionAllotments := []time.Duration{
		cpuInterval / 4,
		cpuInterval / 4,
		cpuInterval / 4,
		cpuInterval / 4,
	}

	resourceManager := newInfiniteResourcePoolManager()
	queue, semaChan := newMultiLevelQueue(
		resourceManager,
		consumptionRanges,
		consumptionAllotments,
		bufferSize,
		logging.NoLog{},
		metrics,
	)

	return queue, semaChan
}

func TestMultiLevelQueueSendsMessages(t *testing.T) {
	bufferSize := 8
	queue, semaChan := setupMultiLevelQueue(t, bufferSize)
	messages := []message{}
	for i := 0; i < bufferSize; i++ {
		messages = append(messages, message{
			validatorID: ids.NewShortID([20]byte{byte(i)}),
		})
	}

	for _, msg := range messages {
		queue.PushMessage(msg)
	}

	for count := 0; count < bufferSize; count++ {
		select {
		case _, ok := <-semaChan:
			if !ok {
				t.Fatal("Semaphore channel was closed early unexpectedly")
			}
			if _, err := queue.PopMessage(); err != nil {
				t.Fatalf("Pop message failed with error: %s", err)
			}
		default:
			t.Fatalf("Should have read message %d from queue", count)
		}
	}

	select {
	case <-semaChan:
		t.Fatal("Semaphore channel should have been empty after reading all messages from the queue")
	default:
	}
}

func TestExtraMessageNoDeadlock(t *testing.T) {
	bufferSize := 8
	oversizedBuffer := bufferSize * 2
	queue, semaChan := setupMultiLevelQueue(t, bufferSize)

	messages := []message{}
	for i := 0; i < oversizedBuffer; i++ {
		messages = append(messages, message{
			validatorID: ids.NewShortID([20]byte{byte(i)}),
		})
	}

	// Test messages are dropped when full to avoid blocking when
	// adding a message to a queue or to the counting semaphore channel
	for _, msg := range messages {
		queue.PushMessage(msg)
	}

	// There should now be [bufferSize] messages on the queue
	// Note: this may not be the case where a message is dropped
	// because there is less than [bufferSize] room on the multi-level
	// queue as a result of rounding when calculating the size of the
	// single-level queues.
	for i := 0; i < bufferSize; i++ {
		<-semaChan
	}
	select {
	case <-semaChan:
		t.Fatal("Semaphore channel should have been empty")
	default:
	}
}

func TestMultiLevelQueuePrioritizes(t *testing.T) {
	bufferSize := 8
	vdrs := validators.NewSet()
	validator1 := validators.GenerateRandomValidator(2000)
	validator2 := validators.GenerateRandomValidator(2000)

	if err := vdrs.Set([]validators.Validator{
		validator1,
		validator2,
	}); err != nil {
		t.Fatal(err)
	}

	metrics := &metrics{}
	if err := metrics.Initialize("", prometheus.NewRegistry()); err != nil {
		t.Fatal(err)
	}
	// Set tier1 cutoff sufficiently low so that only messages from validators
	// the message queue has not serviced will be placed on it for the test.
	tier1 := 0.001
	tier2 := 1.0
	tier3 := math.MaxFloat64
	consumptionRanges := []float64{
		tier1,
		tier2,
		tier3,
	}

	perTier := time.Second
	// Give each tier 1 second of processing time
	consumptionAllotments := []time.Duration{
		perTier,
		perTier,
		perTier,
	}

	cpuTracker := tracker.NewCPUTracker(uptime.IntervalFactory{}, time.Second)
	msgTracker := tracker.NewMessageTracker()
	resourceManager := NewResourceManager(
		vdrs,
		logging.NoLog{},
		msgTracker,
		cpuTracker,
		uint32(bufferSize),
		DefaultMaxNonStakerPendingMsgs,
		DefaultStakerPortion,
		DefaultStakerPortion,
	)
	queue, semaChan := newMultiLevelQueue(
		resourceManager,
		consumptionRanges,
		consumptionAllotments,
		bufferSize,
		logging.NoLog{},
		metrics,
	)

	// Utilize CPU such that the next message from validator2 will be placed on a lower
	// level queue (but be sure not to consume the entire CPU allotment for tier1)
	startTime := time.Now()
	duration := perTier / 2
	endTime := startTime.Add(duration)
	queue.UtilizeCPU(validator2.ID(), duration)
	cpuTracker.UtilizeTime(validator2.ID(), startTime, endTime)

	// Push two messages from from high priority validator and one from
	// low priority validator
	messages := []message{
		{
			validatorID: validator1.ID(),
			requestID:   1,
		},
		{
			validatorID: validator1.ID(),
			requestID:   2,
		},
		{
			validatorID: validator2.ID(),
			requestID:   3,
		},
	}

	for _, msg := range messages {
		queue.PushMessage(msg)
	}

	<-semaChan
	if msg1, err := queue.PopMessage(); err != nil {
		t.Fatal(err)
	} else if !msg1.validatorID.Equals(validator1.ID()) {
		t.Fatal("Expected first message to come from the high priority validator")
	}

	// Utilize the remainder of the time that should be alloted to the highest priority
	// queue.
	duration = perTier
	queue.UtilizeCPU(validator1.ID(), duration)

	<-semaChan
	if msg2, err := queue.PopMessage(); err != nil {
		t.Fatal(err)
	} else if !msg2.validatorID.Equals(validator2.ID()) {
		t.Fatal("Expected second message to come from the low priority validator after moving on to the lower level queue")
	}

	<-semaChan
	if msg3, err := queue.PopMessage(); err != nil {
		t.Fatal(err)
	} else if !msg3.validatorID.Equals(validator1.ID()) {
		t.Fatal("Expected final message to come from validator1")
	}
}

func TestMultiLevelQueuePushesDownOldMessages(t *testing.T) {
	bufferSize := 16
	vdrs := validators.NewSet()
	vdr0 := validators.GenerateRandomValidator(2000)
	vdr1 := validators.GenerateRandomValidator(2000)

	if err := vdrs.Set([]validators.Validator{
		vdr0,
		vdr1,
	}); err != nil {
		t.Fatal(err)
	}

	metrics := &metrics{}
	if err := metrics.Initialize("", prometheus.NewRegistry()); err != nil {
		t.Fatal(err)
	}
	// Set tier1 cutoff sufficiently low so that only messages from validators
	// the message queue has not serviced will be placed on it for the test.
	tier1 := 0.001
	tier2 := 1.0
	tier3 := math.MaxFloat64
	consumptionRanges := []float64{
		tier1,
		tier2,
		tier3,
	}

	perTier := time.Second
	// Give each tier 1 second of processing time
	consumptionAllotments := []time.Duration{
		perTier,
		perTier,
		perTier,
	}

	cpuTracker := tracker.NewCPUTracker(uptime.IntervalFactory{}, time.Second)
	msgTracker := tracker.NewMessageTracker()
	resourceManager := NewResourceManager(
		vdrs,
		logging.NoLog{},
		msgTracker,
		cpuTracker,
		uint32(bufferSize),
		DefaultMaxNonStakerPendingMsgs,
		DefaultStakerPortion,
		DefaultStakerPortion,
	)
	queue, semaChan := newMultiLevelQueue(
		resourceManager,
		consumptionRanges,
		consumptionAllotments,
		bufferSize,
		logging.NoLog{},
		metrics,
	)

	queue.PushMessage(message{
		validatorID: vdr0.ID(),
		requestID:   1,
	})
	queue.PushMessage(message{
		validatorID: vdr0.ID(),
		requestID:   2,
	})
	queue.PushMessage(message{
		validatorID: vdr1.ID(),
		requestID:   3,
	})

	<-semaChan
	if msg, err := queue.PopMessage(); err != nil {
		t.Fatalf("Popping first message errored: %s", err)
	} else if !msg.validatorID.Equals(vdr0.ID()) {
		t.Fatal("Expected first message to come from vdr0")
	}

	// Utilize enough CPU so that messages from vdr0 will be placed in a lower
	// priority queue, but not exhaust the time spent processing messages from
	// the highest priority queue
	startTime := time.Now()
	duration := time.Second / 2
	endTime := startTime.Add(duration)
	queue.UtilizeCPU(vdr0.ID(), duration)
	cpuTracker.UtilizeTime(vdr0.ID(), startTime, endTime)

	<-semaChan
	if msg, err := queue.PopMessage(); err != nil {
		t.Fatalf("Popping second message errored: %s", err)
	} else if !msg.validatorID.Equals(vdr1.ID()) {
		t.Fatal("Expected second message to come from vdr1 after vdr0 dropped in priority")
	}

	<-semaChan
	if msg, err := queue.PopMessage(); err != nil {
		t.Fatalf("Popping third message errored: %s", err)
	} else if !msg.validatorID.Equals(vdr0.ID()) {
		t.Fatal("Expected third message to come from vdr0")
	}
}

func TestMultiLevelQueueFreesSpace(t *testing.T) {
	bufferSize := 8
	vdrs := validators.NewSet()
	validator1 := validators.GenerateRandomValidator(2000)
	validator2 := validators.GenerateRandomValidator(2000)
	if err := vdrs.Set([]validators.Validator{
		validator1,
		validator2,
	}); err != nil {
		t.Fatal(err)
	}

	metrics := &metrics{}
	if err := metrics.Initialize("", prometheus.NewRegistry()); err != nil {
		t.Fatal(err)
	}
	// Set tier1 cutoff sufficiently low so that only messages from validators
	// the message queue has not serviced will be placed on it for the test.
	tier1 := 0.001
	tier2 := 1.0
	tier3 := 2.0
	tier4 := math.MaxFloat64
	consumptionRanges := []float64{
		tier1,
		tier2,
		tier3,
		tier4,
	}

	perTier := time.Second
	// Give each tier 1 second of processing time
	consumptionAllotments := []time.Duration{
		perTier,
		perTier,
		perTier,
		perTier,
	}

	cpuTracker := tracker.NewCPUTracker(uptime.IntervalFactory{}, time.Second)
	msgTracker := tracker.NewMessageTracker()
	resourceManager := NewResourceManager(
		vdrs,
		logging.NoLog{},
		msgTracker,
		cpuTracker,
		uint32(bufferSize),
		DefaultMaxNonStakerPendingMsgs,
		DefaultStakerPortion,
		DefaultStakerPortion,
	)
	queue, semaChan := newMultiLevelQueue(
		resourceManager,
		consumptionRanges,
		consumptionAllotments,
		bufferSize,
		logging.NoLog{},
		metrics,
	)

	for i := 0; i < 4; i++ {
		validator1.ID()
		if success := queue.PushMessage(message{
			validatorID: validator1.ID(),
		}); !success {
			t.Fatalf("Failed to push message from validator1 on (Round 1, Iteration %d)", i)
		}
		if success := queue.PushMessage(message{
			validatorID: validator2.ID(),
		}); !success {
			t.Fatalf("Failed to push message from validator2 on (Round 1, Iteration %d)", i)
		}
	}

	// Empty the message pool
	for i := 0; i < bufferSize; i++ {
		<-semaChan
		if _, err := queue.PopMessage(); err != nil {
			t.Fatalf("Failed to pop message on iteration %d due to: %s", i, err)
		}

	}

	// Fill up message pool again to ensure
	// popping previous messages freed up space
	for i := 0; i < 4; i++ {
		if success := queue.PushMessage(message{
			validatorID: validator1.ID(),
		}); !success {
			t.Fatalf("Failed to push message from validator1 on (Round 2, Iteration %d)", i)
		}
		if success := queue.PushMessage(message{
			validatorID: validator2.ID(),
		}); !success {
			t.Fatalf("Failed to push message from validator2 on (Round 2, Iteration %d)", i)
		}
	}
}

func TestMultiLevelQueueThrottles(t *testing.T) {
	bufferSize := 8
	vdrs := validators.NewSet()
	validator1 := validators.GenerateRandomValidator(2000)
	validator2 := validators.GenerateRandomValidator(2000)
	if err := vdrs.Set([]validators.Validator{
		validator1,
		validator2,
	}); err != nil {
		t.Fatal(err)
	}

	metrics := &metrics{}
	if err := metrics.Initialize("", prometheus.NewRegistry()); err != nil {
		t.Fatal(err)
	}
	// Set tier1 cutoff sufficiently low so that only messages from validators
	// the message queue has not serviced will be placed on it for the test.
	tier1 := 0.001
	tier2 := 1.0
	tier3 := 2.0
	tier4 := math.MaxFloat64
	consumptionRanges := []float64{
		tier1,
		tier2,
		tier3,
		tier4,
	}

	perTier := time.Second
	// Give each tier 1 second of processing time
	consumptionAllotments := []time.Duration{
		perTier,
		perTier,
		perTier,
		perTier,
	}

	resourceManager := newNoResourcesManager()
	queue, _ := newMultiLevelQueue(
		resourceManager,
		consumptionRanges,
		consumptionAllotments,
		bufferSize,
		logging.NoLog{},
		metrics,
	)

	success := queue.PushMessage(message{
		validatorID: ids.NewShortID([20]byte{1}),
	})
	if success {
		t.Fatal("Expected multi-level queue to throttle message when there were no resources available")
	}
}
