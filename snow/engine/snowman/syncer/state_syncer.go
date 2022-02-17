// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package syncer

import (
	"fmt"
	"math/rand"
	"sort"
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

// We want to order summaries passed to VM by weight,
// as a proxy of summary data availability
type summaryWeightedList []weightedSummary

func (swl summaryWeightedList) Len() int           { return len(swl) }
func (swl summaryWeightedList) Less(i, j int) bool { return swl[i].weight < swl[j].weight }
func (swl summaryWeightedList) Swap(i, j int)      { swl[i], swl[j] = swl[j], swl[i] }
func (swl summaryWeightedList) FilterAbove(minWeight uint64) []common.Summary {
	res := make([]common.Summary, 0, len(swl))
	for _, s := range swl {
		if s.weight < minWeight {
			continue
		}
		res = append(res, common.Summary{
			Key:     s.Key,
			Content: s.Content,
		})
	}
	return res
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
	// True if restart has been called at least once
	restarted bool
	// Tracks the last requestID that was used in a request
	requestID uint32

	// State Sync specific fields
	stateSyncVM        block.StateSyncableVM
	onDoneStateSyncing func(lastReqID uint32) error
	lastSummaryBlkID   ids.ID

	// Holds the beacons that were sampled for the accepted frontier
	sampledBeacons validators.Set
	// IDs of validators we should request state summary frontier from
	pendingSendStateSummaryFrontier ids.ShortSet
	// IDs of validators we requested a state summary frontier from
	// but haven't received a reply yet
	pendingReceiveStateSummaryFrontier ids.ShortSet
	// IDs of validators that failed to respond with their state summary frontier
	failedStateSummaryFrontier ids.ShortSet

	// IDs of validators we should request filtering the accepted state summaries from
	pendingSendAcceptedStateSummaries ids.ShortSet
	// IDs of validators we requested filtering the accepted state summaries from
	// but haven't received a reply yet
	pendingReceiveAcceptedStateSummaries ids.ShortSet
	// IDs of validators that failed to respond with their filtered accepted state summaries
	failedAcceptedStateSummaries ids.ShortSet

	weightedSummaries map[string]weightedSummary // key --> (summary, weight)

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
func (ss *stateSyncer) StateSummaryFrontier(validatorID ids.ShortID, requestID uint32, key, summary []byte) error {
	// ignores any late responses
	if requestID != ss.requestID {
		ss.Ctx.Log.Debug("Received an Out-of-Sync StateSummaryFrontier - validator: %v - expectedRequestID: %v, requestID: %v",
			validatorID, ss.requestID, requestID)
		return nil
	}

	if !ss.pendingReceiveStateSummaryFrontier.Contains(validatorID) {
		ss.Ctx.Log.Debug("Received a StateSummaryFrontier message from %s unexpectedly", validatorID)
		return nil
	}

	// Mark that we received a response from [validatorID]
	ss.pendingReceiveStateSummaryFrontier.Remove(validatorID)
	ws, exists := ss.weightedSummaries[string(key)]
	if !exists {
		ss.weightedSummaries[string(key)] = weightedSummary{
			Summary: common.Summary{
				Key:     key,
				Content: summary,
			},
		}
	}

	if len(ss.StateSyncTestingBeacons) != 0 {
		// if state sync beacons are specified, immediately count
		// their frontier as voted by them, as no network wide vote
		// will be held
		weight := uint64(0)
		if w, ok := ss.Beacons.GetWeight(validatorID); ok {
			weight = w
		}

		previousWeight := ws.weight
		newWeight, err := math.Add64(weight, previousWeight)
		if err != nil {
			ss.Ctx.Log.Error("Error calculating the Accepted votes - weight: %v, previousWeight: %v", weight, previousWeight)
			newWeight = stdmath.MaxUint64
		}
		ws.weight = newWeight
		ss.weightedSummaries[string(key)] = ws
	}

	if err := ss.sendGetStateSummaryFrontiers(); err != nil {
		return err
	}

	// still waiting on requests
	if ss.pendingReceiveStateSummaryFrontier.Len() != 0 {
		return nil
	}

	if len(ss.StateSyncTestingBeacons) != 0 {
		// received what we needed. Just pass to VM, ordered by decreasing weight
		summaries := make(summaryWeightedList, 0, len(ss.weightedSummaries))
		for _, ws := range ss.weightedSummaries {
			summaries = append(summaries, ws)
		}
		sort.Sort(sort.Reverse(summaries))
		// if state sync beacons are specified, we keep any summary frontier
		// a beacon has sent. No filtering out of low votes summaries.
		accepted := summaries.FilterAbove(0)

		ss.Ctx.Log.Info("Received (%d) state summaries frontiers from all listed nodes. Starting state sync skipping voting rounds.", len(accepted))
		return ss.stateSyncVM.StateSync(accepted)
	}

	// We've received the accepted frontier from every state syncer
	// Ask each state syncer to filter the list of containers that we were
	// told are on the accepted frontier such that the list only contains containers
	// they think are accepted
	var err error

	// Create a newAlpha taking using the sampled beacon
	// Keep the proportion of b.Alpha in the newAlpha
	// newAlpha := totalSampledWeight * b.Alpha / totalWeight

	newAlpha := float64(ss.sampledBeacons.Weight()*ss.Alpha) / float64(ss.Beacons.Weight())

	failedBeaconWeight, err := ss.Beacons.SubsetWeight(ss.failedStateSummaryFrontier)
	if err != nil {
		return err
	}

	// fail the fast sync if the weight is not enough to fast sync
	if float64(ss.sampledBeacons.Weight())-newAlpha < float64(failedBeaconWeight) {
		if ss.Config.RetryBootstrap {
			ss.Ctx.Log.Debug("Not enough frontiers received, restarting state sync... - Beacons: %d - Failed Bootstrappers: %d "+
				"- state sync attempt: %d", ss.Beacons.Len(), ss.failedStateSummaryFrontier.Len(), ss.attempts)
			return ss.restart(false)
		}

		ss.Ctx.Log.Debug("Didn't receive enough frontiers - failed validators: %d, "+
			"state sync attempt: %d", ss.failedStateSummaryFrontier.Len(), ss.attempts)
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
	ss.failedStateSummaryFrontier.Add(validatorID)
	return ss.StateSummaryFrontier(validatorID, requestID, []byte{}, []byte{})
}

// StateSyncHandler interface implementation
func (ss *stateSyncer) AcceptedStateSummary(validatorID ids.ShortID, requestID uint32, keys [][]byte) error {
	// ignores any late responses
	if requestID != ss.requestID {
		ss.Ctx.Log.Debug("Received an Out-of-Sync Accepted - validator: %v - expectedRequestID: %v, requestID: %v",
			validatorID, ss.requestID, requestID)
		return nil
	}

	if !ss.pendingReceiveAcceptedStateSummaries.Contains(validatorID) {
		ss.Ctx.Log.Debug("Received an AcceptedStateSummary message from %s unexpectedly", validatorID)
		return nil
	}
	// Mark that we received a response from [validatorID]
	ss.pendingReceiveAcceptedStateSummaries.Remove(validatorID)

	weight := uint64(0)
	if w, ok := ss.Beacons.GetWeight(validatorID); ok {
		weight = w
	}

	for _, key := range keys {
		ws := ss.weightedSummaries[string(key)]
		previousWeight := ws.weight
		newWeight, err := math.Add64(weight, previousWeight)
		if err != nil {
			ss.Ctx.Log.Error("Error calculating the Accepted votes - weight: %v, previousWeight: %v", weight, previousWeight)
			newWeight = stdmath.MaxUint64
		}
		ws.weight = newWeight
		ss.weightedSummaries[string(key)] = ws
	}

	if err := ss.sendGetAccepted(); err != nil {
		return err
	}

	// wait on pending responses
	if ss.pendingReceiveAcceptedStateSummaries.Len() != 0 {
		return nil
	}

	// We've received the filtered accepted frontier from every state sync validator
	// Accept all containers that have a sufficient weight behind them
	summaries := make(summaryWeightedList, len(ss.weightedSummaries))
	for _, ws := range ss.weightedSummaries {
		summaries = append(summaries, ws)
	}
	sort.Sort(sort.Reverse(summaries))
	accepted := summaries.FilterAbove(ss.Alpha)

	// if we don't have enough weight for the state summary to be accepted then retry or fail the state sync
	size := len(accepted)
	if size == 0 && ss.Beacons.Len() > 0 {
		// retry the fast sync if the weight is not enough to fast sync
		failedBeaconWeight, err := ss.Beacons.SubsetWeight(ss.failedAcceptedStateSummaries)
		if err != nil {
			return err
		}

		// in a zero network there will be no accepted votes but the voting weight will be greater than the failed weight
		if ss.Config.RetryBootstrap && ss.Beacons.Weight()-ss.Alpha < failedBeaconWeight {
			ss.Ctx.Log.Debug("Not enough votes received, restarting state sync... - Beacons: %d - Failed syncer: %d "+
				"- state sync attempt: %d", ss.Beacons.Len(), ss.failedAcceptedStateSummaries.Len(), ss.attempts)
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

	if len(ss.StateSyncTestingBeacons) != 0 {
		// we are not able to obtain summaries from all StateSyncTestingBeacons; returning fatal error
		return fmt.Errorf("failed downloading summaries from StateSyncTestingBeacons")
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

	// if StateSyncTestingBeacons are specified, VM should direct state-sync related messages
	// to StateSyncTestingBeacons only. So let's inform the VM
	if len(ss.StateSyncTestingBeacons) != 0 {
		if err := ss.stateSyncVM.RegisterStateSyncer(ss.StateSyncTestingBeacons); err != nil {
			return err
		}
	}

	return ss.startup()
}

func (ss *stateSyncer) startup() error {
	ss.Config.Ctx.Log.Info("starting state sync")
	ss.started = true

	// clear up messages tracker
	ss.weightedSummaries = make(map[string]weightedSummary)

	ss.pendingSendStateSummaryFrontier.Clear()
	ss.pendingReceiveStateSummaryFrontier.Clear()
	ss.failedStateSummaryFrontier.Clear()

	ss.pendingSendAcceptedStateSummaries.Clear()
	ss.pendingReceiveAcceptedStateSummaries.Clear()
	ss.failedAcceptedStateSummaries.Clear()

	// set beacons
	if len(ss.StateSyncTestingBeacons) != 0 {
		// if StateSyncTestingBeacons are specified, frontiers from these nodes only will be pulled
		// and and pass them to VM. No voting rounds for these frontiers; just wait to have them all
		// and pass them to VM.
		for _, vdrID := range ss.StateSyncTestingBeacons {
			ss.pendingSendStateSummaryFrontier.Add(vdrID)
		}
	} else {
		beacons, err := ss.Beacons.Sample(ss.Config.SampleK)
		if err != nil {
			return err
		}

		ss.sampledBeacons = validators.NewSet()
		if err = ss.sampledBeacons.Set(beacons); err != nil {
			return err
		}

		for _, vdr := range beacons {
			vdrID := vdr.ID()
			ss.pendingSendStateSummaryFrontier.Add(vdrID)
		}

		for _, vdr := range ss.Beacons.List() {
			vdrID := vdr.ID()
			ss.pendingSendAcceptedStateSummaries.Add(vdrID)
		}

		// initiate messages exchange
		ss.attempts++
		if len(ss.pendingSendStateSummaryFrontier) == 0 {
			ss.Ctx.Log.Info("State syncing skipped due to no provided syncers")
			return ss.stateSyncVM.StateSync(nil)
		}
	}

	ss.requestID++
	return ss.sendGetStateSummaryFrontiers()
}

func (ss *stateSyncer) restart(reset bool) error {
	// resets the attempts when we're pulling blocks/vertices we don't want to
	// fail the state sync at that stage
	if reset {
		ss.Ctx.Log.Debug("Checking for new state sync frontiers")

		ss.restarted = true
		ss.attempts = 0
	}

	if ss.attempts > 0 && ss.attempts%ss.RetryBootstrapWarnFrequency == 0 {
		ss.Ctx.Log.Debug("continuing to attempt to state sync after %d failed attempts. Is this node connected to the internet?",
			ss.attempts)
	}

	return ss.startup()
}

// Ask up to [MaxOutstandingStateSyncRequests] state sync validators to send
// their accepted state summary
func (ss *stateSyncer) sendGetStateSummaryFrontiers() error {
	vdrs := ids.NewShortSet(1)
	for ss.pendingSendStateSummaryFrontier.Len() > 0 && ss.pendingSendStateSummaryFrontier.Len() < maxOutstandingStateSyncRequests {
		vdr, _ := ss.pendingSendStateSummaryFrontier.Pop()
		// Add the validator to the set to send the messages to
		vdrs.Add(vdr)
		// Add the validator to send pending receipt set
		ss.pendingReceiveStateSummaryFrontier.Add(vdr)
	}

	if vdrs.Len() > 0 {
		ss.Sender.SendGetStateSummaryFrontier(vdrs, ss.requestID)
	}
	return nil
}

// Ask up to [MaxOutstandingStateSyncRequests] syncers validators to send
// their filtered accepted frontier
func (ss *stateSyncer) sendGetAccepted() error {
	vdrs := ids.NewShortSet(1)
	for ss.pendingSendAcceptedStateSummaries.Len() > 0 && ss.pendingSendAcceptedStateSummaries.Len() < maxOutstandingStateSyncRequests {
		vdr, _ := ss.pendingSendAcceptedStateSummaries.Pop()
		// Add the validator to the set to send the messages to
		vdrs.Add(vdr)
		// Add the validator to send pending receipt set
		ss.pendingReceiveAcceptedStateSummaries.Add(vdr)
	}

	if vdrs.Len() > 0 {
		ss.Ctx.Log.Debug("sent %d more GetAcceptedStateSummary messages with %d more to send",
			vdrs.Len(), ss.pendingSendAcceptedStateSummaries)

		acceptedKeys := make([][]byte, len(ss.weightedSummaries))
		for k := range ss.weightedSummaries {
			acceptedKeys = append(acceptedKeys, []byte(k))
		}
		ss.Sender.SendGetAcceptedStateSummary(vdrs, ss.requestID, acceptedKeys)
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
	ss.Ctx.Log.AssertTrue(!ss.IsBootstrapped(), "Notify received by StateSync after Bootstrap is done")
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
	var valID ids.ShortID
	if len(ss.StateSyncTestingBeacons) > 0 {
		rndIdx := rand.Intn(len(ss.StateSyncTestingBeacons) - 1) // #nosec G404
		valID = ss.StateSyncTestingBeacons[rndIdx]
	} else {
		// sample from validators
		valList, err := ss.Beacons.Sample(1)
		if err != nil {
			return err
		}
		valID = valList[0].ID()
	}

	// request the block
	ss.Sender.SendGet(valID, ss.requestID, blkID)
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
		if len(ss.StateSyncTestingBeacons) != 0 {
			// if StateSyncTestingBeacons are specified,
			// they are the only validators involved in state sync
			return nil
		}
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
