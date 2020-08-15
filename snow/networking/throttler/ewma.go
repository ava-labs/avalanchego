// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package throttler

import (
	"fmt"
	"sync"
	"time"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow/validators"
	"github.com/ava-labs/gecko/utils/logging"
)

const (
	defaultDecayFactor           float64 = 2
	defaultIntervalsUntilPruning uint32  = 60
	defaultMinimumCPUAllotment           = time.Nanosecond
)

type ewmaThrottler struct {
	lock sync.Mutex
	log  logging.Logger

	// Track peers
	spenders       map[[20]byte]*spender
	cumulativeEWMA time.Duration
	vdrs           validators.Set

	// Track CPU utilization
	decayFactor               float64
	stakerCPU, nonReservedCPU time.Duration

	// Track pending messages
	reservedStakerMessages                  uint32
	pendingNonReservedMsgs, nonReservedMsgs uint32
	maxNonStakerPendingMsgs                 uint32

	// Statistics adjusted at every interval
	currentPeriod        uint32
	periodNonReservedCPU time.Duration
}

// NewEWMAThrottler returns a Throttler that uses exponentially weighted moving
// average to estimate CPU utilization.
//
// [maxMessages] is the maximum number of messages allotted to this chain
// [stakerPortion] is the portion of CPU utilization and messages to reserve
// exclusively for stakers. Should be in the range (0, 1]. The rest of the
// messages will be placed in a shared pool for any validator to use. There is
// an additional hard cap placed on the number of pending messages for
// an individual validator:
// (number of reserved staker messages) +
//     [(number of non-reserved messages)/(num validators + 1)]
// [period] is the interval of time to use for the calculation of EWMA
//
// Note: ewmaThrottler uses the period as the total amount of time per interval,
// which is not the limit since it tracks consumption using EWMA. As a result,
// this aggressiveness should counterbalance the fact that no chain's CPU time
// will actually consume nearly 100% of real time.
func NewEWMAThrottler(
	vdrs validators.Set,
	maxMessages uint32,
	stakerMsgPortion,
	stakerCPUPortion float64,
	period time.Duration,
	log logging.Logger,
) Throttler {
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

	// Number of messages reserved for Stakers vs. Non-Stakers
	reservedStakerMessages := uint32(stakerMsgPortion * float64(maxMessages))
	nonReservedMsgs := maxMessages - reservedStakerMessages

	throttler := &ewmaThrottler{
		spenders: make(map[[20]byte]*spender),
		vdrs:     vdrs,
		log:      log,

		decayFactor: defaultDecayFactor,

		stakerCPU:      stakerCPU,
		nonReservedCPU: nonReservedCPU,

		reservedStakerMessages: reservedStakerMessages,
		nonReservedMsgs:        nonReservedMsgs,
	}

	// Add validators to spenders, so that they will be calculated correctly in
	// EndInterval
	for _, vdr := range vdrs.List() {
		throttler.spenders[vdr.ID().Key()] = &spender{}
	}

	// Call EndInterval to calculate initial period statistics and initial
	// spender values for validators
	throttler.EndInterval()
	return throttler
}

func (et *ewmaThrottler) AddMessage(validatorID ids.ShortID) {
	et.lock.Lock()
	defer et.lock.Unlock()

	sp := et.getSpender(validatorID)
	sp.pendingMessages++

	// If the spender has exceeded its message allotment, then count the new
	// message as taken from the shared pool
	if sp.pendingMessages > sp.msgAllotment {
		sp.pendingPoolMessages++
		et.pendingNonReservedMsgs++
	}
}

func (et *ewmaThrottler) RemoveMessage(validatorID ids.ShortID) {
	et.lock.Lock()
	defer et.lock.Unlock()

	sp := et.getSpender(validatorID)
	sp.pendingMessages--

	// If the spender has pending messages taken from the pool,
	// then remove them first
	if sp.pendingPoolMessages > 0 {
		sp.pendingPoolMessages--
		et.pendingNonReservedMsgs--
	}
}

func (et *ewmaThrottler) UtilizeCPU(
	validatorID ids.ShortID,
	consumption time.Duration,
) {
	et.lock.Lock()
	defer et.lock.Unlock()

	sp := et.getSpender(validatorID)
	sp.cpuEWMA += consumption
	sp.lastSpend = et.currentPeriod
	et.cumulativeEWMA += consumption
}

