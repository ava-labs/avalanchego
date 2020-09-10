// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package throttler

import (
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/ava-labs/avalanche-go/ids"
	"github.com/ava-labs/avalanche-go/snow/validators"
	"github.com/ava-labs/avalanche-go/utils/logging"
)

const (
	defaultDecayFactor         float64 = 2
	defaultMinimumCPUAllotment         = time.Nanosecond
)

type ewmaCPUTracker struct {
	lock sync.Mutex
	log  logging.Logger

	// Track peers
	cpuSpenders    map[[20]byte]*cpuSpender
	cumulativeEWMA time.Duration
	vdrs           validators.Set

	// Track CPU utilization
	decayFactor    float64       // Factor used to discount the EWMA at every period
	stakerCPU      time.Duration // Amount of CPU time reserved for stakers
	nonReservedCPU time.Duration // Amount of CPU time that is not reserved for stakers
}

// NewEWMATracker returns a CPUTracker that uses exponentially weighted moving
// average to estimate CPU utilization.
//
// [stakerCPUPortion] is the portion of CPU utilization to reserve for stakers (range (0, 1])
// [period] is the interval of time to use for the calculation of EWMA
//
// Note: ewmaCPUTracker uses the period as the total amount of time per interval,
// which is not the limit since it tracks consumption using EWMA.
func NewEWMATracker(
	vdrs validators.Set,
	stakerCPUPortion float64,
	period time.Duration,
	log logging.Logger,
) CPUTracker {
	// Amount of CPU time reserved for processing messages from stakers
	stakerCPU := time.Duration(float64(period) * stakerCPUPortion)
	if stakerCPU < defaultMinimumCPUAllotment {
		// defaultMinimumCPUAllotment must be > 0 to avoid divide by 0 errors
		stakerCPU = defaultMinimumCPUAllotment
	}

	// Amount of CPU time unreserved
	nonReservedCPU := period - stakerCPU
	if nonReservedCPU < defaultMinimumCPUAllotment {
		// defaultMinimumCPUAllotment must be > 0 to avoid divide by 0 errors
		nonReservedCPU = defaultMinimumCPUAllotment
	}

	throttler := &ewmaCPUTracker{
		cpuSpenders: make(map[[20]byte]*cpuSpender),
		vdrs:        vdrs,
		log:         log,

		decayFactor: defaultDecayFactor,

		stakerCPU:      stakerCPU,
		nonReservedCPU: nonReservedCPU,
	}

	// Add validators to cpuSpenders, so that they will be calculated correctly in
	// EndInterval
	for _, vdr := range vdrs.List() {
		throttler.cpuSpenders[vdr.ID().Key()] = &cpuSpender{}
	}

	// Call EndInterval to calculate initial period statistics and initial
	// cpuSpender values for validators
	throttler.EndInterval()
	return throttler
}

func (et *ewmaCPUTracker) UtilizeCPU(
	validatorID ids.ShortID,
	consumption time.Duration,
) {
	et.lock.Lock()
	defer et.lock.Unlock()

	sp := et.getSpender(validatorID)
	sp.cpuEWMA += consumption
	et.cumulativeEWMA += consumption
}

// GetUtilization returns a percentage of expected CPU utilization of the peer
// corresponding to [validatorID]
func (et *ewmaCPUTracker) GetUtilization(validatorID ids.ShortID) float64 {
	et.lock.Lock()
	defer et.lock.Unlock()

	sharedUtilization := float64(et.cumulativeEWMA) / float64(et.nonReservedCPU)
	sp := et.getSpender(validatorID)
	if !sp.staking {
		return sharedUtilization
	}
	return math.Min(float64(sp.cpuEWMA)/float64(sp.expectedCPU), sharedUtilization)
}

// EndInterval registers the end of a given CPU interval by discounting
// all cpuSpenders' cpuEWMA and removing outstanding spenders that have sufficiently
// low cpuEWMA stats
func (et *ewmaCPUTracker) EndInterval() {
	et.lock.Lock()
	defer et.lock.Unlock()

	et.cumulativeEWMA = time.Duration(float64(et.cumulativeEWMA) / et.decayFactor)
	stakingWeight := et.vdrs.Weight()

	removed := 0
	for key, cpuSpender := range et.cpuSpenders {
		cpuSpender.cpuEWMA = time.Duration(float64(cpuSpender.cpuEWMA) / et.decayFactor)
		if weight, ok := et.vdrs.GetWeight(ids.NewShortID(key)); ok {
			stakerPortion := float64(weight) / float64(stakingWeight)

			// Calculate staker allotment here
			cpuSpender.staking = true
			cpuSpender.expectedCPU = time.Duration(float64(et.stakerCPU)*stakerPortion) + defaultMinimumCPUAllotment
			continue
		}

		if cpuSpender.cpuEWMA == 0 {
			removed++
			delete(et.cpuSpenders, key)
		}

		// If the validator is not a staker and was not deleted, set its cpuSpender
		// attributes
		cpuSpender.staking = false
		cpuSpender.expectedCPU = defaultMinimumCPUAllotment
	}
	et.log.Verbo("Removed %d validators from CPU Tracker.", removed)
}

// getSpender returns the [cpuSpender] corresponding to [validatorID]
func (et *ewmaCPUTracker) getSpender(validatorID ids.ShortID) *cpuSpender {
	validatorKey := validatorID.Key()
	if sp, exists := et.cpuSpenders[validatorKey]; exists {
		return sp
	}

	// If this validator did not exist in cpuSpenders, create it and return
	sp := &cpuSpender{
		expectedCPU: defaultMinimumCPUAllotment,
	}
	et.cpuSpenders[validatorKey] = sp
	return sp
}

type cpuSpender struct {
	// EWMA of this cpuSpender's CPU utilization
	cpuEWMA time.Duration

	// The expected CPU utilization of this peer
	expectedCPU time.Duration

	// Flag to indicate if this cpuSpender is a staker
	staking bool
}

func (sp *cpuSpender) String() string {
	return fmt.Sprintf("CPUTracker(CPU: %s/%s)",
		sp.cpuEWMA,
		sp.expectedCPU,
	)
}
