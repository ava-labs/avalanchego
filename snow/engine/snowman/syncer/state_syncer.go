// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package syncer

import (
	"time"

	stdmath "math"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/version"
)

const (
	// maxOutstandingStateSyncRequests is the maximum number of
	// messages sent but not responded to/failed for each relevant message type
	maxOutstandingStateSyncRequests = 50
)

var _ common.StateSyncer = &stateSyncer{}

// summary content as received from network, along with accumulated weight.
type weightedSummary struct {
	common.Summary
	weight uint64
}

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

	// Tracks the last requestID that was used in a request
	requestID uint32

	stateSyncVM        block.StateSyncableVM
	onDoneStateSyncing func(lastReqID uint32) error

	// we track the (possibly nil) local summary to help engine
	// choosing among multiple validated summaries
	locallyAvailableSummary common.Summary
	// once vm finishes processing rebuilding its state via state summaries
	// the full block associated with state summary must be download.
	// lastSummaryBlkRequestedFrom tracks the validator reached out to for the full block
	// and ensures that the full block will be downloaded only from that validator.
	lastSummaryBlkRequestedFrom ids.ShortID
	lastSummaryBlkID            ids.ID

	// Holds the beacons that were sampled for the accepted frontier
	frontierSeeders validators.Set
	// IDs of validators we should request state summary frontier from
	targetSeeders ids.ShortSet
	// IDs of validators we requested a state summary frontier from
	// but haven't received a reply yet. ID is cleared if/when reply arrives.
	contactedSeeders ids.ShortSet
	// IDs of validators that failed to respond with their state summary frontier
	failedSeeders ids.ShortSet

	// IDs of validators we should request filtering the accepted state summaries from
	targetVoters ids.ShortSet
	// IDs of validators we requested filtering the accepted state summaries from
	// but haven't received a reply yet. ID is cleared if/when reply arrives.
	contactedVoters ids.ShortSet
	// IDs of validators that failed to respond with their filtered accepted state summaries
	failedVoters ids.ShortSet

	// summaryID --> (summary, weight)
	weightedSummaries map[ids.ID]weightedSummary

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

func (ss *stateSyncer) StateSummaryFrontier(validatorID ids.ShortID, requestID uint32, summaryBytes []byte) error {
	// ignores any late responses
	if requestID != ss.requestID {
		ss.Ctx.Log.Debug("Received an Out-of-Sync StateSummaryFrontier - validator: %v - expectedRequestID: %v, requestID: %v",
			validatorID, ss.requestID, requestID)
		return nil
	}

	if !ss.contactedSeeders.Contains(validatorID) {
		ss.Ctx.Log.Debug("Received a StateSummaryFrontier message from %s unexpectedly", validatorID)
		return nil
	}

	// Mark that we received a response from [validatorID]
	ss.contactedSeeders.Remove(validatorID)

	// retrieve summary ID and register frontier;
	// make sure next beacons are reached out
	// even in case invalid summaries are received
	if summary, err := ss.stateSyncVM.ParseStateSummary(summaryBytes); err == nil {
		if _, exists := ss.weightedSummaries[summary.ID()]; !exists {
			ss.weightedSummaries[summary.ID()] = weightedSummary{
				Summary: summary,
			}
		}
	} else {
		ss.Ctx.Log.Debug("Could not parse summary from bytes%s: %v", summaryBytes, err)
	}

	ss.sendGetStateSummaryFrontiers()

	// still waiting on requests
	if ss.contactedSeeders.Len() != 0 {
		return nil
	}

	// We've received the accepted frontier from every state syncer
	// Ask each state syncer to filter the list of containers that we were
	// told are on the accepted frontier such that the list only contains containers
	// they think are accepted

	// Create a newAlpha taking using the sampled beacon
	// Keep the proportion of b.Alpha in the newAlpha
	// newAlpha := totalSampledWeight * b.Alpha / totalWeight
	newAlpha := float64(ss.frontierSeeders.Weight()*ss.Alpha) / float64(ss.StateSyncBeacons.Weight())

	failedBeaconWeight, err := ss.StateSyncBeacons.SubsetWeight(ss.failedSeeders)
	if err != nil {
		return err
	}

	// fail the fast sync if the weight is not enough to fast sync
	if float64(ss.frontierSeeders.Weight())-newAlpha < float64(failedBeaconWeight) {
		if ss.Config.RetryBootstrap {
			ss.Ctx.Log.Debug("Not enough frontiers received, restarting state sync... - Beacons: %d - Failed Bootstrappers: %d "+
				"- state sync attempt: %d", ss.StateSyncBeacons.Len(), ss.failedSeeders.Len(), ss.attempts)
			return ss.restart()
		}

		ss.Ctx.Log.Debug("Didn't receive enough frontiers - failed validators: %d, "+
			"state sync attempt: %d", ss.failedSeeders.Len(), ss.attempts)
	}

	ss.requestID++
	return ss.sendGetAccepted()
}

