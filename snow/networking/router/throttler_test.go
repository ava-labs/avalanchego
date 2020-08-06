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
	stakerPortion := 0.25
	period := float64(time.Second)
	throttler := NewEWMAThrottler(vdrs, maxMessages, stakerPortion, period, logging.NoLog{})

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

func TestValidatorStopsStaking(t *testing.T) {
	vdrs := validators.NewSet()
	validator0 := validators.GenerateRandomValidator(1)
	validator1 := validators.GenerateRandomValidator(1)
	nonValidator := validators.GenerateRandomValidator(1)
	vdrs.Add(validator0)
	vdrs.Add(validator1)

	maxMessages := uint32(16)
	stakerPortion := 0.25
	period := float64(time.Second)
	throttler := NewEWMAThrottler(vdrs, maxMessages, stakerPortion, period, logging.NoLog{})

	throttler.AddMessage(validator0.ID())
	throttler.AddMessage(validator0.ID())
	throttler.AddMessage(nonValidator.ID())

	vdrs.Remove(validator0.ID())

	ewmat, ok := throttler.(*ewmaThrottler)
	if !ok {
		t.Fatal("Throttler returned by NewEWMAThrottler should have been an instance of ewmaThrottler")
	}

	ewmat.getSpender(validator0.ID())
	if ewmat.nonStaker.pendingMessages != 3 {
		t.Fatalf("nonStaker should have had 3 pending messages after validator0 stopped staking, but had %d", ewmat.nonStaker.pendingMessages)
	}
}

func TestValidatorStartsStaking(t *testing.T) {
	vdrs := validators.NewSet()
	validator0 := validators.GenerateRandomValidator(1)
	validator1 := validators.GenerateRandomValidator(1)
	pendingValidator := validators.GenerateRandomValidator(1)
	vdrs.Add(validator0)
	vdrs.Add(validator1)

	maxMessages := uint32(16)
	stakerPortion := 0.25
	period := float64(time.Second)
	throttler := NewEWMAThrottler(vdrs, maxMessages, stakerPortion, period, logging.NoLog{})

	throttler.AddMessage(validator0.ID())
	throttler.AddMessage(pendingValidator.ID())
	throttler.AddMessage(pendingValidator.ID())

	vdrs.Add(pendingValidator)

	ewmat, ok := throttler.(*ewmaThrottler)
	if !ok {
		t.Fatal("Throttler returned by NewEWMAThrottler should have been an instance of ewmaThrottler")
	}

	ewmat.getSpender(pendingValidator.ID())
	if ewmat.nonStaker.pendingMessages != 0 {
		t.Fatalf("nonStaker should have had 0 pending messages after pendingValidator started staking, but had %d", ewmat.nonStaker.pendingMessages)
	}
}

func TestCalculatesEWMA(t *testing.T) {
	vdrs := validators.NewSet()
	validator0 := validators.GenerateRandomValidator(1)
	validator1 := validators.GenerateRandomValidator(1)
	vdrs.Add(validator0)
	vdrs.Add(validator1)

	maxMessages := uint32(16)
	stakerPortion := 0.25
	period := float64(time.Second)
	throttler := NewEWMAThrottler(vdrs, maxMessages, stakerPortion, period, logging.NoLog{})

	// Spend x amount in consecutive periods and ensure that EWMA is calcualted correctly
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

	ewmat, ok := throttler.(*ewmaThrottler)
	if !ok {
		t.Fatal("Throttler returned by NewEWMAThrottler should be an instance of ewmaThrottler")
	}
	sp, _ := ewmat.getSpender(validator0.ID())
	if sp.ewma != ewma {
		t.Fatalf("EWMA Throttler calculated EWMA incorrectly, expected: %f, but calculated: %f", ewma, sp.ewma)
	}
}
