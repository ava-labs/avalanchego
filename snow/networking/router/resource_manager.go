// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package router

import (
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow/networking/tracker"
	"github.com/ava-labs/gecko/snow/validators"
	"github.com/ava-labs/gecko/utils/logging"
	"github.com/ava-labs/gecko/utils/timer"
)

const (
	// DefaultMaxNonStakerPendingMsgs is the default number of messages that can be taken from
	// the shared message
	DefaultMaxNonStakerPendingMsgs uint32 = 20
	// DefaultStakerPortion is the default amount of CPU time and pending messages to allot to stakers
	DefaultStakerPortion float64 = 0.375
)

// ResourceManager defines the interface for the allocation
// of resources from different pools
type ResourceManager interface {
	TakeMessage(ids.ShortID) bool
	ReturnMessage(ids.ShortID)
	Utilization(ids.ShortID) float64
}

type throttler struct {
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
	// Number of messages reserved for Stakers vs. Non-Stakers
	reservedMessages := uint32(stakerMsgPortion * float64(bufferSize))
	poolMessages := bufferSize - reservedMessages

	return &throttler{
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
// It tags the message with the ID of the resource pool it was taken
// from and registers it with the message tracker if successful
// Returns true if it finds a resource for the message.
func (et *throttler) TakeMessage(vdr ids.ShortID) bool {
	// Attempt to take the message from the pool
	outstandingPoolMessages := et.msgTracker.PoolCount()
	totalPeerMessages, peerPoolMessages := et.msgTracker.OutstandingCount(vdr)
	if outstandingPoolMessages < et.poolMessages && peerPoolMessages < et.maxNonStakerPendingMsgs {
		et.msgTracker.AddPool(vdr)
		return true
	}

	// Attempt to take the message from the individual allotment
	weight, isStaker := et.vdrs.GetWeight(vdr)
	if !isStaker {
		et.log.Verbo("Throttling message from non-staker %s. %d/%d.", vdr, peerPoolMessages, et.poolMessages)
		return false
	}
	totalWeight := et.vdrs.Weight()
	stakerPortion := float64(weight) / float64(totalWeight)
	messageAllotment := uint32(stakerPortion * float64(et.reservedMessages))
	messageCount := totalPeerMessages - peerPoolMessages
	// Allow at least one message per staker, even when staking
	// portion rounds message allotment down to 0
	if messageCount <= messageAllotment {
		et.msgTracker.Add(vdr)
		return true
	}

	et.log.Debug("Throttling message from staker %s. %d/%d. %d/%d.", vdr, messageCount, messageAllotment, peerPoolMessages, et.poolMessages)
	return false
}

// ReturnMessage returns a message
func (et *throttler) ReturnMessage(vdr ids.ShortID) {
	et.msgTracker.Remove(vdr)
}

// Utilization returns the percentage of expected utilization
// for [vdr] to determine message priority
func (et *throttler) Utilization(vdr ids.ShortID) float64 {
	currentTime := et.clock.Time()
	vdrUtilization := et.cpuTracker.Utilization(vdr, currentTime)
	numSpenders := et.cpuTracker.Len()
	poolAllotment := (1 - et.stakerCPUPortion) / float64(numSpenders)

	weight, exists := et.vdrs.GetWeight(vdr)
	if !exists {
		return vdrUtilization / poolAllotment
	}

	totalWeight := et.vdrs.Weight()
	stakerPortion := float64(weight) / float64(totalWeight)
	stakerAllotment := stakerPortion*et.stakerCPUPortion + poolAllotment

	return vdrUtilization / stakerAllotment
}