func (ss *stateSyncer) GetStateSummaryFrontierFailed(validatorID ids.ShortID, requestID uint32) error {
	// ignores any late responses
	if requestID != ss.requestID {
		ss.Ctx.Log.Debug("Received an Out-of-Sync GetStateSummaryFrontierFailed - validator: %v - expectedRequestID: %v, requestID: %v",
			validatorID, ss.requestID, requestID)
		return nil
	}

	// If we can't get a response from [validatorID], act as though they said their state summary frontier is empty
	// and we add the validator to the failed list
	ss.failedSeeders.Add(validatorID)
	return ss.StateSummaryFrontier(validatorID, requestID, []byte{})
}

func (ss *stateSyncer) AcceptedStateSummary(validatorID ids.ShortID, requestID uint32, summaryIDs []ids.ID) error {
	// ignores any late responses
	if requestID != ss.requestID {
		ss.Ctx.Log.Debug("Received an Out-of-Sync Accepted - validator: %v - expectedRequestID: %v, requestID: %v",
			validatorID, ss.requestID, requestID)
		return nil
	}

	if !ss.contactedVoters.Contains(validatorID) {
		ss.Ctx.Log.Debug("Received an AcceptedStateSummary message from %s unexpectedly", validatorID)
		return nil
	}
	// Mark that we received a response from [validatorID]
	ss.contactedVoters.Remove(validatorID)

	weight := uint64(0)
	if w, ok := ss.StateSyncBeacons.GetWeight(validatorID); ok {
		weight = w
	}

	for _, summaryID := range summaryIDs {
		ws, ok := ss.weightedSummaries[summaryID]
		if !ok {
			ss.Ctx.Log.Debug("Received a vote from %s for unknown summary %s. Skipped.", validatorID, summaryID)
			continue
		}
		previousWeight := ws.weight
		newWeight, err := math.Add64(weight, previousWeight)
		if err != nil {
			ss.Ctx.Log.Error("Error calculating the Accepted votes - weight: %v, previousWeight: %v", weight, previousWeight)
			newWeight = stdmath.MaxUint64
		}
		ws.weight = newWeight
		ss.weightedSummaries[summaryID] = ws
	}

	if err := ss.sendGetAccepted(); err != nil {
		return err
	}

	// wait on pending responses
	if ss.contactedVoters.Len() != 0 {
		return nil
	}

	// We've received the filtered accepted frontier from every state sync validator
	// Drop all summaries without a sufficient weight behind them
	for key, ws := range ss.weightedSummaries {
		if ws.weight < ss.Alpha {
			delete(ss.weightedSummaries, key)
		}
	}

	// if we don't have enough weight for the state summary to be accepted then retry or fail the state sync
	size := len(ss.weightedSummaries)
	if size == 0 && ss.StateSyncBeacons.Len() > 0 {
		// retry the fast sync if the weight is not enough to fast sync
		failedBeaconWeight, err := ss.StateSyncBeacons.SubsetWeight(ss.failedVoters)
		if err != nil {
			return err
		}

		// in a zero network there will be no accepted votes but the voting weight will be greater than the failed weight
		if ss.Config.RetryBootstrap && ss.StateSyncBeacons.Weight()-ss.Alpha < failedBeaconWeight {
			ss.Ctx.Log.Debug("Not enough votes received, restarting state sync... - Beacons: %d - Failed syncer: %d "+
				"- state sync attempt: %d", ss.StateSyncBeacons.Len(), ss.failedVoters.Len(), ss.attempts)
			return ss.restart()
		}
	}

	ss.Ctx.Log.Info("State sync started syncing with %d vertices in the accepted frontier", size)
	bestStateSummary := ss.selectSyncableStateSummary()
	return bestStateSummary.Accept()
}

