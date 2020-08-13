package router

import (
	"sync"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow/validators"
	"github.com/ava-labs/gecko/utils/logging"
)

const (
	defaultDecayFactor           float64 = 2
	defaultIntervalsUntilPruning uint32  = 60
)

type ewmaThrottler struct {
	lock sync.Mutex
	log  logging.Logger

	// Track peers
	spenders  map[[20]byte]*spender
	nonStaker *spender
	vdrs      validators.Set

	// Track CPU utilization
	maxEWMA, period, decayFactor                           float64
	stakerPortion, stakerCPU, nonStakerCPU                 float64
	maxMessages, reservedStakerMessages, nonStakerMessages uint32
	stakingWeight                                          uint64

	// Statistics adjusted at every interval
	currentPeriod            uint32
	periodNonStakerAllotment float64
	periodNonStakerMessages  uint32
}

// NewEWMAThrottler returns a Throttler that uses exponentially weighted moving
// average to estimate CPU utilization.
//
// [maxMessages] is the maximum number of messages allotted to this chain
// [stakerPortion] is the portion of CPU utilization and messages to reserve
// exclusively for stakers should be in the range (0, 1]
// [period] is the interval of time to use for the caclulation of EWMA
//
// Note: ewmaThrottler uses the period as the total amount of time per
// interval, which is not the limit since it tracks consumption using EWMA.
// As a result, this aggressiveness should counterbalance the fact that no
// chain's CPU time will actually consume nearly 100% of real time.
func NewEWMAThrottler(vdrs validators.Set, maxMessages uint32, stakerPortion, period float64, log logging.Logger) Throttler {
	// Amount of CPU time reserved for processing messages from stakers
	stakerCPU := period * stakerPortion
	nonStakerCPU := period - stakerCPU

	// Number of messages reserved for Stakers vs. Non-Stakers
	reservedStakerMessages := uint32(stakerPortion * float64(maxMessages))
	nonStakerMessages := maxMessages - reservedStakerMessages

	throttler := &ewmaThrottler{
		spenders:  make(map[[20]byte]*spender),
		nonStaker: &spender{},
		vdrs:      vdrs,
		log:       log,

		maxEWMA:     period * defaultDecayFactor,
		period:      period,
		decayFactor: defaultDecayFactor,

		stakerPortion: stakerPortion,
		stakerCPU:     stakerCPU,
		nonStakerCPU:  nonStakerCPU,

		maxMessages:            maxMessages,
		reservedStakerMessages: reservedStakerMessages,
		nonStakerMessages:      nonStakerMessages,
	}

	// Call EndInterval to calculate initial period statistics
	throttler.EndInterval()
	return throttler
}

// AddMessage...
func (et *ewmaThrottler) AddMessage(validatorID ids.ShortID) {
	et.lock.Lock()
	defer et.lock.Unlock()

	sp, staker := et.getSpender(validatorID)

	if !staker {
		et.nonStaker.pendingMessages++
	}
	sp.pendingMessages++

}

// RemoveMessage...
func (et *ewmaThrottler) RemoveMessage(validatorID ids.ShortID) {
	et.lock.Lock()
	defer et.lock.Unlock()

	sp, staking := et.getSpender(validatorID)
	sp.pendingMessages--

	if !staking {
		et.nonStaker.pendingMessages--
	}
}

// UtilizeCPU...
func (et *ewmaThrottler) UtilizeCPU(validatorID ids.ShortID, consumption float64) {
	et.lock.Lock()
	defer et.lock.Unlock()

	sp, staking := et.getSpender(validatorID)
	sp.ewma += consumption
	sp.lastSpend = et.currentPeriod

	if !staking {
		et.nonStaker.ewma += consumption
		et.nonStaker.lastSpend = et.currentPeriod
	}
}

// GetUtilization...
// Returns CPU GetUtilization metric as percentage of expected utilization and
// boolean specifying whether or not the validator has exceeded its message
// allotment.
func (et *ewmaThrottler) GetUtilization(validatorID ids.ShortID) (float64, bool) {
	et.lock.Lock()
	defer et.lock.Unlock()

	sp, _ := et.getSpender(validatorID)
	vdr, exists := et.vdrs.Get(validatorID)

	if !exists {
		cpuUtilization := et.nonStaker.ewma / et.periodNonStakerAllotment
		exceedsMessageAllotment := et.nonStaker.pendingMessages > et.periodNonStakerMessages
		if exceedsMessageAllotment {
			et.log.Verbo("Throttling message from non-staker %s with %d pending messages, cumulative non-staker has %d pending and CPU Utilization: %f",
				validatorID, sp.pendingMessages, et.nonStaker.pendingMessages, et.nonStaker.ewma)
		}
		return cpuUtilization, exceedsMessageAllotment
	}

	stakingFactor := float64(vdr.Weight()) / float64(et.stakingWeight)
	stakerAllotment := et.stakerCPU * stakingFactor
	stakerMessages := uint32(float64(et.reservedStakerMessages) * stakingFactor)
	cpuUtilization := sp.ewma / (et.periodNonStakerAllotment + stakerAllotment)
	exceedsMessageAllotment := sp.pendingMessages > stakerMessages+et.periodNonStakerMessages

	if exceedsMessageAllotment {
		et.log.Debug("Staker %s has exceeded its message allotment with %d messages at CPU Utilization: %f", validatorID, sp.pendingMessages, cpuUtilization)
	}
	return cpuUtilization, exceedsMessageAllotment
}

// EndInterval...
func (et *ewmaThrottler) EndInterval() {
	et.lock.Lock()
	defer et.lock.Unlock()

	et.nonStaker.ewma /= et.decayFactor

	for key, spender := range et.spenders {
		spender.ewma /= et.decayFactor
		if spender.lastSpend+defaultIntervalsUntilPruning < et.currentPeriod && spender.pendingMessages == 0 {
			et.log.Debug("Removing validator from throttler after not hearing from it for %d periods", et.currentPeriod-spender.lastSpend)
			delete(et.spenders, key)
		}
	}
	et.stakingWeight = et.vdrs.Weight()

	// Assume all non-validators are a single peer to defend
	// against Sybil attack
	numPeers := et.vdrs.Len() + 1
	et.periodNonStakerAllotment = et.nonStakerCPU / float64(numPeers)
	et.periodNonStakerMessages = et.nonStakerMessages / uint32(numPeers)

	et.currentPeriod++
}

// getSpender returns the [spender] corresponding to [validatorID] and whether or not the validator is currently staking
func (et *ewmaThrottler) getSpender(validatorID ids.ShortID) (*spender, bool) {
	validatorKey := validatorID.Key()
	sp, exists := et.spenders[validatorKey]
	staking := et.vdrs.Contains(validatorID)

	if !exists {
		// If this validator did not exist in spenders, create it and return
		sp = &spender{
			currentPeriod: et.currentPeriod,
			staking:       staking,
		}
		et.spenders[validatorKey] = sp
		return sp, staking
	}

	// If this validator has not changed whether or not it is staking
	// return as is
	if sp.staking == staking {
		return sp, staking
	}

	// If this spender was previously staking and stopped, add its
	// pending messages to nonStaker's.
	if sp.staking {
		sp.staking = false
		et.nonStaker.pendingMessages += sp.pendingMessages

		return sp, staking
	}

	// Otherwise, it has started staking, so remove its pending messages
	// from nonStaker's
	sp.staking = true
	et.nonStaker.pendingMessages -= sp.pendingMessages

	return sp, staking
}

type spender struct {
	currentPeriod, lastSpend, pendingMessages uint32
	ewma                                      float64
	staking                                   bool
}
