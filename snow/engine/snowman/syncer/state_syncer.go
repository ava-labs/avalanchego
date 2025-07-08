// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package syncer

import (
	"context"
	"fmt"
	"math"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/proto/pb/p2p"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/version"

	safemath "github.com/ava-labs/avalanchego/utils/math"
)

// maxOutstandingBroadcastRequests is the maximum number of requests to have
// outstanding when broadcasting.
const maxOutstandingBroadcastRequests = 50

var _ common.StateSyncer = (*stateSyncer)(nil)

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
	onDoneStateSyncing func(ctx context.Context, lastReqID uint32) error

	// we track the (possibly nil) local summary to help engine
	// choosing among multiple validated summaries
	locallyAvailableSummary block.StateSummary

	// Holds the beacons that were sampled for the accepted frontier
	// Won't be consumed as seeders are reached out. Used to rescale
	// alpha for frontiers
	frontierSeeders validators.Manager
	// IDs of validators we should request state summary frontier from.
	// Will be consumed seeders are reached out for frontier.
	targetSeeders set.Set[ids.NodeID]
	// IDs of validators we requested a state summary frontier from
	// but haven't received a reply yet. ID is cleared if/when reply arrives.
	pendingSeeders set.Set[ids.NodeID]
	// IDs of validators that failed to respond with their state summary frontier
	failedSeeders set.Set[ids.NodeID]

	// IDs of validators we should request filtering the accepted state summaries from
	targetVoters set.Set[ids.NodeID]
	// IDs of validators we requested filtering the accepted state summaries from
	// but haven't received a reply yet. ID is cleared if/when reply arrives.
	pendingVoters set.Set[ids.NodeID]
	// IDs of validators that failed to respond with their filtered accepted state summaries
	failedVoters set.Set[ids.NodeID]

	// summaryID --> (summary, weight)
	weightedSummaries map[ids.ID]*weightedSummary

	// summaries received may be different even if referring to the same height
	// we keep a list of deduplicated height ready for voting
	summariesHeights       set.Set[uint64]
	uniqueSummariesHeights []uint64
}

func New(
	cfg Config,
	onDoneStateSyncing func(ctx context.Context, lastReqID uint32) error,
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
		AppHandler:              cfg.VM,
		stateSyncVM:             ssVM,
		onDoneStateSyncing:      onDoneStateSyncing,
	}
}

func (ss *stateSyncer) Start(ctx context.Context, startReqID uint32) error {
	ss.Ctx.Log.Info("starting state sync")

	ss.Ctx.State.Set(snow.EngineState{
		Type:  p2p.EngineType_ENGINE_TYPE_SNOWMAN,
		State: snow.StateSyncing,
	})
	if err := ss.VM.SetState(ctx, snow.StateSyncing); err != nil {
		return fmt.Errorf("failed to notify VM that state syncing has started: %w", err)
	}

	ss.requestID = startReqID

	return ss.tryStartSyncing(ctx)
}

func (ss *stateSyncer) Connected(ctx context.Context, nodeID ids.NodeID, nodeVersion *version.Application) error {
	if err := ss.VM.Connected(ctx, nodeID, nodeVersion); err != nil {
		return err
	}

	if err := ss.StartupTracker.Connected(ctx, nodeID, nodeVersion); err != nil {
		return err
	}

	return ss.tryStartSyncing(ctx)
}

func (ss *stateSyncer) Disconnected(ctx context.Context, nodeID ids.NodeID) error {
	if err := ss.VM.Disconnected(ctx, nodeID); err != nil {
		return err
	}

	return ss.StartupTracker.Disconnected(ctx, nodeID)
}

// tryStartSyncing will start syncing the first time it is called while the
// startupTracker is reporting that the protocol should start.
func (ss *stateSyncer) tryStartSyncing(ctx context.Context) error {
	if ss.started || !ss.StartupTracker.ShouldStart() {
		return nil
	}

	ss.started = true
	return ss.startup(ctx)
}

