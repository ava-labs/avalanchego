// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package throttler

import (
	"testing"
	"time"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow/validators"
	"github.com/ava-labs/gecko/utils/logging"
)

const (
	defaultMaxNonStakerPendingMsgs uint32 = 3
)

func TestEWMAThrottler(t *testing.T) {
	vdrs := validators.NewSet()
	validator0 := validators.GenerateRandomValidator(1)
	validator1 := validators.GenerateRandomValidator(1)
	vdrs.Add(validator0)
	vdrs.Add(validator1)

	maxMessages := uint32(16)
	msgPortion := 0.25
	cpuPortion := 0.25
	period := time.Second
	throttler := NewEWMAThrottler(vdrs, maxMessages, defaultMaxNonStakerPendingMsgs, msgPortion, cpuPortion, period, logging.NoLog{})

	throttler.UtilizeCPU(validator0.ID(), 25*time.Millisecond)
	throttler.UtilizeCPU(validator1.ID(), 5*time.Second)

	cpu0, throttle0 := throttler.GetUtilization(validator0.ID())
	cpu1, throttle1 := throttler.GetUtilization(validator1.ID())

	if throttle0 {
		t.Fatalf("Should not throttle validator0 with no pending messages")
	}
	if throttle1 {
		t.Fatalf("Should not throttle validator1 with no pending messages")
	}

	if cpu1 <= cpu0 {
		t.Fatalf("CPU utilization for validator1: %f should be greater than that of validator0: %f", cpu1, cpu0)
	}

	// Test that throttler prevents unknown validators from taking up half the message queue
	for i := uint32(0); i < maxMessages; i++ {
		throttler.AddMessage(ids.NewShortID([20]byte{byte(i)}))
	}

	_, throttle := throttler.GetUtilization(ids.NewShortID([20]byte{'s', 'y', 'b', 'i', 'l'}))
	if !throttle {
		t.Fatal("Throttler should have started throttling messages from unknown peers")
	}
}

func TestThrottlerPrunesSpenders(t *testing.T) {
	vdrs := validators.NewSet()
	staker0 := validators.GenerateRandomValidator(1)
	staker1 := validators.GenerateRandomValidator(1)
	nonStaker0 := ids.NewShortID([20]byte{1})
	nonStaker1 := ids.NewShortID([20]byte{2})
	nonStaker2 := ids.NewShortID([20]byte{3})

	vdrs.Add(staker0)
	vdrs.Add(staker1)

	maxMessages := uint32(1024)
	cpuPortion := 0.25
	msgPortion := 0.25
	period := time.Second
	throttler := NewEWMAThrottler(vdrs, maxMessages, defaultMaxNonStakerPendingMsgs, msgPortion, cpuPortion, period, logging.NoLog{})

	throttler.AddMessage(nonStaker2) // nonStaker2 should not be removed with a pending message
	throttler.UtilizeCPU(nonStaker0, 1.0)
	throttler.UtilizeCPU(nonStaker1, 1.0)
	intervalsUntilPruning := int(defaultIntervalsUntilPruning)
	// Let two intervals pass with no activity to ensure that nonStaker1 can be pruned
	throttler.EndInterval()
	throttler.EndInterval()
	throttler.UtilizeCPU(nonStaker0, 1.0)
	// Let the required number of intervals elapse to allow nonStaker1 to be pruned
	for i := 0; i < intervalsUntilPruning; i++ {
		throttler.EndInterval()
	}

	// Ensure that the validators and the non-staker heard from in the past [intervalsUntilPruning] were not pruned
	ewmat := throttler.(*ewmaThrottler)
	if _, ok := ewmat.spenders[staker0.ID().Key()]; !ok {
		t.Fatal("Staker was pruned from the set of spenders")
	}
	if _, ok := ewmat.spenders[staker1.ID().Key()]; !ok {
		t.Fatal("Staker was pruned from the set of spenders")
	}
	if _, ok := ewmat.spenders[nonStaker0.Key()]; !ok {
		t.Fatal("Non-staker heard from recently was pruned from the set of spenders")
	}
	if _, ok := ewmat.spenders[nonStaker1.Key()]; ok {
		t.Fatal("Non-staker not heard from in a long time was not pruned from the set of spenders")
	}
	if _, ok := ewmat.spenders[nonStaker2.Key()]; !ok {
		t.Fatal("Non-staker with a pending message was pruned from the set of spenders")
	}
}