// selectSyncableStateSummary encapsules the logic to choose a state summary
// out of all the network validated one
func (ss *stateSyncer) selectSyncableStateSummary() common.Summary {
	highestSummary := uint64(0)
	bestID := ids.Empty

	// by default pick highest summary, unless locallyAvailableSummary is still valid.
	// In such case we pick locallyAvailableSummary to allow VM resuming state syncing.
	for id, ws := range ss.weightedSummaries {
		if id == ss.locallyAvailableSummary.ID() {
			bestID = id
			break
		}

		if highestSummary < ws.Summary.Height() {
			highestSummary = ws.Summary.Height()
			bestID = id
		}
	}
	return ss.weightedSummaries[bestID].Summary
}

func (ss *stateSyncer) GetAcceptedStateSummaryFailed(validatorID ids.ShortID, requestID uint32) error {
	// ignores any late responses
	if requestID != ss.requestID {
		ss.Ctx.Log.Debug("Received an Out-of-Sync GetAcceptedStateSummaryFailed - validator: %v - expectedRequestID: %v, requestID: %v",
			validatorID, ss.requestID, requestID)
		return nil
	}

	// If we can't get a response from [validatorID], act as though they said
	// that they think none of the containers we sent them in GetAccepted are accepted
	ss.failedVoters.Add(validatorID)

	return ss.AcceptedStateSummary(validatorID, requestID, []ids.ID{})
}

func (ss *stateSyncer) Start(startReqID uint32) error {
	ss.requestID = startReqID
	ss.Ctx.SetState(snow.StateSyncing)

	if !ss.WeightTracker.EnoughConnectedWeight() {
		return nil
	}

	ss.started = true
	return ss.startup()
}

func (ss *stateSyncer) startup() error {
	ss.Config.Ctx.Log.Info("starting state sync")

	// clear up messages trackers
	ss.weightedSummaries = make(map[ids.ID]weightedSummary)

	ss.targetSeeders.Clear()
	ss.contactedSeeders.Clear()
	ss.failedSeeders.Clear()
	ss.targetVoters.Clear()
	ss.contactedVoters.Clear()
	ss.failedVoters.Clear()

	// sample K beacons to retrieve frontier from
	beacons, err := ss.StateSyncBeacons.Sample(ss.Config.SampleK)
	if err != nil {
		return err
	}

	ss.frontierSeeders = validators.NewSet()
	if err = ss.frontierSeeders.Set(beacons); err != nil {
		return err
	}

	for _, vdr := range beacons {
		vdrID := vdr.ID()
		ss.targetSeeders.Add(vdrID)
	}

	// list all beacons, to reach them for voting on frontier
	for _, vdr := range ss.StateSyncBeacons.List() {
		vdrID := vdr.ID()
		ss.targetVoters.Add(vdrID)
	}

	// check if there is an ongoing state sync; if so add its state localSummary
	// to the frontier to request votes on
	localSummary, err := ss.stateSyncVM.GetOngoingStateSyncSummary()
	if err != nil {
		return err
	}
	ss.locallyAvailableSummary = localSummary

	// register localSummary among those to validate over netwok,
	// unless its the emptySummary
	if localSummary.ID() != ids.Empty {
		ss.weightedSummaries[localSummary.ID()] = weightedSummary{
			Summary: localSummary,
		}
	}

	// initiate messages exchange
	ss.attempts++
	if ss.targetSeeders.Len() == 0 {
		// we make sure that a state summary is always eventually called if state sync is enabled
		ss.Ctx.Log.Info("State syncing skipped due to no provided syncers")
		ss.locallyAvailableSummary.Accept()
	}

	ss.requestID++
	ss.sendGetStateSummaryFrontiers()
	return nil
}

