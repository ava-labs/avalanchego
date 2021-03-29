// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package router

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/networking/tracker"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/timer"
)

const (
	// DefaultMaxNonStakerPendingMsgs is the default number of messages that can be taken from
	// the shared message pool by a single validator
	DefaultMaxNonStakerPendingMsgs uint32 = 20
	// DefaultStakerPortion is the default portion of resources to reserve for stakers
	DefaultStakerPortion float64 = 0.375
)

// MsgManager manages incoming messages. It should be called when an incoming message
// is ready to be processed and when an incoming message is processed. We call the
// message "pending" if it has been received but not processed.
type MsgManager interface {
	// AddPending marks that there is a message from [vdr] ready to be processed.
	// Returns true if the message will eventually be processed.
	AddPending(ids.ShortID) bool
	// Called when we process a message from the given peer
	RemovePending(ids.ShortID)
	Utilization(ids.ShortID) float64
}

// msgManager implements MsgManager
type msgManager struct {
	log                            logging.Logger
	vdrs                           validators.Set
	maxNonStakerPendingMsgs        uint32
	poolMessages, reservedMessages uint32
	msgTracker                     tracker.CountingTracker
	stakerCPUPortion               float64
	cpuTracker                     tracker.TimeTracker
	clock                          timer.Clock
}

// NewMsgManager returns a new MsgManager
// [vdrs] is the network validator set
// [msgTracker] tracks how many messages we've received from each peer
// [cpuTracker] tracks how much time we spend processing messages from each peer
// [maxPendingMsgs] is the maximum number of pending messages (those we have
//   received but not processed.)
// [maxNonStakerPendingMsgs] is the maximum number of pending messages from non-validators.
func NewMsgManager(
	vdrs validators.Set,
	log logging.Logger,
	msgTracker tracker.CountingTracker,
	cpuTracker tracker.TimeTracker,
	maxPendingMsgs,
	maxNonStakerPendingMsgs uint32,
	stakerMsgPortion,
	stakerCPUPortion float64,
) MsgManager {
	// Number of messages reserved for stakers vs. non-stakers
	reservedMessages := uint32(stakerMsgPortion * float64(maxPendingMsgs))
	poolMessages := maxPendingMsgs - reservedMessages

	return &msgManager{
		vdrs:                    vdrs,
		msgTracker:              msgTracker,
		cpuTracker:              cpuTracker,
		log:                     log,
		reservedMessages:        reservedMessages,
		poolMessages:            poolMessages,
		maxNonStakerPendingMsgs: maxNonStakerPendingMsgs,
		stakerCPUPortion:        stakerCPUPortion,
	}
}

// AddPending marks that there is a message from [vdr] ready to be processed.
// Return true if the message was added to the processing list.
func (rm *msgManager) AddPending(vdr ids.ShortID) bool {
	// Attempt to take the message from the pool
	outstandingPoolMessages := rm.msgTracker.PoolCount()
	totalPeerMessages, peerPoolMessages := rm.msgTracker.OutstandingCount(vdr)
	if outstandingPoolMessages < rm.poolMessages && peerPoolMessages < rm.maxNonStakerPendingMsgs {
		rm.msgTracker.AddPool(vdr)
		return true
	}

	// Attempt to take the message from the individual allotment
	weight, isStaker := rm.vdrs.GetWeight(vdr)
	if !isStaker {
		rm.log.Verbo("Throttling message from non-staker %s. %d/%d.", vdr, peerPoolMessages, rm.poolMessages)
		return false
	}
	totalWeight := rm.vdrs.Weight()
	stakerPortion := float64(weight) / float64(totalWeight)
	messageAllotment := uint32(stakerPortion * float64(rm.reservedMessages))
	messageCount := totalPeerMessages - peerPoolMessages
	// Allow at least one message per staker, even when staking
	// portion rounds message allotment down to 0.
	if messageCount <= messageAllotment {
		rm.msgTracker.Add(vdr)
		return true
	}

	rm.log.Debug("Throttling message from staker %s. %d/%d. %d/%d.", vdr, messageCount, messageAllotment, peerPoolMessages, rm.poolMessages)
	return false
}

// RemovePending marks that a message from [vdr] has been processed.
func (rm *msgManager) RemovePending(vdr ids.ShortID) {
	rm.msgTracker.Remove(vdr)
}

// Utilization returns the percentage of expected utilization
// for [vdr] to determine message priority
func (rm *msgManager) Utilization(vdr ids.ShortID) float64 {
	currentTime := rm.clock.Time()
	vdrUtilization := rm.cpuTracker.Utilization(vdr, currentTime)
	numSpenders := rm.cpuTracker.Len()
	poolAllotment := (1 - rm.stakerCPUPortion) / float64(numSpenders)

	weight, exists := rm.vdrs.GetWeight(vdr)
	if !exists {
		return vdrUtilization / poolAllotment
	}

	totalWeight := rm.vdrs.Weight()
	stakerPortion := float64(weight) / float64(totalWeight)
	stakerAllotment := stakerPortion*rm.stakerCPUPortion + poolAllotment

	return vdrUtilization / stakerAllotment
}
