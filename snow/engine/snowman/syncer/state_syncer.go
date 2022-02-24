// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package syncer

import (
	"sort"
	"time"

	stdmath "math"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/version"
)

const (
	// maxOutstandingStateSyncRequests is the maximum number of
	// messages sent but not responded to/failed for each relevant message type
	maxOutstandingStateSyncRequests = 50
)

var _ common.StateSyncer = &stateSyncer{}

type stateSyncer struct {
	Config

	// list of NoOpsHandler for messages dropped by state syncer
	common.AcceptedFrontierHandler
	common.AcceptedHandler
	common.AncestorsHandler
	common.PutHandler
	common.QueryHandler
	common.ChitsHandler
	common.AppHandler

	started bool
	// True if restart has been called at least once
	restarted bool
	// Tracks the last requestID that was used in a request
	requestID uint32

	// State Sync specific fields
	stateSyncVM        block.StateSyncableVM
	onDoneStateSyncing func(lastReqID uint32) error
	lastSummaryBlkID   ids.ID

	frontierTracker
	voteTracker

	// // IDs of validators we should request filtering the accepted state summaries from
	// IDs of validators that failed to respond with their filtered accepted state summaries
	failedAcceptedStateSummaries ids.ShortSet

	weightedSummaries map[string]weightedSummary // hash --> (summary, weight)

	// number of times the state sync has been attempted
	attempts int
}

func New(
	cfg Config,
	onDoneStateSyncing func(lastReqID uint32) error,
) common.StateSyncer {
	ssVM, _ := cfg.VM.(block.StateSyncableVM)
	return &stateSyncer{
		Config:                  cfg,
		AcceptedFrontierHandler: common.NewNoOpAcceptedFrontierHandler(cfg.Ctx.Log),
		AcceptedHandler:         common.NewNoOpAcceptedHandler(cfg.Ctx.Log),
		AncestorsHandler:        common.NewNoOpAncestorsHandler(cfg.Ctx.Log),
		PutHandler:              common.NewNoOpPutHandler(cfg.Ctx.Log),
		QueryHandler:            common.NewNoOpQueryHandler(cfg.Ctx.Log),
		ChitsHandler:            common.NewNoOpChitsHandler(cfg.Ctx.Log),
		AppHandler:              common.NewNoOpAppHandler(cfg.Ctx.Log),
		stateSyncVM:             ssVM,
		onDoneStateSyncing:      onDoneStateSyncing,
	}
}

// StateSyncHandler interface implementation
func (ss *stateSyncer) StateSummaryFrontier(validatorID ids.ShortID, requestID uint32, summary []byte) error {
	// ignores any late responses
	if requestID != ss.requestID {
		ss.Ctx.Log.Debug("Received an Out-of-Sync StateSummaryFrontier - validator: %v - expectedRequestID: %v, requestID: %v",
			validatorID, ss.requestID, requestID)
		return nil
	}

	if !ss.hasSeederBeenContacted(validatorID) {
		ss.Ctx.Log.Debug("Received a StateSummaryFrontier message from %s unexpectedly", validatorID)
		return nil
	}

	// Mark that we received a response from [validatorID]
	ss.markSeederResponded(validatorID)

	// retrieve key for summary and register frontier
	_, hash, err := ss.stateSyncVM.StateSyncGetKeyHash(common.Summary{Content: summary})
	if err != nil {
		ss.Ctx.Log.Debug("Could not retrieve key from summary %s: %v", summary, err)
		return nil
	}

	if _, exists := ss.weightedSummaries[string(hash.Content)]; !exists {
		ss.weightedSummaries[string(hash.Content)] = weightedSummary{
			Summary: common.Summary{
				Content: summary,
			},
		}
	}

	ss.sendGetStateSummaryFrontiers()

	// still waiting on requests
	if ss.anyPendingSeederResponse() {
		return nil
	}

	// We've received the accepted frontier from every state syncer
	// Ask each state syncer to filter the list of containers that we were
	// told are on the accepted frontier such that the list only contains containers
	// they think are accepted

	// Create a newAlpha taking using the sampled beacon
	// Keep the proportion of b.Alpha in the newAlpha
	// newAlpha := totalSampledWeight * b.Alpha / totalWeight
	newAlpha := float64(ss.sampledSeedersWeight()*ss.Alpha) / float64(ss.StateSyncBeacons.Weight())

	failedBeaconWeight, err := ss.StateSyncBeacons.SubsetWeight(ss.failedSeeders)
	if err != nil {
		return err
	}

	// fail the fast sync if the weight is not enough to fast sync
	if float64(ss.sampledSeedersWeight())-newAlpha < float64(failedBeaconWeight) {
		if ss.Config.RetrySyncing {
			ss.Ctx.Log.Debug("Not enough frontiers received, restarting state sync... - Beacons: %d - Failed Bootstrappers: %d "+
				"- state sync attempt: %d", ss.StateSyncBeacons.Len(), ss.failedSeeders.Len(), ss.attempts)
			return ss.restart(false)
		}

		ss.Ctx.Log.Debug("Didn't receive enough frontiers - failed validators: %d, "+
			"state sync attempt: %d", ss.failedSeeders.Len(), ss.attempts)
	}

	ss.requestID++
	return ss.sendGetAccepted()
}