// Returns CPU GetUtilization metric as percentage of expected utilization and
// boolean specifying whether or not the validator has exceeded its message
// allotment.
func (et *ewmaThrottler) GetUtilization(
	validatorID ids.ShortID,
) (float64, bool) {
	et.lock.Lock()
	defer et.lock.Unlock()

	sp := et.getSpender(validatorID)
	if !sp.staking {
		cpuUtilization := float64(et.cumulativeEWMA) / float64(et.periodNonReservedCPU)

		exceedsMessageAllotment := et.pendingNonReservedMsgs > et.nonReservedMsgs || // the shared message pool has been taken
			sp.pendingMessages > sp.maxMessages // exceeds its own individual message cap

		if exceedsMessageAllotment {
			et.log.Verbo("Throttling non-staker %s: %s. Pending pool messages: %d of %d reserved",
				validatorID,
				sp,
				et.pendingNonReservedMsgs,
				et.nonReservedMsgs)
		}
		return cpuUtilization, exceedsMessageAllotment
	}

	// Staker should only be throttled if it has exceeded its message allotment
	// and there are either no messages left in the shared pool or it has
	// exceeded its own maximum message allocation.
	exceedsMessageAllotment := sp.pendingMessages > sp.msgAllotment && // exceeds its own individual message allotment
		(et.pendingNonReservedMsgs > et.nonReservedMsgs || // no unreserved messages
			sp.pendingMessages > sp.maxMessages) // exceeds its own individual message cap

	if exceedsMessageAllotment {
		et.log.Debug("Throttling staker %s: %s. Pending pool messages: %d of %d reserved",
			validatorID,
			sp,
			et.pendingNonReservedMsgs,
			et.nonReservedMsgs)
	}
	return float64(sp.cpuEWMA) / float64(sp.expectedCPU), exceedsMessageAllotment
}

func (et *ewmaThrottler) EndInterval() {
	et.lock.Lock()
	defer et.lock.Unlock()

	et.currentPeriod++

	et.cumulativeEWMA = time.Duration(float64(et.cumulativeEWMA) / et.decayFactor)
	stakingWeight := et.vdrs.Weight()
	numPeers := et.vdrs.Len() + 1
	et.periodNonReservedCPU = et.nonReservedCPU
	et.maxNonStakerPendingMsgs = et.nonReservedMsgs / uint32(numPeers)

	for key, spender := range et.spenders {
		spender.cpuEWMA = time.Duration(float64(spender.cpuEWMA) / et.decayFactor)
		if vdr, exists := et.vdrs.Get(ids.NewShortID(key)); exists {
			stakerPortion := float64(vdr.Weight()) / float64(stakingWeight)

			// Calculate staker allotment here
			spender.staking = true
			spender.msgAllotment = uint32(float64(et.reservedStakerMessages) * stakerPortion)
			spender.maxMessages = uint32(float64(et.reservedStakerMessages)*stakerPortion) + et.maxNonStakerPendingMsgs
			spender.expectedCPU = time.Duration(float64(et.stakerCPU)*stakerPortion) + et.periodNonReservedCPU
			continue
		}

		if spender.lastSpend+defaultIntervalsUntilPruning < et.currentPeriod && spender.pendingMessages == 0 {
			et.log.Debug("Removing validator from throttler after not hearing from it for %d periods",
				et.currentPeriod-spender.lastSpend)
			delete(et.spenders, key)
		}

		// If the validator is not a staker and was not deleted, set its spender
		// attributes
		spender.staking = false
		spender.msgAllotment = 0
		spender.maxMessages = et.maxNonStakerPendingMsgs
		spender.expectedCPU = et.periodNonReservedCPU
	}
}

// getSpender returns the [spender] corresponding to [validatorID]
func (et *ewmaThrottler) getSpender(validatorID ids.ShortID) *spender {
	validatorKey := validatorID.Key()
	if sp, exists := et.spenders[validatorKey]; exists {
		return sp
	}

	// If this validator did not exist in spenders, create it and return
	sp := &spender{
		maxMessages: et.maxNonStakerPendingMsgs,
		expectedCPU: et.periodNonReservedCPU,
	}
	et.spenders[validatorKey] = sp
	return sp
}

type spender struct {
	// Last period that this spender utilized the CPU
	lastSpend uint32

	// Number of pending messages this spender has taken from the pool
	pendingPoolMessages uint32

	// Number of messages this spender currently has pending
	pendingMessages uint32

	// Number of messages allocated to this spender as a staker
	msgAllotment uint32

	// Max number of messages this spender can use even if the shared pool is
	// non-empty
	maxMessages uint32

	// EWMA of this spender's CPU utilization
	cpuEWMA time.Duration

	// The expected CPU utilization of this peer
	expectedCPU time.Duration

	// Flag to indicate if this spender is a staker
	staking bool
}

func (sp *spender) String() string {
	return fmt.Sprintf("Spender(Messages: (%d+%d)/(%d+%d), CPU: %s/%s)",
		sp.pendingPoolMessages,
		sp.pendingMessages-sp.pendingPoolMessages,
		sp.msgAllotment,
		sp.maxMessages-sp.msgAllotment,
		sp.cpuEWMA,
		sp.expectedCPU,
	)
}
