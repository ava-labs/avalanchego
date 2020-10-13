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

// ResourceManager defines the interface for the allocation
// of resources from different pools
type ResourceManager interface {
	TakeMessage(ids.ShortID) bool
	ReturnMessage(ids.ShortID)
	Utilization(ids.ShortID) float64
}

type resourceManager struct {
	log  logging.Logger
	vdrs validators.Set

	maxNonStakerPendingMsgs        uint32
	poolMessages, reservedMessages uint32
	stakerMsgPortion               float64
	msgTracker                     tracker.CountingTracker

	stakerCPUPortion float64
	cpuTracker       tracker.TimeTracker

	clock timer.Clock
}

// NewResourceManager ...
func NewResourceManager(
	vdrs validators.Set,
	log logging.Logger,
	msgTracker tracker.CountingTracker,
	cpuTracker tracker.TimeTracker,
	bufferSize,
	maxNonStakerPendingMsgs uint32,
	stakerMsgPortion,
	stakerCPUPortion float64,
) ResourceManager {
	// Number of messages reserved for stakers vs. non-stakers
	reservedMessages := uint32(stakerMsgPortion * float64(bufferSize))
	poolMessages := bufferSize - reservedMessages

	return &resourceManager{
		vdrs:       vdrs,
		msgTracker: msgTracker,
		cpuTracker: cpuTracker,
		log:        log,

		reservedMessages:        reservedMessages,
		poolMessages:            poolMessages,
		maxNonStakerPendingMsgs: maxNonStakerPendingMsgs,

		stakerCPUPortion: stakerCPUPortion,
	}
}

// TakeMessage attempts to take a message from an available resource
func (rm *resourceManager) TakeMessage(vdr ids.ShortID) bool {
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

// ReturnMessage returns a message to its resource pool
func (rm *resourceManager) ReturnMessage(vdr ids.ShortID) {
	rm.msgTracker.Remove(vdr)
}

// Utilization returns the percentage of expected utilization
// for [vdr] to determine message priority
func (rm *resourceManager) Utilization(vdr ids.ShortID) float64 {
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