// StateSyncHandler interface implementation
func (ss *stateSyncer) GetStateSummaryFrontierFailed(validatorID ids.ShortID, requestID uint32) error {
	// ignores any late responses
	if requestID != ss.requestID {
		ss.Ctx.Log.Debug("Received an Out-of-Sync GetStateSummaryFrontierFailed - validator: %v - expectedRequestID: %v, requestID: %v",
			validatorID, ss.requestID, requestID)
		return nil
	}

	// If we can't get a response from [validatorID], act as though they said their state summary frontier is empty
	// and we add the validator to the failed list
	ss.markSeederFailed(validatorID)
	return ss.StateSummaryFrontier(validatorID, requestID, []byte{})
}

// StateSyncHandler interface implementation
func (ss *stateSyncer) AcceptedStateSummary(validatorID ids.ShortID, requestID uint32, hashes [][]byte) error {
	// ignores any late responses
	if requestID != ss.requestID {
		ss.Ctx.Log.Debug("Received an Out-of-Sync Accepted - validator: %v - expectedRequestID: %v, requestID: %v",
			validatorID, ss.requestID, requestID)
		return nil
	}

	if !ss.hasVoterBeenContacted(validatorID) {
		ss.Ctx.Log.Debug("Received an AcceptedStateSummary message from %s unexpectedly", validatorID)
		return nil
	}
	// Mark that we received a response from [validatorID]
	ss.markVoterResponded(validatorID)

	weight := uint64(0)
	if w, ok := ss.StateSyncBeacons.GetWeight(validatorID); ok {
		weight = w
	}

	for _, hash := range hashes {
		ws := ss.weightedSummaries[string(hash)]
		previousWeight := ws.weight
		newWeight, err := math.Add64(weight, previousWeight)
		if err != nil {
			ss.Ctx.Log.Error("Error calculating the Accepted votes - weight: %v, previousWeight: %v", weight, previousWeight)
			newWeight = stdmath.MaxUint64
		}
		ws.weight = newWeight
		ss.weightedSummaries[string(hash)] = ws
	}

	if err := ss.sendGetAccepted(); err != nil {
		return err
	}

	// wait on pending responses
	if ss.anyPendingVoterResponse() {
		return nil
	}

	// We've received the filtered accepted frontier from every state sync validator
	// Accept all containers that have a sufficient weight behind them
	summaries := make(summaryWeightedList, len(ss.weightedSummaries))
	for _, ws := range ss.weightedSummaries {
		if ws.weight < ss.Alpha {
			continue
		}
		summaries = append(summaries, ws)
	}
	sort.Sort(sort.Reverse(summaries))
	accepted := summaries.List()

	// if we don't have enough weight for the state summary to be accepted then retry or fail the state sync
	size := len(accepted)
	if size == 0 && ss.StateSyncBeacons.Len() > 0 {
		// retry the fast sync if the weight is not enough to fast sync
		failedBeaconWeight, err := ss.StateSyncBeacons.SubsetWeight(ss.failedVoters)
		if err != nil {
			return err
		}

		// in a zero network there will be no accepted votes but the voting weight will be greater than the failed weight
		if ss.Config.RetrySyncing && ss.StateSyncBeacons.Weight()-ss.Alpha < failedBeaconWeight {
			ss.Ctx.Log.Debug("Not enough votes received, restarting state sync... - Beacons: %d - Failed syncer: %d "+
				"- state sync attempt: %d", ss.StateSyncBeacons.Len(), ss.failedVoters.Len(), ss.attempts)
			return ss.restart(false)
		}
	}

	if !ss.restarted {
		ss.Ctx.Log.Info("State sync started syncing with %d vertices in the accepted frontier", size)
	} else {
		ss.Ctx.Log.Debug("State sync started syncing with %d vertices in the accepted frontier", size)
	}

	return ss.stateSyncVM.StateSync(accepted)
}