func TestThrottleStaker(t *testing.T) {
	vdrs := validators.NewSet()
	staker0 := validators.GenerateRandomValidator(1)
	staker1 := validators.GenerateRandomValidator(1)
	nonStaker0 := ids.NewShortID([20]byte{1})
	nonStaker1 := ids.NewShortID([20]byte{2})

	vdrs.Add(staker0)
	vdrs.Add(staker1)

	maxMessages := uint32(9)
	msgPortion := 0.25
	cpuPortion := 0.25
	period := time.Second
	throttler := NewEWMAThrottler(vdrs, maxMessages, defaultMaxNonStakerPendingMsgs, msgPortion, cpuPortion, period, logging.NoLog{})

	// Message Allotment: 0.5 * 0.25 * 8 = 1
	// Message Pool: 6 messages
	// Max Messages: 1 + defaultMaxNonStakerPendingMsgs
	// Validator should be throttled if it has exceeded its max messages
	// or it has exceeded its message allotment and the shared message pool is empty.

	// staker0 consumes its entire message allotment

	// Ensure that it is allowed to consume its entire max messages before being throttled
	for i := 0; i < int(defaultMaxNonStakerPendingMsgs)+1; i++ {
		throttler.AddMessage(staker0.ID())
		if _, throttle := throttler.GetUtilization(staker0.ID()); throttle {
			t.Fatal("Should not throttle message from staker until it has exceeded its own allotment")
		}
	}

	throttler.AddMessage(staker0.ID())
	if _, throttle := throttler.GetUtilization(staker0.ID()); !throttle {
		t.Fatal("Should have throttled message after exceeding message")
	}

	// Remove messages to reduce staker0 to have its normal message allotment pending
	for i := 0; i < int(defaultMaxNonStakerPendingMsgs); i++ {
		throttler.RemoveMessage(staker0.ID())
	}

	// Consume the entire message pool among two non-stakers
	for i := 0; i < int(defaultMaxNonStakerPendingMsgs); i++ {
		throttler.AddMessage(nonStaker0)
		throttler.AddMessage(nonStaker1)

		// Neither should be throttled because they are only consuming until their own messsage cap
		// and the shared pool has been emptied.
		if _, throttle := throttler.GetUtilization(nonStaker0); throttle {
			t.Fatalf("Should not have throttled message from nonStaker0 after %d messages", i)
		}
		if _, throttle := throttler.GetUtilization(nonStaker1); throttle {
			t.Fatalf("Should not have throttled message from nonStaker1 after %d messages", i)
		}
	}

	// An additional message from staker0 should now cause it to be throttled since the mesasage pool
	// has been emptied.
	if _, throttle := throttler.GetUtilization(staker0.ID()); throttle {
		t.Fatal("Should not have throttled message from staker until it had exceeded its message allotment.")
	}
	throttler.AddMessage(staker0.ID())
	if _, throttle := throttler.GetUtilization(staker0.ID()); !throttle {
		t.Fatal("Should have throttled message from staker0 after it exceeded its message allotment because the message pool was empty.")
	}
}

func TestCalculatesEWMA(t *testing.T) {
	vdrs := validators.NewSet()
	validator0 := validators.GenerateRandomValidator(1)
	validator1 := validators.GenerateRandomValidator(1)
	vdrs.Add(validator0)
	vdrs.Add(validator1)

	maxMessages := uint32(16)
	msgPortion := 0.25
	stakerPortion := 0.25
	period := time.Second
	throttler := NewEWMAThrottler(vdrs, maxMessages, defaultMaxNonStakerPendingMsgs, msgPortion, stakerPortion, period, logging.NoLog{})

	// Spend X CPU time in consecutive intervals and ensure that the throttler correctly calculates EWMA
	spends := []time.Duration{
		23,
		23894,
		130482349732,
		23984,
		2382,
	}

	ewma := time.Duration(0)
	decayFactor := defaultDecayFactor
	for _, spend := range spends {
		ewma += spend
		ewma = time.Duration(float64(ewma) / decayFactor)

		throttler.UtilizeCPU(validator0.ID(), spend)
		throttler.EndInterval()
	}

	ewmat := throttler.(*ewmaThrottler)
	sp := ewmat.getSpender(validator0.ID())
	if sp.cpuEWMA != ewma {
		t.Fatalf("EWMA Throttler calculated EWMA incorrectly, expected: %s, but calculated: %s", ewma, sp.cpuEWMA)
	}
}
