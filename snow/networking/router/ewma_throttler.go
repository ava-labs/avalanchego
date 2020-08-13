package router

import (
	"fmt"
	"sync"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow/validators"
	"github.com/ava-labs/gecko/utils/logging"
)

const (
	defaultDecayFactor               float64 = 2
	defaultIntervalsUntilPruning     uint32  = 60
	defaultMinimumNonStakerAllotment float64 = 0.0001
)

type ewmaThrottler struct {
	lock sync.Mutex
	log  logging.Logger

	// Track peers
	spenders       map[[20]byte]*spender
	cumulativeEWMA float64
	vdrs           validators.Set

	// Track CPU utilization
	maxEWMA, period, decayFactor       float64
	stakerMsgPortion, stakerCPUPortion float64
	stakerCPU, nonReservedCPU          float64

	// Track pending messages
	maxMessages, reservedStakerMessages     uint32
	pendingNonReservedMsgs, nonReservedMsgs uint32
	maxNonStakerPendingMsgs                 uint32

	// Statistics adjusted at every interval
	currentPeriod           uint32
	periodNonReservedCPU    float64
	periodNonStakerMessages uint32
}

// NewEWMAThrottler returns a Throttler that uses exponentially weighted moving
// average to estimate CPU utilization.
//
// [maxMessages] is the maximum number of messages allotted to this chain
// [stakerPortion] is the portion of CPU utilization and messages to reserve
// exclusively for stakers should be in the range (0, 1]
// the rest of the messages will be placed in a shared pool for any validator to
// use. There is an additional hard cap placed on the number of pending messages for
// an individual valudator:
// (number of reserved staker messages) + [(number of non-reserved messages)/(num validators + 1)]
// [period] is the interval of time to use for the caclulation of EWMA
//
// Note: ewmaThrottler uses the period as the total amount of time per
// interval, which is not the limit since it tracks consumption using EWMA.
// As a result, this aggressiveness should counterbalance the fact that no
// chain's CPU time will actually consume nearly 100% of real time.
func NewEWMAThrottler(vdrs validators.Set, maxMessages uint32, stakerMsgPortion, stakerCPUPortion, period float64, log logging.Logger) Throttler {
	// Amount of CPU time reserved for processing messages from stakers
	stakerCPU := period * stakerCPUPortion
	nonReservedCPU := period - stakerCPU

	// Number of messages reserved for Stakers vs. Non-Stakers
	reservedStakerMessages := uint32(stakerMsgPortion * float64(maxMessages))
	nonReservedMsgs := maxMessages - reservedStakerMessages

	throttler := &ewmaThrottler{
		spenders: make(map[[20]byte]*spender),
		vdrs:     vdrs,
		log:      log,

		maxEWMA:     period * defaultDecayFactor,
		period:      period,
		decayFactor: defaultDecayFactor,

		stakerCPUPortion: stakerCPUPortion,
		stakerMsgPortion: stakerMsgPortion,
		stakerCPU:        stakerCPU,
		nonReservedCPU:   nonReservedCPU,

		maxMessages:            maxMessages,
		reservedStakerMessages: reservedStakerMessages,
		nonReservedMsgs:        nonReservedMsgs,
	}

	// Add validators to spenders, so that they will be calculated correctly in EndInterval
	for _, vdr := range vdrs.List() {
		throttler.spenders[vdr.ID().Key()] = &spender{}
	}

	// Call EndInterval to calculate initial period statistics and initial spender values for validators
	throttler.EndInterval()
	return throttler
}

// AddMessage...
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

// RemoveMessage...
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

// UtilizeCPU...
func (et *ewmaThrottler) UtilizeCPU(validatorID ids.ShortID, consumption float64) {
	et.lock.Lock()
	defer et.lock.Unlock()

	sp := et.getSpender(validatorID)
	sp.ewma += consumption
	sp.lastSpend = et.currentPeriod
	et.cumulativeEWMA += consumption
}

