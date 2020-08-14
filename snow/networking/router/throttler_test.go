package router

import (
	"testing"
	"time"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow/validators"
	"github.com/ava-labs/gecko/utils/logging"
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
	period := float64(time.Second)
	throttler := NewEWMAThrottler(vdrs, maxMessages, msgPortion, cpuPortion, period, logging.NoLog{})

	throttler.UtilizeCPU(validator0.ID(), float64(25*time.Millisecond))
	throttler.UtilizeCPU(validator1.ID(), float64(50*time.Millisecond))

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
	period := float64(time.Second)
	throttler := NewEWMAThrottler(vdrs, maxMessages, msgPortion, cpuPortion, period, logging.NoLog{})

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

	vdrs.Add(staker0)
	vdrs.Add(staker1)

	maxMessages := uint32(16)
	msgPortion := 0.25
	cpuPortion := 0.25
	period := float64(time.Second)
	throttler := NewEWMAThrottler(vdrs, maxMessages, msgPortion, cpuPortion, period, logging.NoLog{})

	// Message Allotment: 0.5 * 0.25 * 15 = 2
	// Message Pool: 12 messages
	// Validator should be throttled iff it has exceeded its message allotment and the shared
	// message pool is empty

	// staker0 consumes its own allotment plus 10 messages from the shared pool
	for i := 0; i < 12; i++ {
		throttler.AddMessage(staker0.ID())
	}

	for i := 0; i < 3; i++ {
		throttler.AddMessage(staker1.ID())
		if _, throttle := throttler.GetUtilization(staker1.ID()); throttle {
			t.Fatal("Should not throttle message from staker until it has exceeded its own allotment")
		}
	}

	// Consume the last message and one extra message from the shared pool
	throttler.AddMessage(nonStaker0)
	throttler.AddMessage(nonStaker0)
	throttler.AddMessage(nonStaker0)

	if _, throttle := throttler.GetUtilization(staker1.ID()); !throttle {
		t.Fatal("Should have throttled message from staker after it exceeded its own allotment and the shared pool was empty")
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
	period := float64(time.Second)
	throttler := NewEWMAThrottler(vdrs, maxMessages, msgPortion, stakerPortion, period, logging.NoLog{})

	// Spend X CPU time in consecutive intervals and ensure that the throttler correctly calculates EWMA
	spends := []float64{
		23.2,
		23894.5,
		130482349732.12,
		23984.32,
		2382.54,
	}

	ewma := 0.0
	decayFactor := defaultDecayFactor
	for _, spend := range spends {
		ewma += spend
		ewma /= decayFactor

		throttler.UtilizeCPU(validator0.ID(), spend)
		throttler.EndInterval()
	}

	ewmat := throttler.(*ewmaThrottler)
	sp := ewmat.getSpender(validator0.ID())
	if sp.ewma != ewma {
		t.Fatalf("EWMA Throttler calculated EWMA incorrectly, expected: %f, but calculated: %f", ewma, sp.ewma)
	}
}
