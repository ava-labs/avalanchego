// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package throttler

import (
	"testing"
	"time"

	"github.com/ava-labs/avalanche-go/ids"
	"github.com/ava-labs/avalanche-go/snow/validators"
	"github.com/ava-labs/avalanche-go/utils/logging"
)

func TestEWMATrackerPrioritizes(t *testing.T) {
	vdrs := validators.NewSet()

	vdr0 := ids.GenerateTestShortID()
	vdr1 := ids.GenerateTestShortID()
	nonStaker := ids.GenerateTestShortID()

	vdrs.AddWeight(vdr0, 1)
	vdrs.AddWeight(vdr1, 1)

	cpuPortion := 0.25
	period := time.Second
	throttler := NewEWMATracker(vdrs, cpuPortion, period, logging.NoLog{})

	throttler.UtilizeCPU(vdr0, 25*time.Millisecond)
	throttler.UtilizeCPU(vdr1, 5*time.Second)

	cpu0 := throttler.GetUtilization(vdr0)
	cpu1 := throttler.GetUtilization(vdr1)
	cpuNonStaker := throttler.GetUtilization(nonStaker)

	if cpu1 <= cpu0 {
		t.Fatalf("CPU utilization for vdr1: %f should be greater than that of vdr0: %f", cpu1, cpu0)
	}

	if cpuNonStaker < cpu1 {
		t.Fatalf("CPU Utilization for non-staker: %f should be greater than or equal to the CPU Utilization for the highest spending staker: %f", cpuNonStaker, cpu1)
	}
}

func TestEWMATrackerPrunesSpenders(t *testing.T) {
	vdrs := validators.NewSet()

	staker0 := ids.GenerateTestShortID()
	staker1 := ids.GenerateTestShortID()
	nonStaker0 := ids.GenerateTestShortID()
	nonStaker1 := ids.GenerateTestShortID()

	vdrs.AddWeight(staker0, 1)
	vdrs.AddWeight(staker1, 1)

	cpuPortion := 0.25
	period := time.Second
	throttler := NewEWMATracker(vdrs, cpuPortion, period, logging.NoLog{})

	throttler.UtilizeCPU(staker0, 1.0)
	throttler.UtilizeCPU(nonStaker0, 1.0)

	// 3 Cases:
	// 		Stakers should not be pruned
	// 		Non-stakers with non-zero cpuEWMA should not be pruned
	// 		Non-stakers with cpuEWMA of 0 should be pruned

	// After 64 intervals nonStaker0 should be removed because its cpuEWMA statistic should reach 0
	// while nonStaker1 utilizes the CPU in every interval, so it should not be removed.
	for i := 0; i < 64; i++ {
		throttler.UtilizeCPU(nonStaker1, 1.0)
		throttler.EndInterval()
	}

	// Ensure that the validators and the non-staker heard from every interval were not pruned
	ewmat := throttler.(*ewmaCPUTracker)
	if _, ok := ewmat.cpuSpenders[staker0.Key()]; !ok {
		t.Fatal("Staker was pruned from the set of spenders")
	}
	if _, ok := ewmat.cpuSpenders[staker1.Key()]; !ok {
		t.Fatal("Staker was pruned from the set of spenders")
	}
	if _, ok := ewmat.cpuSpenders[nonStaker0.Key()]; ok {
		t.Fatal("Non-staker, not heard from in 64 periods, should have been pruned from the set of spenders")
	}
	if _, ok := ewmat.cpuSpenders[nonStaker1.Key()]; ok {
		t.Fatal("Non-staker heard from in every period, was pruned from the set of spenders")
	}
}

func TestMessageThrottlerPrunesSpenders(t *testing.T) {
	vdrs := validators.NewSet()

	staker0 := ids.GenerateTestShortID()
	staker1 := ids.GenerateTestShortID()
	nonStaker0 := ids.GenerateTestShortID()
	nonStaker1 := ids.GenerateTestShortID()
	nonStaker2 := ids.GenerateTestShortID()

	vdrs.AddWeight(staker0, 1)
	vdrs.AddWeight(staker1, 1)

	maxMessages := uint32(1024)
	msgPortion := 0.25

	throttler := NewMessageThrottler(vdrs, maxMessages, DefaultMaxNonStakerPendingMsgs, msgPortion, logging.NoLog{})

	// 4 Cases:
	// 		Stakers should not be pruned
	// 		Non-stakers with pending messages should not be pruned
	// 		Non-stakers heard from recently should not be pruned
	// 		Non-stakers not heard from in [defaultIntervalsUntilPruning] should be pruned

	// Add pending messages for nonStaker1 and nonStaker2
	throttler.Add(nonStaker2) // Will not be removed, so it should not be pruned
	throttler.Add(nonStaker1)

	throttler.EndInterval()
	throttler.Remove(nonStaker1) // The pending message was removed, so nonStaker1 should be pruned
	throttler.EndInterval()
	intervalsUntilPruning := int(defaultIntervalsUntilPruning)
	// Let the required number of intervals elapse to allow nonStaker1 to be pruned
	for i := 0; i < intervalsUntilPruning; i++ {
		throttler.Add(nonStaker0) // nonStaker0 is heard from in every interval, so it should not be pruned
		throttler.EndInterval()
		throttler.Remove(nonStaker0)
	}

	msgThrottler := throttler.(*messageThrottler)
	if _, ok := msgThrottler.msgSpenders[staker0.Key()]; !ok {
		t.Fatal("Staker was pruned from the set of spenders")
	}
	if _, ok := msgThrottler.msgSpenders[staker1.Key()]; !ok {
		t.Fatal("Staker was pruned from the set of spenders")
	}
	if _, ok := msgThrottler.msgSpenders[nonStaker0.Key()]; !ok {
		t.Fatal("Non-staker heard from within [intervalsUntilPruning] was removed from the set of spenders")
	}
	if _, ok := msgThrottler.msgSpenders[nonStaker1.Key()]; ok {
		t.Fatal("Non-staker not heard from within [intervalsUntilPruning] was not removed from the set of spenders")
	}
	if _, ok := msgThrottler.msgSpenders[nonStaker2.Key()]; !ok {
		t.Fatal("Non-staker with a pending message was pruned from the set of spenders")
	}
}

