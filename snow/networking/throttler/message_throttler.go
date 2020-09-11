// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package throttler

import (
	"fmt"
	"sync"

	"github.com/ava-labs/avalanche-go/ids"
	"github.com/ava-labs/avalanche-go/snow/validators"
	"github.com/ava-labs/avalanche-go/utils/logging"
)

const (
	defaultIntervalsUntilPruning uint32 = 60
)

type messageThrottler struct {
	lock sync.Mutex
	log  logging.Logger

	// Track peers
	msgSpenders map[[20]byte]*msgSpender
	vdrs        validators.Set

	// Track pending messages
	reservedStakerMessages uint32 // Number of messages reserved for stakers
	nonReservedMsgs        uint32 // Number of non-reserved messages left to a shared message pool
	pendingNonReservedMsgs uint32 // Number of pending messages taken from the shared message pool

	// Cap on number of pending messages allowed to a non-staker
	maxNonStakerPendingMsgs uint32

	// Statistics adjusted at every interval
	currentPeriod uint32
}

// NewMessageThrottler returns a MessageThrottler that throttles peers
// when they have too many pending messages outstanding.
//
// [maxMessages] is the maximum number of messages allotted to this chain
// [stakerMsgPortion] is the portion of messages to reserve exclusively for stakers
// should be in the range (0, 1]
func NewMessageThrottler(
	vdrs validators.Set,
	maxMessages,
	maxNonStakerPendingMsgs uint32,
	stakerMsgPortion float64,
	log logging.Logger,
) CountingThrottler {
	// Number of messages reserved for Stakers vs. Non-Stakers
	reservedStakerMessages := uint32(stakerMsgPortion * float64(maxMessages))
	nonReservedMsgs := maxMessages - reservedStakerMessages

	throttler := &messageThrottler{
		msgSpenders: make(map[[20]byte]*msgSpender),
		vdrs:        vdrs,
		log:         log,

		reservedStakerMessages:  reservedStakerMessages,
		nonReservedMsgs:         nonReservedMsgs,
		maxNonStakerPendingMsgs: maxNonStakerPendingMsgs,
	}

	// Add validators to msgSpenders, so that they will be calculated correctly in
	// EndInterval
	for _, vdr := range vdrs.List() {
		throttler.msgSpenders[vdr.ID().Key()] = &msgSpender{}
	}

	// Call EndInterval to calculate initial period statistics and initial
	// msgSpender values for validators
	throttler.EndInterval()
	return throttler
}

func (et *messageThrottler) Add(validatorID ids.ShortID) {
	et.lock.Lock()
	defer et.lock.Unlock()

	sp := et.getSpender(validatorID)
	sp.pendingMessages++
	sp.lastSpend = et.currentPeriod

	// If the msgSpender has exceeded its message allotment, then the additional
	// message is taken from the pool
	if sp.pendingMessages > sp.msgAllotment {
		sp.pendingPoolMessages++
		et.pendingNonReservedMsgs++
	}
}

func (et *messageThrottler) Remove(validatorID ids.ShortID) {
	et.lock.Lock()
	defer et.lock.Unlock()

	sp := et.getSpender(validatorID)
	sp.pendingMessages--

	// If the msgSpender has pending messages taken from the pool,
	// they are the first messages to be removed.
	if sp.pendingPoolMessages > 0 {
		sp.pendingPoolMessages--
		et.pendingNonReservedMsgs--
	}
}

// Throttle returns true if messages from [validatorID] should be throttled due
// to having too many pending messages
func (et *messageThrottler) Throttle(
	validatorID ids.ShortID,
) bool {
	et.lock.Lock()
	defer et.lock.Unlock()

	sp := et.getSpender(validatorID)
	if !sp.staking {
		exceedsMessageAllotment := et.pendingNonReservedMsgs > et.nonReservedMsgs || // the shared message pool has been taken
			(sp.pendingMessages > sp.maxMessages) // Spender has exceeded its individual cap

		if exceedsMessageAllotment {
			et.log.Verbo("Throttling non-staker %s: %s. Pending pool messages: %d/%d.",
				validatorID,
				sp,
				et.pendingNonReservedMsgs,
				et.nonReservedMsgs)
		}
		return exceedsMessageAllotment
	}

	exceedsMessageAllotment := sp.pendingMessages > sp.msgAllotment && // Throttle if the staker has exceeded its allotment
		(et.pendingNonReservedMsgs > et.nonReservedMsgs || // And either the shared message pool is empty
			sp.pendingMessages > sp.maxMessages) // or this staker has exceeded its individual cap

	if exceedsMessageAllotment {
		et.log.Debug("Throttling staker %s: %s. Pending pool messages: %d/%d.",
			validatorID,
			sp,
			et.pendingNonReservedMsgs,
			et.nonReservedMsgs)
	}
	return exceedsMessageAllotment
}

func (et *messageThrottler) EndInterval() {
	et.lock.Lock()
	defer et.lock.Unlock()

	et.currentPeriod++
	stakingWeight := et.vdrs.Weight()

	for key, msgSpender := range et.msgSpenders {
		if weight, exists := et.vdrs.GetWeight(ids.NewShortID(key)); exists {
			stakerPortion := float64(weight) / float64(stakingWeight)

			// Calculate staker allotment here
			msgSpender.staking = true
			msgSpender.msgAllotment = uint32(float64(et.reservedStakerMessages) * stakerPortion)
			msgSpender.maxMessages = msgSpender.msgAllotment + et.maxNonStakerPendingMsgs
			continue
		}

		if msgSpender.lastSpend+defaultIntervalsUntilPruning < et.currentPeriod && msgSpender.pendingMessages == 0 {
			et.log.Debug("Removing validator from throttler after not hearing from it for %d periods",
				et.currentPeriod-msgSpender.lastSpend)
			delete(et.msgSpenders, key)
		}

		// If the validator is not a staker and was not deleted, set its msgSpender
		// attributes
		msgSpender.staking = false
		msgSpender.msgAllotment = 0
		msgSpender.maxMessages = et.maxNonStakerPendingMsgs
	}
}

// getSpender returns the [msgSpender] corresponding to [validatorID]
func (et *messageThrottler) getSpender(validatorID ids.ShortID) *msgSpender {
	validatorKey := validatorID.Key()
	if sp, exists := et.msgSpenders[validatorKey]; exists {
		return sp
	}

	// If this validator did not exist in msgSpenders, create it and return
	sp := &msgSpender{
		maxMessages: et.maxNonStakerPendingMsgs,
	}
	et.msgSpenders[validatorKey] = sp
	return sp
}

type msgSpender struct {
	// Last period that this msgSpender utilized the CPU
	lastSpend uint32

	// Number of pending messages this msgSpender has taken from the pool
	pendingPoolMessages uint32

	// Number of messages this msgSpender currently has pending
	pendingMessages uint32

	// Number of messages allocated to this msgSpender as a staker
	msgAllotment uint32

	// Max number of messages this msgSpender can use even if the shared pool is
	// non-empty
	maxMessages uint32

	// Flag to indicate if this msgSpender is a staker
	staking bool
}

func (sp *msgSpender) String() string {
	return fmt.Sprintf("MsgSpender(Messages: (%d+%d)/(%d+%d))",
		sp.pendingPoolMessages,
		sp.pendingMessages-sp.pendingPoolMessages,
		sp.msgAllotment,
		sp.maxMessages-sp.msgAllotment,
	)
}