func (ss *stateSyncer) restart() error {
	if ss.attempts > 0 && ss.attempts%ss.RetryBootstrapWarnFrequency == 0 {
		ss.Ctx.Log.Info("continuing to attempt to state sync after %d failed attempts. Is this node connected to the internet?",
			ss.attempts)
	}

	return ss.startup()
}

// Ask up to [maxOutstandingStateSyncRequests] state sync validators at times
// to send their accepted state summary. It is called again until there are
// no more seeders to be reached in the pending set
func (ss *stateSyncer) sendGetStateSummaryFrontiers() {
	vdrs := ids.NewShortSet(1)
	for ss.targetSeeders.Len() > 0 && vdrs.Len() < maxOutstandingStateSyncRequests {
		vdr, _ := ss.targetSeeders.Pop()
		vdrs.Add(vdr)
	}

	if vdrs.Len() > 0 {
		ss.Sender.SendGetStateSummaryFrontier(vdrs, ss.requestID)
		ss.contactedSeeders.Add(vdrs.List()...)
	}
}

// Ask up to [maxOutstandingStateSyncRequests] syncers validators to send
// their filtered accepted frontier. It is called again until there are
// no more voters to be reached in the pending set.
func (ss *stateSyncer) sendGetAccepted() error {
	// pick voters to contact
	vdrs := ids.NewShortSet(1)
	for ss.targetVoters.Len() > 0 && vdrs.Len() < maxOutstandingStateSyncRequests {
		vdr, _ := ss.targetVoters.Pop()
		vdrs.Add(vdr)
	}

	if len(vdrs) == 0 {
		return nil
	}

	acceptedKeys := make([]uint64, 0, len(ss.weightedSummaries))
	for _, summary := range ss.weightedSummaries {
		acceptedKeys = append(acceptedKeys, summary.Height())
	}
	ss.Sender.SendGetAcceptedStateSummary(vdrs, ss.requestID, acceptedKeys)
	ss.contactedVoters.Add(vdrs.List()...)
	ss.Ctx.Log.Debug("sent %d more GetAcceptedStateSummary messages with %d more to send",
		vdrs.Len(), ss.targetVoters.Len())
	return nil
}

func (ss *stateSyncer) AppRequest(nodeID ids.ShortID, requestID uint32, deadline time.Time, request []byte) error {
	return ss.VM.AppRequest(nodeID, requestID, deadline, request)
}

func (ss *stateSyncer) AppResponse(nodeID ids.ShortID, requestID uint32, response []byte) error {
	return ss.VM.AppResponse(nodeID, requestID, response)
}

func (ss *stateSyncer) AppRequestFailed(nodeID ids.ShortID, requestID uint32) error {
	return ss.VM.AppRequestFailed(nodeID, requestID)
}

