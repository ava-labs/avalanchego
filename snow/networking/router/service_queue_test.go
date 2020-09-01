// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package router

import (
	"math"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/gecko/snow/networking/throttler"
	"github.com/ava-labs/gecko/snow/validators"
	"github.com/ava-labs/gecko/utils/logging"
)

func setupMultiLevelQueue(t *testing.T, bufferSize int) (messageQueue, chan struct{}, validators.Set) {
	vdrs := validators.NewSet()
	metrics := &metrics{}
	metrics.Initialize("", prometheus.NewRegistry())
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

	queue, semaChan := newMultiLevelQueue(
		vdrs,
		logging.NoLog{},
		metrics,
		consumptionRanges,
		consumptionAllotments,
		bufferSize,
		throttler.DefaultMaxNonStakerPendingMsgs,
		time.Second,
		throttler.DefaultStakerPortion,
		throttler.DefaultStakerPortion,
	)

	return queue, semaChan, vdrs
}

func TestMultiLevelQueueSendsMessages(t *testing.T) {
	bufferSize := 8
	queue, semaChan, vdrs := setupMultiLevelQueue(t, bufferSize)
	vdrList := []validators.Validator{}
	messages := []message{}
	for i := 0; i < bufferSize; i++ {
		vdr := validators.GenerateRandomValidator(2)
		messages = append(messages, message{
			validatorID: vdr.ID(),
		})
		vdrList = append(vdrList, vdr)
	}

	vdrs.Set(vdrList)
	queue.EndInterval()

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

	// Ensure that the 6th message was never added to the queue
	select {
	case <-semaChan:
		t.Fatal("Semaphore channel should have been empty after reading all messages from the queue")
	default:
	}
}

func TestExtraMessageDeadlock(t *testing.T) {
	bufferSize := 8
	oversizedBuffer := bufferSize * 2
	queue, semaChan, vdrs := setupMultiLevelQueue(t, bufferSize)

	vdrList := []validators.Validator{}
	messages := []message{}
	for i := 0; i < oversizedBuffer; i++ {
		vdr := validators.GenerateRandomValidator(2)
		messages = append(messages, message{
			validatorID: vdr.ID(),
		})
		vdrList = append(vdrList, vdr)
	}

	vdrs.Set(vdrList)
	queue.EndInterval()

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
	vdrs.Set([]validators.Validator{
		validator1,
		validator2,
	})

	metrics := &metrics{}
	metrics.Initialize("", prometheus.NewRegistry())
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

	queue, semaChan := newMultiLevelQueue(
		vdrs,
		logging.NoLog{},
		metrics,
		consumptionRanges,
		consumptionAllotments,
		bufferSize,
		throttler.DefaultMaxNonStakerPendingMsgs,
		time.Second,
		throttler.DefaultStakerPortion,
		throttler.DefaultStakerPortion,
	)

	// Utilize CPU such that the next message from validator2 will be placed on a lower
	// level queue (but be sure not to consume the entire CPU allotment for tier1)
	queue.UtilizeCPU(validator2.ID(), perTier/2)

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
	queue.UtilizeCPU(validator1.ID(), perTier)

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
	vdrs.Set([]validators.Validator{
		vdr0,
		vdr1,
	})

	metrics := &metrics{}
	metrics.Initialize("", prometheus.NewRegistry())
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

	queue, semaChan := newMultiLevelQueue(
		vdrs,
		logging.NoLog{},
		metrics,
		consumptionRanges,
		consumptionAllotments,
		bufferSize,
		throttler.DefaultMaxNonStakerPendingMsgs,
		time.Second,
		throttler.DefaultStakerPortion,
		throttler.DefaultStakerPortion,
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
	msg, err := queue.PopMessage()
	if err != nil {
		t.Fatalf("Popping first message errored: %s", err)
	}
	if !msg.validatorID.Equals(vdr0.ID()) {
		t.Fatal("Expected first message to come from vdr0")
	}

	// Utilize enough CPU so that messages from vdr0 will be placed in a lower
	// priority queue, but not exhaust the time spent processing messages from
	// the highest priority queue
	queue.UtilizeCPU(vdr0.ID(), time.Second/2)

	<-semaChan
	msg, err = queue.PopMessage()
	if err != nil {
		t.Fatalf("Popping second message errored: %s", err)
	}
	if !msg.validatorID.Equals(vdr1.ID()) {
		t.Fatal("Expected second message to come from vdr1 after vdr0 dropped in priority")
	}

	<-semaChan
	msg, err = queue.PopMessage()
	if err != nil {
		t.Fatalf("Popping third message errored: %s", err)
	}
	if !msg.validatorID.Equals(vdr0.ID()) {
		t.Fatal("Expected third message to come from vdr0")
	}
}
