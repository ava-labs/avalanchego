// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package syncer

import (
	"fmt"
	"time"

	stdmath "math"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/version"
)

var _ common.StateSyncer = &stateSyncer{}

// summary content as received from network, along with accumulated weight.
type weightedSummary struct {
	summary block.StateSummary
	weight  uint64
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
	locallyAvailableSummary block.StateSummary

	// Holds the beacons that were sampled for the accepted frontier
	// Won't be consumed as seeders are reached out. Used to rescale
	// alpha for frontiers
	frontierSeeders validators.Set
	// IDs of validators we should request state summary frontier from.
	// Will be consumed seeders are reached out for frontier.
	targetSeeders ids.NodeIDSet
	// IDs of validators we requested a state summary frontier from
	// but haven't received a reply yet. ID is cleared if/when reply arrives.
	pendingSeeders ids.NodeIDSet
	// IDs of validators that failed to respond with their state summary frontier
	failedSeeders ids.NodeIDSet

	// IDs of validators we should request filtering the accepted state summaries from
	targetVoters ids.NodeIDSet
	// IDs of validators we requested filtering the accepted state summaries from
	// but haven't received a reply yet. ID is cleared if/when reply arrives.
	pendingVoters ids.NodeIDSet
	// IDs of validators that failed to respond with their filtered accepted state summaries
	failedVoters ids.NodeIDSet

	// summaryID --> (summary, weight)
	weightedSummaries map[ids.ID]*weightedSummary

	// summaries received may be different even if referring to the same height
	// we keep a list of deduplcated height ready for voting
	summariesHeights       map[uint64]struct{}
	uniqueSummariesHeights []uint64

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

func (ss *stateSyncer) StateSummaryFrontier(validatorID ids.NodeID, requestID uint32, summaryBytes []byte) error {
	// ignores any late responses
	if requestID != ss.requestID {
		ss.Ctx.Log.Debug("Received an Out-of-Sync StateSummaryFrontier - validator: %v - expectedRequestID: %v, requestID: %v",
			validatorID, ss.requestID, requestID)
		return nil
	}

	if !ss.pendingSeeders.Contains(validatorID) {
		ss.Ctx.Log.Debug("Received a StateSummaryFrontier message from %s unexpectedly", validatorID)
		return nil
	}

	// Mark that we received a response from [validatorID]
	ss.pendingSeeders.Remove(validatorID)

	// retrieve summary ID and register frontier;
	// make sure next beacons are reached out
	// even in case invalid summaries are received
	if summary, err := ss.stateSyncVM.ParseStateSummary(summaryBytes); err == nil {
		ss.weightedSummaries[summary.ID()] = &weightedSummary{
			summary: summary,
		}

		height := summary.Height()
		if _, exists := ss.summariesHeights[height]; !exists {
			ss.summariesHeights[height] = struct{}{}
			ss.uniqueSummariesHeights = append(ss.uniqueSummariesHeights, height)
		}
	} else {
		ss.Ctx.Log.Debug("Could not parse summary from bytes: %s", err)
		ss.Ctx.Log.Verbo("%s", formatting.DumpBytes(summaryBytes))
	}

	return ss.receivedStateSummaryFrontier()
}

func (ss *stateSyncer) GetStateSummaryFrontierFailed(validatorID ids.NodeID, requestID uint32) error {
	// ignores any late responses
	if requestID != ss.requestID {
		ss.Ctx.Log.Debug("Received an Out-of-Sync GetStateSummaryFrontierFailed - validator: %v - expectedRequestID: %v, requestID: %v",
			validatorID, ss.requestID, requestID)
		return nil
	}

	// Mark that we didn't get a respose from [validatorID]
	ss.failedSeeders.Add(validatorID)
	ss.pendingSeeders.Remove(validatorID)

	return ss.receivedStateSummaryFrontier()
}

func (ss *stateSyncer) receivedStateSummaryFrontier() error {
	ss.sendGetStateSummaryFrontiers()

	// still waiting on requests
	if ss.pendingSeeders.Len() != 0 {
		return nil
	}

	// All nodes reached out for the summary frontier have responded or timed out.
	// If enough of them have indeed responded we'll go ahead and ask
	// each state syncer (not just a sample) to filter the list of state summaries
	// that we were told are on the accepted frontier.
	// If we got too many timeouts, we restart state syncing hoping that network
	// problems will go away and we can collect a qualified frontier.
	// We assume the frontier is qualified after an alpha proportion of frontier seeders have responded
	frontierAlpha := float64(ss.frontierSeeders.Weight()*ss.Alpha) / float64(ss.StateSyncBeacons.Weight())
	failedBeaconWeight, err := ss.StateSyncBeacons.SubsetWeight(ss.failedSeeders)
	if err != nil {
		return err
	}

	frontierStake := ss.frontierSeeders.Weight() - failedBeaconWeight
	if float64(frontierStake) < frontierAlpha {
		ss.Ctx.Log.Debug("Didn't receive enough frontiers - failed validators: %d, "+
			"state sync attempt: %d", ss.failedSeeders.Len(), ss.attempts)

		if ss.Config.RetryBootstrap {
			ss.Ctx.Log.Debug("Restarting state sync")
			return ss.restart()
		}
	}

	ss.requestID++
	ss.sendGetAcceptedStateSummaries()
	return nil
}

func (ss *stateSyncer) AcceptedStateSummary(validatorID ids.NodeID, requestID uint32, summaryIDs []ids.ID) error {
	// ignores any late responses
	if requestID != ss.requestID {
		ss.Ctx.Log.Debug("Received an Out-of-Sync Accepted - validator: %v - expectedRequestID: %v, requestID: %v",
			validatorID, ss.requestID, requestID)
		return nil
	}

	if !ss.pendingVoters.Contains(validatorID) {
		ss.Ctx.Log.Debug("Received an AcceptedStateSummary message from %s unexpectedly", validatorID)
		return nil
	}

	// Mark that we received a response from [validatorID]
	ss.pendingVoters.Remove(validatorID)

	weight, _ := ss.StateSyncBeacons.GetWeight(validatorID)
	for _, summaryID := range summaryIDs {
		ws, ok := ss.weightedSummaries[summaryID]
		if !ok {
			ss.Ctx.Log.Debug("Received a vote from %s for unknown summary %s. Skipped.", validatorID, summaryID)
			continue
		}

		newWeight, err := math.Add64(weight, ws.weight)
		if err != nil {
			ss.Ctx.Log.Error("Error calculating the Accepted votes - weight: %v, previousWeight: %v", weight, ws.weight)
			newWeight = stdmath.MaxUint64
		}
		ws.weight = newWeight
	}

	ss.sendGetAcceptedStateSummaries()

	// wait on pending responses
	if ss.pendingVoters.Len() != 0 {
		return nil
	}

	// We've received the filtered accepted frontier from every state sync validator
	// Drop all summaries without a sufficient weight behind them
	for summaryID, ws := range ss.weightedSummaries {
		if ws.weight < ss.Alpha {
			ss.Ctx.Log.Debug("Removing summary %s due to an insufficient weight %d - needed %d",
				summaryID, ws.weight, ss.Alpha,
			)
			delete(ss.weightedSummaries, summaryID)
		}
	}

	// if we don't have enough weight for the state summary to be accepted then retry or fail the state sync
	size := len(ss.weightedSummaries)
	if size == 0 {
		// retry the state sync if the weight is not enough to state sync
		failedBeaconWeight, err := ss.StateSyncBeacons.SubsetWeight(ss.failedVoters)
		if err != nil {
			return err
		}

		// if we had too many timeouts when asking for validator votes, we should restart
		// state sync hoping for the network problems to go away; otherwise, we received
		// enough (>= ss.Alpha) responses, but no state summary was supported by a majority
		// of validators (i.e. votes are split between minorities supporting different state
		// summaries), so there is no point in retrying state sync; we should move ahead to bootstrapping
		votingStakes := ss.StateSyncBeacons.Weight() - failedBeaconWeight
		if ss.Config.RetryBootstrap && votingStakes < ss.Alpha {
			ss.Ctx.Log.Debug("Not enough votes received, restarting state sync... - Beacons: %d - Failed syncer: %d "+
				"- state sync attempt: %d", ss.StateSyncBeacons.Len(), ss.failedVoters.Len(), ss.attempts)
			return ss.restart()
		}

		ss.Ctx.Log.Info("No acceptable summaries found, skipping state sync")

		// if we do not restart state sync, move on to bootstrapping.
		return ss.onDoneStateSyncing(ss.requestID)
	}

	preferredStateSummary := ss.selectSyncableStateSummary()
	ss.Ctx.Log.Info("Selected summary %s out of %d to start state sync",
		preferredStateSummary.ID(), size,
	)

	startedSyncing, err := preferredStateSummary.Accept()
	if err != nil {
		return err
	}
	if startedSyncing {
		// summary was accepted and VM is state syncing.
		// Engine will wait for notification of state sync done.
		return nil
	}

	// VM did not accept the summary, move on to bootstrapping.
	return ss.onDoneStateSyncing(ss.requestID)
}

// selectSyncableStateSummary chooses a state summary from all
// the network validated summaries.
func (ss *stateSyncer) selectSyncableStateSummary() block.StateSummary {
	var (
		maxSummaryHeight      uint64
		preferredStateSummary block.StateSummary
	)

	// by default pick highest summary, unless locallyAvailableSummary is still valid.
	// In such case we pick locallyAvailableSummary to allow VM resuming state syncing.
	for id, ws := range ss.weightedSummaries {
		if ss.locallyAvailableSummary != nil && id == ss.locallyAvailableSummary.ID() {
			return ss.locallyAvailableSummary
		}

		height := ws.summary.Height()
		if maxSummaryHeight <= height {
			maxSummaryHeight = height
			preferredStateSummary = ws.summary
		}
	}
	return preferredStateSummary
}

func (ss *stateSyncer) GetAcceptedStateSummaryFailed(validatorID ids.NodeID, requestID uint32) error {
	// ignores any late responses
	if requestID != ss.requestID {
		ss.Ctx.Log.Debug("Received an Out-of-Sync GetAcceptedStateSummaryFailed - validator: %v - expectedRequestID: %v, requestID: %v",
			validatorID, ss.requestID, requestID)
		return nil
	}

	// If we can't get a response from [validatorID], act as though they said
	// that they think none of the containers we sent them in GetAccepted are accepted
	ss.failedVoters.Add(validatorID)

	return ss.AcceptedStateSummary(validatorID, requestID, nil)
}

func (ss *stateSyncer) Start(startReqID uint32) error {
	ss.Ctx.Log.Info("Starting state sync...")

	ss.Ctx.SetState(snow.StateSyncing)
	if err := ss.VM.SetState(snow.StateSyncing); err != nil {
		return fmt.Errorf("failed to notify VM that state syncing has started: %w", err)
	}

	ss.requestID = startReqID

	if !ss.StartupTracker.ShouldStart() {
		return nil
	}

	ss.started = true
	return ss.startup()
}

// startup do start the whole state sync process by
// sampling frontier seeders, listing state syncers to request votes to
// and reaching out frontier seeders if any. Othewise it move immediately
// to bootstrapping. Unlike Start, startup does not check
// whether sufficient stake amount is connected.
func (ss *stateSyncer) startup() error {
	ss.Config.Ctx.Log.Info("starting state sync")

	// clear up messages trackers
	ss.weightedSummaries = make(map[ids.ID]*weightedSummary)
	ss.summariesHeights = make(map[uint64]struct{})
	ss.uniqueSummariesHeights = nil

	ss.targetSeeders.Clear()
	ss.pendingSeeders.Clear()
	ss.failedSeeders.Clear()
	ss.targetVoters.Clear()
	ss.pendingVoters.Clear()
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

	// check if there is an ongoing state sync; if so add its state summary
	// to the frontier to request votes on
	// Note: database.ErrNotFound means there is no ongoing summary
	localSummary, err := ss.stateSyncVM.GetOngoingSyncStateSummary()
	switch err {
	case database.ErrNotFound:
		// no action needed
	case nil:
		ss.locallyAvailableSummary = localSummary
		ss.weightedSummaries[localSummary.ID()] = &weightedSummary{
			summary: localSummary,
		}

		height := localSummary.Height()
		ss.summariesHeights[height] = struct{}{}
		ss.uniqueSummariesHeights = append(ss.uniqueSummariesHeights, height)
	default:
		return err
	}

	// initiate messages exchange
	ss.attempts++
	if ss.targetSeeders.Len() == 0 {
		ss.Ctx.Log.Info("State syncing skipped due to no provided syncers")
		return ss.onDoneStateSyncing(ss.requestID)
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

// Ask up to [common.MaxOutstandingBroadcastRequests] state sync validators at a time
// to send their accepted state summary. It is called again until there are
// no more seeders to be reached in the pending set
func (ss *stateSyncer) sendGetStateSummaryFrontiers() {
	vdrs := ids.NewNodeIDSet(1)
	for ss.targetSeeders.Len() > 0 && ss.pendingSeeders.Len() < common.MaxOutstandingBroadcastRequests {
		vdr, _ := ss.targetSeeders.Pop()
		vdrs.Add(vdr)
		ss.pendingSeeders.Add(vdr)
	}

	if vdrs.Len() > 0 {
		ss.Sender.SendGetStateSummaryFrontier(vdrs, ss.requestID)
	}
}

// Ask up to [common.MaxOutstandingStateSyncRequests] syncers validators to send
// their filtered accepted frontier. It is called again until there are
// no more voters to be reached in the pending set.
func (ss *stateSyncer) sendGetAcceptedStateSummaries() {
	vdrs := ids.NewNodeIDSet(1)
	for ss.targetVoters.Len() > 0 && ss.pendingVoters.Len() < common.MaxOutstandingBroadcastRequests {
		vdr, _ := ss.targetVoters.Pop()
		vdrs.Add(vdr)
		ss.pendingVoters.Add(vdr)
	}

	if len(vdrs) > 0 {
		ss.Sender.SendGetAcceptedStateSummary(vdrs, ss.requestID, ss.uniqueSummariesHeights)
		ss.Ctx.Log.Debug("sent %d more GetAcceptedStateSummary messages with %d more to send",
			vdrs.Len(), ss.targetVoters.Len())
	}
}

func (ss *stateSyncer) AppRequest(nodeID ids.NodeID, requestID uint32, deadline time.Time, request []byte) error {
	return ss.VM.AppRequest(nodeID, requestID, deadline, request)
}

func (ss *stateSyncer) AppResponse(nodeID ids.NodeID, requestID uint32, response []byte) error {
	return ss.VM.AppResponse(nodeID, requestID, response)
}

func (ss *stateSyncer) AppRequestFailed(nodeID ids.NodeID, requestID uint32) error {
	return ss.VM.AppRequestFailed(nodeID, requestID)
}

func (ss *stateSyncer) Notify(msg common.Message) error {
	if msg != common.StateSyncDone {
		ss.Ctx.Log.Warn("unexpected message from the VM: %s", msg)
		return nil
	}
	return ss.onDoneStateSyncing(ss.requestID)
}

func (ss *stateSyncer) Connected(nodeID ids.NodeID, nodeVersion *version.Application) error {
	if err := ss.VM.Connected(nodeID, nodeVersion); err != nil {
		return err
	}

	if err := ss.StartupTracker.Connected(nodeID, nodeVersion); err != nil {
		return err
	}

	if ss.started || !ss.StartupTracker.ShouldStart() {
		return nil
	}

	ss.started = true
	return ss.startup()
}

func (ss *stateSyncer) Disconnected(nodeID ids.NodeID) error {
	if err := ss.VM.Disconnected(nodeID); err != nil {
		return err
	}

	return ss.StartupTracker.Disconnected(nodeID)
}

func (ss *stateSyncer) Gossip() error { return nil }

func (ss *stateSyncer) Shutdown() error {
	ss.Config.Ctx.Log.Info("shutting down state syncer")
	return ss.VM.Shutdown()
}

func (ss *stateSyncer) Halt() {}

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