func (ss *stateSyncer) Notify(msg common.Message) error {
	// if state sync and bootstrap is done, we shouldn't receive StateSyncDone from the VM
	ss.Ctx.Log.Verbo("snowman engine notified of %s from the vm", msg)
	switch msg {
	case common.PendingTxs:
		ss.Ctx.Log.Warn("Message %s received in state sync. Dropped.", msg.String())

	case common.StateSyncSkipped:
		return ss.onDoneStateSyncing(ss.requestID)

	case common.StateSyncDone:
		// retrieve the blkID to request
		var err error
		ss.lastSummaryBlkID, _, err = ss.stateSyncVM.GetStateSyncResult()
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

// following completion of state sync on VM side, engine needs to request
// the full block associated with state summary.
func (ss *stateSyncer) requestBlk(blkID ids.ID) error {
	// pick random beacon
	vdrsList, err := ss.StateSyncBeacons.Sample(1)
	if err != nil {
		return err
	}
	vdrID := vdrsList[0].ID()

	// request the block
	ss.requestID++
	ss.Sender.SendGet(vdrID, ss.requestID, blkID)
	ss.lastSummaryBlkRequestedFrom = vdrID
	return nil
}

// following completion of state sync on VM side, block associated with state summary is requested.
// Pass it to VM, declare state sync done and move onto bootstrapping
func (ss *stateSyncer) Put(validatorID ids.ShortID, requestID uint32, blkBytes []byte) error {
	// ignores any late responses
	if requestID != ss.requestID {
		ss.Ctx.Log.Debug("Received an Out-of-Sync Put - validator: %v - expectedRequestID: %v, requestID: %v",
			validatorID, ss.requestID, requestID)
		return nil
	}

	if validatorID != ss.lastSummaryBlkRequestedFrom {
		ss.Ctx.Log.Debug("Received a Put message from %s unexpectedly", validatorID)
		return nil
	}

	blk, err := ss.VM.ParseBlock(blkBytes)
	if err != nil {
		ss.Ctx.Log.Debug("Received unparsable block from vdr %s, err %v. Requesting it again.",
			validatorID,
			err,
		)
		return ss.requestBlk(ss.lastSummaryBlkID)
	}

	rcvdBlkID := blk.ID()
	if rcvdBlkID != ss.lastSummaryBlkID {
		ss.Ctx.Log.Debug("Received wrong block; expected ID %s, received ID %s, Requesting it again.",
			rcvdBlkID,
			ss.lastSummaryBlkID)
		return ss.requestBlk(ss.lastSummaryBlkID)
	}

	if err := ss.stateSyncVM.SetLastStateSummaryBlock(blkBytes); err != nil {
		return err
	}

	return ss.onDoneStateSyncing(ss.requestID)
}

// following completion of state sync on VM side, block associated with state summary is requested.
// Since Put failed, request again the block to a different validator
func (ss *stateSyncer) GetFailed(validatorID ids.ShortID, requestID uint32) error {
	// ignores any late responses
	if requestID != ss.requestID {
		ss.Ctx.Log.Debug("Received an Out-of-Sync GetFailed - validator: %v - expectedRequestID: %v, requestID: %v",
			validatorID, ss.requestID, requestID)
		return ss.requestBlk(ss.lastSummaryBlkID)
	}

	ss.Ctx.Log.Warn("Failed downloading Last Summary block. Retrying block download.")
	return ss.requestBlk(ss.lastSummaryBlkID)
}

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

func (ss *stateSyncer) Disconnected(nodeID ids.ShortID) error {
	if err := ss.VM.Disconnected(nodeID); err != nil {
		return err
	}

	return ss.WeightTracker.RemoveWeightForNode(nodeID)
}

func (ss *stateSyncer) Gossip() error { return nil }

func (ss *stateSyncer) Shutdown() error {
	ss.Config.Ctx.Log.Info("shutting down state syncer")
	return ss.VM.Shutdown()
}

func (ss *stateSyncer) Context() *snow.ConsensusContext { return ss.Config.Ctx }

func (ss *stateSyncer) IsBootstrapped() bool { return ss.Ctx.GetState() == snow.NormalOp }

// Halt implements the InternalHandler interface
func (ss *stateSyncer) Halt() {}

// Timeout implements the InternalHandler interface
func (ss *stateSyncer) Timeout() error { return nil }

func (ss *stateSyncer) HealthCheck() (interface{}, error) {
	vmIntf, vmErr := ss.VM.HealthCheck()
	intf := map[string]interface{}{
		"consensus": struct{}{},
		"vm":        vmIntf,
	}
	return intf, vmErr
}

func (ss *stateSyncer) GetVM() common.VM { return ss.VM }

func (ss *stateSyncer) IsEnabled() (bool, error) {
	if ss.stateSyncVM == nil {
		// state sync is not implemented
		return false, nil
	}

	return ss.stateSyncVM.StateSyncEnabled()
}