func TestMessageThrottling(t *testing.T) {
	vdrs := validators.NewSet()

	staker0 := ids.GenerateTestShortID()
	staker1 := ids.GenerateTestShortID()
	nonStaker0 := ids.GenerateTestShortID()
	nonStaker1 := ids.GenerateTestShortID()

	vdrs.AddWeight(staker0, 1)
	vdrs.AddWeight(staker1, 1)

	maxMessages := uint32(8)
	msgPortion := 0.25
	throttler := NewMessageThrottler(vdrs, maxMessages, DefaultMaxNonStakerPendingMsgs, msgPortion, logging.NoLog{})

	// Message Allotment: 0.5 * 0.25 * 8 = 1
	// Message Pool: 8 * 0.75 = 6 messages
	// Max Messages: 1 + DefaultMaxNonStakerPendingMsgs
	// Validator should be throttled if it has exceeded its max messages
	// or it has exceeded its message allotment and the shared message pool is empty.

	// staker0 consumes its entire message allotment

	// Ensure that it is allowed to consume its entire max messages before being throttled
	for i := 0; i < int(DefaultMaxNonStakerPendingMsgs)+1; i++ {
		throttler.Add(staker0)
		if throttler.Throttle(staker0) {
			t.Fatal("Should not throttle message from staker until it has exceeded its own allotment")
		}
	}

	// Ensure staker is throttled after exceeding its own max messages cap
	throttler.Add(staker0)
	if !throttler.Throttle(staker0) {
		t.Fatal("Should have throttled message after exceeding message cap")
	}

	// Remove messages to reduce staker0 to have its normal message allotment in pending
	for i := 0; i < int(DefaultMaxNonStakerPendingMsgs)+1; i++ {
		throttler.Remove(staker0)
	}

	// Consume the entire message pool among two non-stakers
	for i := 0; i < int(DefaultMaxNonStakerPendingMsgs); i++ {
		throttler.Add(nonStaker0)
		throttler.Add(nonStaker1)

		// Neither should be throttled because they are only consuming until their own messsage cap
		// and the shared pool has been emptied.
		if throttler.Throttle(nonStaker0) {
			t.Fatalf("Should not have throttled message from nonStaker0 after %d messages", i)
		}
		if throttler.Throttle(nonStaker1) {
			t.Fatalf("Should not have throttled message from nonStaker1 after %d messages", i)
		}
	}

	// An additional message from staker0 should now cause it to be throttled since the mesasage pool
	// has been emptied.
	if throttler.Throttle(staker0) {
		t.Fatal("Should not have throttled message from staker until it had exceeded its message allotment.")
	}
	throttler.Add(staker0)
	if !throttler.Throttle(staker0) {
		t.Fatal("Should have throttled message from staker0 after it exceeded its message allotment because the message pool was empty.")
	}

	if !throttler.Throttle(nonStaker0) {
		t.Fatal("Should have throttled message from nonStaker0 after the message pool was emptied")
	}

	if !throttler.Throttle(nonStaker1) {
		t.Fatal("Should have throttled message from nonStaker1 after the message pool was emptied")
	}
}

func TestCalculatesEWMA(t *testing.T) {
	vdrs := validators.NewSet()

	vdr0 := ids.GenerateTestShortID()
	vdr1 := ids.GenerateTestShortID()

	vdrs.AddWeight(vdr0, 1)
	vdrs.AddWeight(vdr1, 1)

	stakerPortion := 0.25
	period := time.Second
	throttler := NewEWMATracker(vdrs, stakerPortion, period, logging.NoLog{})

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

		throttler.UtilizeCPU(vdr0, spend)
		throttler.EndInterval()
	}

	ewmat := throttler.(*ewmaCPUTracker)
	sp := ewmat.getSpender(vdr0)
	if sp.cpuEWMA != ewma {
		t.Fatalf("EWMA Throttler calculated EWMA incorrectly, expected: %s, but calculated: %s", ewma, sp.cpuEWMA)
	}
}