// StateSyncHandler interface implementation
func (ss *stateSyncer) GetAcceptedStateSummaryFailed(validatorID ids.ShortID, requestID uint32) error {
	// ignores any late responses
	if requestID != ss.requestID {
		ss.Ctx.Log.Debug("Received an Out-of-Sync GetAcceptedStateSummaryFailed - validator: %v - expectedRequestID: %v, requestID: %v",
			validatorID, ss.requestID, requestID)
		return nil
	}

	// If we can't get a response from [validatorID], act as though they said
	// that they think none of the containers we sent them in GetAccepted are accepted
	ss.failedAcceptedStateSummaries.Add(validatorID)

	return ss.AcceptedStateSummary(validatorID, requestID, [][]byte{})
}

// Engine interface implementation
func (ss *stateSyncer) Start(startReqID uint32) error {
	ss.requestID = startReqID
	ss.Ctx.SetState(snow.StateSyncing)
	return ss.startup()
}

func (ss *stateSyncer) startup() error {
	ss.Config.Ctx.Log.Info("starting state sync")
	ss.started = true

	// clear up messages trackers
	ss.weightedSummaries = make(map[string]weightedSummary)
	ss.frontierTracker.clear()
	ss.voteTracker.clear()

	// set beacons
	if err := ss.sampleSeeders(ss.StateSyncBeacons, ss.Config.SampleK); err != nil {
		return err
	}

	ss.storeVoters(ss.StateSyncBeacons)

	// initiate messages exchange
	ss.attempts++
	if !ss.hasSeedersToContact() {
		ss.Ctx.Log.Info("State syncing skipped due to no provided syncers")
		return ss.stateSyncVM.StateSync(nil)
	}

	ss.requestID++
	ss.sendGetStateSummaryFrontiers()
	return nil
}

func (ss *stateSyncer) restart(reset bool) error {
	// resets the attempts when we're pulling blocks/vertices we don't want to
	// fail the state sync at that stage
	if reset {
		ss.Ctx.Log.Debug("Checking for new state sync frontiers")

		ss.restarted = true
		ss.attempts = 0
	}

	if ss.attempts > 0 && ss.attempts%ss.RetrySyncingWarnFrequency == 0 {
		ss.Ctx.Log.Debug("continuing to attempt to state sync after %d failed attempts. Is this node connected to the internet?",
			ss.attempts)
	}

	return ss.startup()
}

// Ask up to [MaxOutstandingStateSyncRequests] state sync validators to send
// their accepted state summary
func (ss *stateSyncer) sendGetStateSummaryFrontiers() {
	if vdrs := ss.pickSeedersToContact(); vdrs.Len() > 0 {
		ss.Sender.SendGetStateSummaryFrontier(vdrs, ss.requestID)
		ss.markSeederContacted(vdrs)
	}
}

// Ask up to [MaxOutstandingStateSyncRequests] syncers validators to send
// their filtered accepted frontier
func (ss *stateSyncer) sendGetAccepted() error {
	if vdrs := ss.pickVotersToContact(); len(vdrs) > 0 {
		ss.Ctx.Log.Debug("sent %d more GetAcceptedStateSummary messages with %d more to send",
			vdrs.Len(), ss.targetVoters.Len())

		acceptedKeys := make([][]byte, 0, len(ss.weightedSummaries))
		for _, summary := range ss.weightedSummaries {
			key, _, err := ss.stateSyncVM.StateSyncGetKeyHash(summary.Summary)
			if err != nil {
				return err
			}
			acceptedKeys = append(acceptedKeys, key.Content)
		}
		ss.Sender.SendGetAcceptedStateSummary(vdrs, ss.requestID, acceptedKeys)
		ss.markVoterContacted(vdrs)
	}

	return nil
}

// AppHandler interface implementation
func (ss *stateSyncer) AppRequest(nodeID ids.ShortID, requestID uint32, deadline time.Time, request []byte) error {
	return ss.VM.AppRequest(nodeID, requestID, deadline, request)
}

// AppHandler interface implementation
func (ss *stateSyncer) AppResponse(nodeID ids.ShortID, requestID uint32, response []byte) error {
	return ss.VM.AppResponse(nodeID, requestID, response)
}

// AppHandler interface implementation
func (ss *stateSyncer) AppRequestFailed(nodeID ids.ShortID, requestID uint32) error {
	return ss.VM.AppRequestFailed(nodeID, requestID)
}