func (ss *stateSyncer) StateSummaryFrontier(ctx context.Context, nodeID ids.NodeID, requestID uint32, summaryBytes []byte) error {
	// ignores any late responses
	if requestID != ss.requestID {
		ss.Ctx.Log.Debug("received out-of-sync StateSummaryFrontier message",
			zap.Stringer("nodeID", nodeID),
			zap.Uint32("expectedRequestID", ss.requestID),
			zap.Uint32("requestID", requestID),
		)
		return nil
	}

	if !ss.pendingSeeders.Contains(nodeID) {
		ss.Ctx.Log.Debug("received unexpected StateSummaryFrontier message",
			zap.Stringer("nodeID", nodeID),
		)
		return nil
	}

	// Mark that we received a response from [nodeID]
	ss.pendingSeeders.Remove(nodeID)

	// retrieve summary ID and register frontier;
	// make sure next beacons are reached out
	// even in case invalid summaries are received
	if summary, err := ss.stateSyncVM.ParseStateSummary(ctx, summaryBytes); err == nil {
		ss.weightedSummaries[summary.ID()] = &weightedSummary{
			summary: summary,
		}

		height := summary.Height()
		if !ss.summariesHeights.Contains(height) {
			ss.summariesHeights.Add(height)
			ss.uniqueSummariesHeights = append(ss.uniqueSummariesHeights, height)
		}
	} else {
		if ss.Ctx.Log.Enabled(logging.Verbo) {
			ss.Ctx.Log.Verbo("failed to parse summary",
				zap.Binary("summary", summaryBytes),
				zap.Error(err),
			)
		} else {
			ss.Ctx.Log.Debug("failed to parse summary",
				zap.Error(err),
			)
		}
	}

	return ss.receivedStateSummaryFrontier(ctx)
}

func (ss *stateSyncer) GetStateSummaryFrontierFailed(ctx context.Context, nodeID ids.NodeID, requestID uint32) error {
	// ignores any late responses
	if requestID != ss.requestID {
		ss.Ctx.Log.Debug("received out-of-sync GetStateSummaryFrontierFailed message",
			zap.Stringer("nodeID", nodeID),
			zap.Uint32("expectedRequestID", ss.requestID),
			zap.Uint32("requestID", requestID),
		)
		return nil
	}

	// Mark that we didn't get a response from [nodeID]
	ss.failedSeeders.Add(nodeID)
	ss.pendingSeeders.Remove(nodeID)

	return ss.receivedStateSummaryFrontier(ctx)
}

func (ss *stateSyncer) receivedStateSummaryFrontier(ctx context.Context) error {
	ss.sendGetStateSummaryFrontiers(ctx)

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
	frontiersTotalWeight, err := ss.frontierSeeders.TotalWeight(ss.Ctx.SubnetID)
	if err != nil {
		return fmt.Errorf("failed to get total weight of frontier seeders for subnet %s: %w", ss.Ctx.SubnetID, err)
	}
	beaconsTotalWeight, err := ss.StateSyncBeacons.TotalWeight(ss.Ctx.SubnetID)
	if err != nil {
		return fmt.Errorf("failed to get total weight of state sync beacons for subnet %s: %w", ss.Ctx.SubnetID, err)
	}
	frontierAlpha := float64(frontiersTotalWeight*ss.Alpha) / float64(beaconsTotalWeight)
	failedBeaconWeight, err := ss.StateSyncBeacons.SubsetWeight(ss.Ctx.SubnetID, ss.failedSeeders)
	if err != nil {
		return fmt.Errorf("failed to get total weight of failed beacons: %w", err)
	}

	frontierStake := frontiersTotalWeight - failedBeaconWeight
	if float64(frontierStake) < frontierAlpha {
		ss.Ctx.Log.Debug("restarting state sync",
			zap.String("reason", "didn't receive enough frontiers"),
			zap.Int("numFailedValidators", ss.failedSeeders.Len()),
		)
		return ss.startup(ctx)
	}

	ss.requestID++
	ss.sendGetAcceptedStateSummaries(ctx)
	return nil
}