// GetUtilization...
// Returns CPU GetUtilization metric as percentage of expected utilization and
// boolean specifying whether or not the validator has exceeded its message
// allotment.
func (et *ewmaThrottler) GetUtilization(validatorID ids.ShortID) (float64, bool) {
	et.lock.Lock()
	defer et.lock.Unlock()

	sp := et.getSpender(validatorID)
	if !sp.staking {
		cpuUtilization := et.cumulativeEWMA / et.periodNonReservedCPU
		// A non-staker will be throttled if the shared message pool has been taken or it has exceeded its own individual message cap
		exceedsMessageAllotment := et.pendingNonReservedMsgs > et.nonReservedMsgs || sp.pendingMessages > sp.maxMessages
		if exceedsMessageAllotment {
			et.log.Verbo("Throttling non-staker %s: %s. Pending pool messages: %d of %d reserved", validatorID, sp, et.pendingNonReservedMsgs, et.nonReservedMsgs)
		}
		return cpuUtilization, exceedsMessageAllotment
	}

	// Staker should only be throttled if it has exceede its message allotment and there are either no messages left in the shared pool
	// or it has exceeded its own maximum message allocation.
	exceedsMessageAllotment := sp.pendingMessages > sp.msgAllotment && (et.pendingNonReservedMsgs > et.nonReservedMsgs || sp.pendingMessages > sp.maxMessages)
	if exceedsMessageAllotment {
		et.log.Debug("Throttling staker %s: %s. Pending pool messages: %d of %d reserved", validatorID, sp.pendingMessages, et.pendingNonReservedMsgs, et.nonReservedMsgs)
	}
	return (sp.ewma / sp.expectedEWMA), exceedsMessageAllotment
}

// EndInterval...
func (et *ewmaThrottler) EndInterval() {
	et.lock.Lock()
	defer et.lock.Unlock()

	et.currentPeriod++

	et.cumulativeEWMA /= et.decayFactor
	stakingWeight := et.vdrs.Weight()
	numPeers := et.vdrs.Len() + 1
	et.periodNonReservedCPU = et.nonReservedCPU
	if et.periodNonReservedCPU == 0 {
		et.periodNonReservedCPU = defaultMinimumNonStakerAllotment
	}
	et.maxNonStakerPendingMsgs = et.nonReservedMsgs / uint32(numPeers)

	for key, spender := range et.spenders {
		spender.ewma /= et.decayFactor
		if vdr, exists := et.vdrs.Get(ids.NewShortID(key)); exists {
			// Calculate staker allotment here
			spender.staking = true
			stakerPortion := float64(vdr.Weight()) / float64(stakingWeight)
			spender.msgAllotment = uint32(float64(et.reservedStakerMessages) * stakerPortion)
			spender.maxMessages = uint32(float64(et.reservedStakerMessages)*stakerPortion) + et.maxNonStakerPendingMsgs
			spender.expectedEWMA = et.stakerCPU*stakerPortion + et.periodNonReservedCPU
			if spender.expectedEWMA == 0 {
				spender.expectedEWMA = defaultMinimumNonStakerAllotment
			}
			continue
		}

		if spender.lastSpend+defaultIntervalsUntilPruning < et.currentPeriod && spender.pendingMessages == 0 {
			et.log.Debug("Removing validator from throttler after not hearing from it for %d periods", et.currentPeriod-spender.lastSpend)
			delete(et.spenders, key)
		}

		// If the validator is not a staker and was not deleted, set its spender attributes
		spender.staking = false
		spender.msgAllotment = 0
		spender.maxMessages = et.maxNonStakerPendingMsgs
		spender.expectedEWMA = et.periodNonReservedCPU
	}
}

// getSpender returns the [spender] corresponding to [validatorID] and whether or not the validator is currently staking
func (et *ewmaThrottler) getSpender(validatorID ids.ShortID) *spender {
	validatorKey := validatorID.Key()
	sp, exists := et.spenders[validatorKey]

	if !exists {
		// If this validator did not exist in spenders, create it and return
		sp = &spender{
			maxMessages:  et.maxNonStakerPendingMsgs,
			expectedEWMA: et.periodNonReservedCPU,
		}
		et.spenders[validatorKey] = sp
		return sp
	}

	return sp
}

type spender struct {
	// Last period that this spender utilized the CPU
	lastSpend uint32

	pendingMessages uint32
	// Number of messages allocated to this spender as a staker
	msgAllotment uint32
	// Max number of messages this spender can use even if the shared pool is non-empty
	maxMessages uint32
	// Number of pending messages this spender has taken from the pool
	pendingPoolMessages uint32

	// EWMA of this spender's CPU utilization and its expected value calculated at each interval
	ewma, expectedEWMA float64
	staking            bool
}

func (sp *spender) String() string {
	return fmt.Sprintf("Spender(%d, %d, %d, %d, %f)", sp.pendingMessages, sp.msgAllotment, sp.maxMessages, sp.pendingPoolMessages, sp.ewma/sp.expectedEWMA)
}