// InternalHandler interface implementation
func (ss *stateSyncer) Notify(msg common.Message) error {
	// if state sync and bootstrap is done, we shouldn't receive StateSyncDone from the VM
	ss.Ctx.Log.Verbo("snowman engine notified of %s from the vm", msg)
	switch msg {
	case common.PendingTxs:
		ss.Ctx.Log.Warn("Message %s received in state sync. Dropped.", msg.String())

	case common.StateSyncDone:
		// retrieve the blkID to request
		var err error
		ss.lastSummaryBlkID, err = ss.stateSyncVM.GetLastSummaryBlockID()
		if err != nil {
			ss.Ctx.Log.Warn("Could not retrieve last summary block ID to complete state sync. Err: %v", err)
			return err
		}
		return ss.requestBlk(ss.lastSummaryBlkID)

	default:
		ss.Ctx.Log.Warn("unexpected message from the VM: %s", msg)
	}
	return nil
}

func (ss *stateSyncer) requestBlk(blkID ids.ID) error {
	// pick random beacon
	vdrsList, err := ss.StateSyncBeacons.Sample(1)
	if err != nil {
		return err
	}
	vdrID := vdrsList[0].ID()

	// request the block
	ss.Sender.SendGet(vdrID, ss.requestID, blkID)
	return nil
}

// FetchHandler interface implementation
func (ss *stateSyncer) Put(validatorID ids.ShortID, requestID uint32, container []byte) error {
	// ignores any late responses
	if requestID != ss.requestID {
		ss.Ctx.Log.Debug("Received an Out-of-Sync Put - validator: %v - expectedRequestID: %v, requestID: %v",
			validatorID, ss.requestID, requestID)
		return nil
	}

	if err := ss.stateSyncVM.SetLastSummaryBlock(container); err != nil {
		ss.Ctx.Log.Warn("Could not accept last summary block, err :%v. Retrying block download.", err)
		return ss.requestBlk(ss.lastSummaryBlkID)
	}

	if err := ss.onDoneStateSyncing(ss.requestID); err != nil {
		ss.Ctx.Log.Warn("Could not complete state sync: %w", err)
	}

	return nil
}

func (ss *stateSyncer) GetFailed(validatorID ids.ShortID, requestID uint32) error {
	// ignores any late responses
	if requestID != ss.requestID {
		ss.Ctx.Log.Debug("Received an Out-of-Sync GetFailed - validator: %v - expectedRequestID: %v, requestID: %v",
			validatorID, ss.requestID, requestID)
		return nil
	}

	ss.Ctx.Log.Warn("Failed downloading Last Summary block. Retrying block download.")
	return ss.requestBlk(ss.lastSummaryBlkID)
}

// InternalHandler interface implementation
func (ss *stateSyncer) Connected(nodeID ids.ShortID, nodeVersion version.Application) error {
	if err := ss.VM.Connected(nodeID, nodeVersion); err != nil {
		return err
	}

	if err := ss.WeightTracker.AddWeightForNode(nodeID); err != nil {
		return err
	}

	if ss.WeightTracker.EnoughConnectedWeight() && !ss.started {
		ss.started = true
		return ss.startup()
	}

	return nil
}

// InternalHandler interface implementation
func (ss *stateSyncer) Disconnected(nodeID ids.ShortID) error {
	if err := ss.VM.Disconnected(nodeID); err != nil {
		return err
	}

	return ss.WeightTracker.RemoveWeightForNode(nodeID)
}

// Gossip implements the InternalHandler interface.
func (ss *stateSyncer) Gossip() error { return nil }

// Shutdown implements the InternalHandler interface.
func (ss *stateSyncer) Shutdown() error { return nil }

// Context implements the common.Engine interface.
func (ss *stateSyncer) Context() *snow.ConsensusContext { return ss.Config.Ctx }

// IsBootstrapped implements the common.Engine interface.
func (ss *stateSyncer) IsBootstrapped() bool { return ss.Ctx.GetState() == snow.NormalOp }

// Halt implements the InternalHandler interface
func (ss *stateSyncer) Halt() {}

// Timeout implements the InternalHandler interface
func (ss *stateSyncer) Timeout() error { return nil }

// HealthCheck implements the common.Engine interface.
func (ss *stateSyncer) HealthCheck() (interface{}, error) {
	vmIntf, vmErr := ss.VM.HealthCheck()
	intf := map[string]interface{}{
		"consensus": struct{}{},
		"vm":        vmIntf,
	}
	return intf, vmErr
}

// Engine interface implementation
func (ss *stateSyncer) GetVM() common.VM { return ss.VM }

func (ss *stateSyncer) IsEnabled() bool {
	if ss.stateSyncVM == nil {
		// state sync is not implemented
		return false
	}

	enabled, err := ss.stateSyncVM.StateSyncEnabled()
	switch {
	case err == common.ErrStateSyncableVMNotImplemented:
		// nothing to do, state sync is not implemented
		return false
	case err != nil:
		return false
	default:
		return enabled
	}
}