func (ss *stateSyncer) AcceptedStateSummary(ctx context.Context, nodeID ids.NodeID, requestID uint32, summaryIDs set.Set[ids.ID]) error {
	// ignores any late responses
	if requestID != ss.requestID {
		ss.Ctx.Log.Debug("received out-of-sync AcceptedStateSummary message",
			zap.Stringer("nodeID", nodeID),
			zap.Uint32("expectedRequestID", ss.requestID),
			zap.Uint32("requestID", requestID),
		)
		return nil
	}

	if !ss.pendingVoters.Contains(nodeID) {
		ss.Ctx.Log.Debug("received unexpected AcceptedStateSummary message",
			zap.Stringer("nodeID", nodeID),
		)
		return nil
	}

	// Mark that we received a response from [nodeID]
	ss.pendingVoters.Remove(nodeID)

	nodeWeight := ss.StateSyncBeacons.GetWeight(ss.Ctx.SubnetID, nodeID)
	ss.Ctx.Log.Debug("adding weight to summaries",
		zap.Stringer("nodeID", nodeID),
		zap.Stringer("subnetID", ss.Ctx.SubnetID),
		zap.Reflect("summaryIDs", summaryIDs),
		zap.Uint64("nodeWeight", nodeWeight),
	)
	for summaryID := range summaryIDs {
		ws, ok := ss.weightedSummaries[summaryID]
		if !ok {
			ss.Ctx.Log.Debug("skipping summary",
				zap.String("reason", "unknown summary"),
				zap.Stringer("nodeID", nodeID),
				zap.Stringer("summaryID", summaryID),
			)
			continue
		}

		newWeight, err := safemath.Add(nodeWeight, ws.weight)
		if err != nil {
			ss.Ctx.Log.Error("failed to calculate new summary weight",
				zap.Stringer("nodeID", nodeID),
				zap.Stringer("summaryID", summaryID),
				zap.Uint64("height", ws.summary.Height()),
				zap.Uint64("nodeWeight", nodeWeight),
				zap.Uint64("previousWeight", ws.weight),
				zap.Error(err),
			)
			newWeight = math.MaxUint64
		}

		ss.Ctx.Log.Verbo("updating summary weight",
			zap.Stringer("nodeID", nodeID),
			zap.Stringer("summaryID", summaryID),
			zap.Uint64("height", ws.summary.Height()),
			zap.Uint64("previousWeight", ws.weight),
			zap.Uint64("newWeight", newWeight),
		)
		ws.weight = newWeight
	}

	ss.sendGetAcceptedStateSummaries(ctx)

	// wait on pending responses
	if ss.pendingVoters.Len() != 0 {
		return nil
	}

	// We've received the filtered accepted frontier from every state sync validator
	// Drop all summaries without a sufficient weight behind them
	for summaryID, ws := range ss.weightedSummaries {
		if ws.weight < ss.Alpha {
			ss.Ctx.Log.Debug("removing summary",
				zap.String("reason", "insufficient weight"),
				zap.Stringer("summaryID", summaryID),
				zap.Uint64("height", ws.summary.Height()),
				zap.Uint64("currentWeight", ws.weight),
				zap.Uint64("requiredWeight", ss.Alpha),
			)
			delete(ss.weightedSummaries, summaryID)
		}
	}

	// if we don't have enough weight for the state summary to be accepted then retry or fail the state sync
	size := len(ss.weightedSummaries)
	if size == 0 {
		// retry the state sync if the weight is not enough to state sync
		failedVotersWeight, err := ss.StateSyncBeacons.SubsetWeight(ss.Ctx.SubnetID, ss.failedVoters)
		if err != nil {
			return fmt.Errorf("failed to get total weight of failed voters: %w", err)
		}

		// if we had too many timeouts when asking for validator votes, we should restart
		// state sync hoping for the network problems to go away; otherwise, we received
		// enough (>= ss.Alpha) responses, but no state summary was supported by a majority
		// of validators (i.e. votes are split between minorities supporting different state
		// summaries), so there is no point in retrying state sync; we should move ahead to bootstrapping
		beaconsTotalWeight, err := ss.StateSyncBeacons.TotalWeight(ss.Ctx.SubnetID)
		if err != nil {
			return fmt.Errorf("failed to get total weight of state sync beacons for subnet %s: %w", ss.Ctx.SubnetID, err)
		}
		votingStakes := beaconsTotalWeight - failedVotersWeight
		if votingStakes < ss.Alpha {
			ss.Ctx.Log.Debug("restarting state sync",
				zap.String("reason", "not enough votes received"),
				zap.Int("numBeacons", ss.StateSyncBeacons.NumValidators(ss.Ctx.SubnetID)),
				zap.Int("numFailedSyncers", ss.failedVoters.Len()),
			)
			return ss.startup(ctx)
		}

		ss.Ctx.Log.Info("skipping state sync",
			zap.String("reason", "no acceptable summaries found"),
		)

		// if we do not restart state sync, move on to bootstrapping.
		return ss.onDoneStateSyncing(ctx, ss.requestID)
	}

	preferredStateSummary := ss.selectSyncableStateSummary()
	syncMode, err := preferredStateSummary.Accept(ctx)
	if err != nil {
		return err
	}

	ss.Ctx.Log.Info("accepted state summary",
		zap.Stringer("summaryID", preferredStateSummary.ID()),
		zap.Stringer("syncMode", syncMode),
		zap.Int("numTotalSummaries", size),
	)

	switch syncMode {
	case block.StateSyncSkipped:
		// VM did not accept the summary, move on to bootstrapping.
		return ss.onDoneStateSyncing(ctx, ss.requestID)
	case block.StateSyncStatic:
		// Summary was accepted and VM is state syncing.
		// Engine will wait for notification of state sync done.
		ss.Ctx.StateSyncing.Set(true)
		return nil
	case block.StateSyncDynamic:
		// Summary was accepted and VM is state syncing.
		// Engine will continue into bootstrapping and the VM will sync in the
		// background.
		ss.Ctx.StateSyncing.Set(true)
		return ss.onDoneStateSyncing(ctx, ss.requestID)
	default:
		ss.Ctx.Log.Warn("unhandled state summary mode, proceeding to bootstrap",
			zap.Stringer("syncMode", syncMode),
		)
		return ss.onDoneStateSyncing(ctx, ss.requestID)
	}
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

func (ss *stateSyncer) GetAcceptedStateSummaryFailed(ctx context.Context, nodeID ids.NodeID, requestID uint32) error {
	// ignores any late responses
	if requestID != ss.requestID {
		ss.Ctx.Log.Debug("received out-of-sync GetAcceptedStateSummaryFailed message",
			zap.Stringer("nodeID", nodeID),
			zap.Uint32("expectedRequestID", ss.requestID),
			zap.Uint32("requestID", requestID),
		)
		return nil
	}

	// If we can't get a response from [nodeID], act as though they said that
	// they think none of the containers we sent them in GetAccepted are
	// accepted
	ss.failedVoters.Add(nodeID)

	return ss.AcceptedStateSummary(ctx, nodeID, requestID, nil)
}

// startup do start the whole state sync process by
// sampling frontier seeders, listing state syncers to request votes to
// and reaching out frontier seeders if any. Otherwise, it moves immediately
// to bootstrapping. Unlike Start, startup does not check
// whether sufficient stake amount is connected.
func (ss *stateSyncer) startup(ctx context.Context) error {
	ss.Config.Ctx.Log.Info("starting state sync")

	// clear up messages trackers
	ss.weightedSummaries = make(map[ids.ID]*weightedSummary)
	ss.summariesHeights.Clear()
	ss.uniqueSummariesHeights = nil

	ss.targetSeeders.Clear()
	ss.pendingSeeders.Clear()
	ss.failedSeeders.Clear()
	ss.targetVoters.Clear()
	ss.pendingVoters.Clear()
	ss.failedVoters.Clear()

	// sample K beacons to retrieve frontier from
	beaconIDs, err := ss.StateSyncBeacons.Sample(ss.Ctx.SubnetID, ss.Config.SampleK)
	if err != nil {
		return err
	}

	ss.frontierSeeders = validators.NewManager()
	for _, nodeID := range beaconIDs {
		if _, ok := ss.frontierSeeders.GetValidator(ss.Ctx.SubnetID, nodeID); !ok {
			// Invariant: We never use the TxID or BLS keys populated here.
			err = ss.frontierSeeders.AddStaker(ss.Ctx.SubnetID, nodeID, nil, ids.Empty, 1)
		} else {
			err = ss.frontierSeeders.AddWeight(ss.Ctx.SubnetID, nodeID, 1)
		}
		if err != nil {
			return err
		}
		ss.targetSeeders.Add(nodeID)
	}

	// list all beacons, to reach them for voting on frontier
	ss.targetVoters.Add(ss.StateSyncBeacons.GetValidatorIDs(ss.Ctx.SubnetID)...)

	// check if there is an ongoing state sync; if so add its state summary
	// to the frontier to request votes on
	// Note: database.ErrNotFound means there is no ongoing summary
	localSummary, err := ss.stateSyncVM.GetOngoingSyncStateSummary(ctx)
	switch err {
	case database.ErrNotFound:
		// no action needed
	case nil:
		ss.locallyAvailableSummary = localSummary
		ss.weightedSummaries[localSummary.ID()] = &weightedSummary{
			summary: localSummary,
		}

		height := localSummary.Height()
		ss.summariesHeights.Add(height)
		ss.uniqueSummariesHeights = append(ss.uniqueSummariesHeights, height)
	default:
		return err
	}

	// initiate messages exchange
	if ss.targetSeeders.Len() == 0 {
		ss.Ctx.Log.Info("State syncing skipped due to no provided syncers")
		return ss.onDoneStateSyncing(ctx, ss.requestID)
	}

	ss.requestID++
	ss.sendGetStateSummaryFrontiers(ctx)
	return nil
}

// Ask up to [common.MaxOutstandingBroadcastRequests] state sync validators at a time
// to send their accepted state summary. It is called again until there are
// no more seeders to be reached in the pending set
func (ss *stateSyncer) sendGetStateSummaryFrontiers(ctx context.Context) {
	vdrs := set.NewSet[ids.NodeID](1)
	for ss.targetSeeders.Len() > 0 && ss.pendingSeeders.Len() < maxOutstandingBroadcastRequests {
		vdr, _ := ss.targetSeeders.Pop()
		vdrs.Add(vdr)
		ss.pendingSeeders.Add(vdr)
	}

	if vdrs.Len() > 0 {
		ss.Sender.SendGetStateSummaryFrontier(ctx, vdrs, ss.requestID)
	}
}

// Ask up to [common.MaxOutstandingStateSyncRequests] syncers validators to send
// their filtered accepted frontier. It is called again until there are
// no more voters to be reached in the pending set.
func (ss *stateSyncer) sendGetAcceptedStateSummaries(ctx context.Context) {
	vdrs := set.NewSet[ids.NodeID](1)
	for ss.targetVoters.Len() > 0 && ss.pendingVoters.Len() < maxOutstandingBroadcastRequests {
		vdr, _ := ss.targetVoters.Pop()
		vdrs.Add(vdr)
		ss.pendingVoters.Add(vdr)
	}

	if len(vdrs) > 0 {
		ss.Sender.SendGetAcceptedStateSummary(ctx, vdrs, ss.requestID, ss.uniqueSummariesHeights)
		ss.Ctx.Log.Debug("sent GetAcceptedStateSummary messages",
			zap.Int("numSent", vdrs.Len()),
			zap.Int("numPending", ss.targetVoters.Len()),
		)
	}
}

func (ss *stateSyncer) Notify(ctx context.Context, msg common.Message) error {
	if msg != common.StateSyncDone {
		ss.Ctx.Log.Info("received an unexpected message from the VM",
			zap.Stringer("msg", msg),
		)
		return nil
	}

	ss.Ctx.StateSyncing.Set(false)
	return ss.onDoneStateSyncing(ctx, ss.requestID)
}

func (*stateSyncer) Gossip(context.Context) error {
	return nil
}

func (ss *stateSyncer) Shutdown(ctx context.Context) error {
	ss.Config.Ctx.Log.Info("shutting down state syncer")

	ss.Ctx.Lock.Lock()
	defer ss.Ctx.Lock.Unlock()

	return ss.VM.Shutdown(ctx)
}

func (*stateSyncer) Timeout(context.Context) error {
	return nil
}

func (ss *stateSyncer) HealthCheck(ctx context.Context) (interface{}, error) {
	ss.Ctx.Lock.Lock()
	defer ss.Ctx.Lock.Unlock()

	vmIntf, vmErr := ss.VM.HealthCheck(ctx)
	intf := map[string]interface{}{
		"consensus": struct{}{},
		"vm":        vmIntf,
	}
	return intf, vmErr
}

func (ss *stateSyncer) IsEnabled(ctx context.Context) (bool, error) {
	if ss.stateSyncVM == nil {
		// state sync is not implemented
		return false, nil
	}

	ss.Ctx.Lock.Lock()
	defer ss.Ctx.Lock.Unlock()

	return ss.stateSyncVM.StateSyncEnabled(ctx)
}
