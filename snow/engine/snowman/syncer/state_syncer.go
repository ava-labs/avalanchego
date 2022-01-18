// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package syncer

import (
	"fmt"
	stdmath "math"
	"math/rand"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/version"
)

const (
	// MaxOutstandingStateSyncRequests is the maximum number of
	// messages sent but not responded to/failed for each relevant message type
	MaxOutstandingStateSyncRequests = 50
)

var _ common.StateSyncer = &stateSyncer{}

func New(
	cfg Config,
	onDoneStateSyncing func(lastReqID uint32) error,
) common.StateSyncer {
	ssVM, _ := cfg.VM.(block.StateSyncableVM)
	gR := common.NewGearRequester(
		cfg.Ctx.Log,
		[]message.Op{
			message.StateSummaryFrontier,
			message.AcceptedStateSummary,
		})

	fs := &stateSyncer{
		Config:                  cfg,
		AcceptedFrontierHandler: common.NewNoOpAcceptedFrontierHandler(cfg.Ctx.Log),
		AcceptedHandler:         common.NewNoOpAcceptedHandler(cfg.Ctx.Log),
		AncestorsHandler:        common.NewNoOpAncestorsHandler(cfg.Ctx.Log),
		PutHandler:              common.NewNoOpPutHandler(cfg.Ctx.Log),
		QueryHandler:            common.NewNoOpQueryHandler(cfg.Ctx.Log),
		ChitsHandler:            common.NewNoOpChitsHandler(cfg.Ctx.Log),
		AppHandler:              common.NewNoOpAppHandler(cfg.Ctx.Log),
		gR:                      gR,
		stateSyncVM:             ssVM,
		onDoneStateSyncing:      onDoneStateSyncing,
	}

	return fs
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

	gR common.GearRequester

	started bool

	// Tracks the last requestID that was used in a request
	RequestID uint32

	// Holds the beacons that were sampled for the accepted frontier
	sampledBeacons validators.Set
	// IDs of all the returned accepted frontiers
	acceptedFrontierSet map[string][]byte
	// IDs of the returned accepted containers and the stake weight that has
	// marked them as accepted
	acceptedVotes map[string]uint64
	acceptedKeys  [][]byte

	// True if RestartBootstrap has been called at least once
	Restarted bool

	// number of times the state sync has been attempted
	attempts int

	// State Sync specific fields
	stateSyncVM        block.StateSyncableVM
	onDoneStateSyncing func(lastReqID uint32) error
	lastSummaryBlkID   ids.ID
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

// Engine interface implementation
func (ss *stateSyncer) Start(startReqID uint32) error {
	ss.RequestID = startReqID
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
	ss.gR.ClearToRequest(message.StateSummaryFrontier)
	ss.gR.ClearRequested(message.StateSummaryFrontier)
	ss.gR.ClearFailed(message.StateSummaryFrontier)
	ss.acceptedFrontierSet = make(map[string][]byte)

	ss.gR.ClearToRequest(message.AcceptedStateSummary)
	ss.gR.ClearRequested(message.AcceptedStateSummary)
	ss.gR.ClearFailed(message.AcceptedStateSummary)
	ss.acceptedVotes = make(map[string]uint64)

	// set beacons
	if len(ss.StateSyncTestingBeacons) != 0 {
		// if StateSyncTestingBeacons are specified, frontiers from these nodes only will be pulled
		// and and pass them to VM. No voting rounds for these frontiers; just wait to have them all
		// and pass them to VM
		for _, vdrID := range ss.StateSyncTestingBeacons {
			if err := ss.gR.PushToRequest(message.StateSummaryFrontier, vdrID); err != nil {
				return err
			}
		}
	} else {
		beacons, err := ss.Beacons.Sample(ss.Config.SampleK)
		if err != nil {
			return err
		}

		ss.sampledBeacons = validators.NewSet()
		err = ss.sampledBeacons.Set(beacons)
		if err != nil {
			return err
		}

		for _, vdr := range beacons {
			vdrID := vdr.ID()
			if err := ss.gR.PushToRequest(message.StateSummaryFrontier, vdrID); err != nil {
				return err
			}
		}

		for _, vdr := range ss.Beacons.List() {
			vdrID := vdr.ID()
			if err := ss.gR.PushToRequest(message.AcceptedStateSummary, vdrID); err != nil {
				return err
			}
		}

		// initiate messages exchange
		ss.attempts++
		if !ss.gR.HasToRequest(message.StateSummaryFrontier) {
			ss.Ctx.Log.Info("State syncing skipped due to no provided bootstraps")
			return ss.stateSyncVM.StateSync(nil)
		}
	}

	ss.RequestID++
	return ss.sendGetStateSummaryFrontiers()
}

func (ss *stateSyncer) restartBootstrap(reset bool) error {
	// resets the attempts when we're pulling blocks/vertices we don't want to
	// fail the bootstrap at that stage
	if reset {
		ss.Ctx.Log.Debug("Checking for new state sync frontiers")

		ss.Restarted = true
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
	frontiersToRequest := MaxOutstandingStateSyncRequests - ss.gR.CountRequested(message.StateSummaryFrontier)
	vdrsList := ss.gR.PopToRequest(message.StateSummaryFrontier, frontiersToRequest)
	if err := ss.gR.RecordRequested(message.StateSummaryFrontier, vdrsList); err != nil {
		return err
	}

	vdrs := ids.NewShortSet(1)
	vdrs.Add(vdrsList...)
	if vdrs.Len() > 0 {
		ss.Sender.SendGetStateSummaryFrontier(vdrs, ss.RequestID)
	}
	return nil
}

// Ask up to [MaxOutstandingStateSyncRequests] bootstrap validators to send
// their filtered accepted frontier
func (ss *stateSyncer) sendGetAccepted() error {
	acceptedFrontiersToRequest := MaxOutstandingStateSyncRequests - ss.gR.CountRequested(message.AcceptedStateSummary)
	vdrsList := ss.gR.PopToRequest(message.AcceptedStateSummary, acceptedFrontiersToRequest)
	if err := ss.gR.RecordRequested(message.AcceptedStateSummary, vdrsList); err != nil {
		return err
	}

	vdrs := ids.NewShortSet(1)
	vdrs.Add(vdrsList...)
	if vdrs.Len() > 0 {
		ss.Ctx.Log.Debug("sent %d more GetAccepted messages with %d more to send",
			vdrs.Len(), ss.gR.CountRequested(message.AcceptedStateSummary))
		ss.Sender.SendGetAcceptedStateSummary(vdrs, ss.RequestID, ss.acceptedKeys)
	}
	return nil
}

// StateSyncHandler interface implementation
func (ss *stateSyncer) StateSummaryFrontier(validatorID ids.ShortID, requestID uint32, key, summary []byte) error {
	// ignores any late responses
	if requestID != ss.RequestID {
		ss.Ctx.Log.Debug("Received an Out-of-Sync AcceptedFrontier - validator: %v - expectedRequestID: %v, requestID: %v",
			validatorID, ss.RequestID, requestID)
		return nil
	}
	if !ss.gR.ConsumeRequested(message.StateSummaryFrontier, validatorID) {
		return nil
	}
	ss.acceptedFrontierSet[string(key)] = summary

	if len(ss.StateSyncTestingBeacons) != 0 {
		// No voting rounds for these frontiers. Just download them from specified beacons
		if ss.gR.CountRequested(message.StateSummaryFrontier) != 0 {
			return nil
		}

		// received what we needed. Just pass to VM
		accepted := make([]common.Summary, 0, len(ss.acceptedFrontierSet))
		for k, v := range ss.acceptedFrontierSet {
			accepted = append(accepted, common.Summary{
				Key:     []byte(k),
				Content: v,
			})
		}

		ss.Ctx.Log.Info("Received state summaries frontiers from all listed nodes. Starting state sync skipping voting rounds.")
		return ss.stateSyncVM.StateSync(accepted)
	}

	if err := ss.sendGetStateSummaryFrontiers(); err != nil {
		return err
	}

	// still waiting on requests
	if ss.gR.CountRequested(message.StateSummaryFrontier) != 0 {
		return nil
	}

	// We've received the accepted frontier from every bootstrap validator
	// Ask each bootstrap validator to filter the list of containers that we were
	// told are on the accepted frontier such that the list only contains containers
	// they think are accepted
	var err error

	// Create a newAlpha taking using the sampled beacon
	// Keep the proportion of b.Alpha in the newAlpha
	// newAlpha := totalSampledWeight * b.Alpha / totalWeight

	newAlpha := float64(ss.sampledBeacons.Weight()*ss.Alpha) / float64(ss.Beacons.Weight())

	failedAcceptedFrontier := ss.gR.GetAllFailed(message.StateSummaryFrontier)
	failedBeaconWeight, err := ss.Beacons.SubsetWeight(failedAcceptedFrontier)
	if err != nil {
		return err
	}

	// fail the bootstrap if the weight is not enough to bootstrap
	if float64(ss.sampledBeacons.Weight())-newAlpha < float64(failedBeaconWeight) {
		if ss.Config.RetryBootstrap {
			ss.Ctx.Log.Debug("Not enough frontiers received, restarting state sync... - Beacons: %d - Failed Bootstrappers: %d "+
				"- state sync attempt: %d", ss.Beacons.Len(), failedAcceptedFrontier.Len(), ss.attempts)
			return ss.restartBootstrap(false)
		}

		ss.Ctx.Log.Debug("Didn't receive enough frontiers - failed validators: %d, "+
			"state sync attempt: %d", failedAcceptedFrontier.Len(), ss.attempts)
	}

	ss.RequestID++
	acceptedFrontierList := make([][]byte, 0)
	for _, acceptedFrontier := range ss.acceptedFrontierSet {
		acceptedFrontierList = append(acceptedFrontierList, acceptedFrontier)
	}
	ss.acceptedKeys = acceptedFrontierList

	return ss.sendGetAccepted()
}

// StateSyncHandler interface implementation
func (ss *stateSyncer) AcceptedStateSummary(validatorID ids.ShortID, requestID uint32, keys [][]byte) error {
	// ignores any late responses
	if requestID != ss.RequestID {
		ss.Ctx.Log.Debug("Received an Out-of-Sync Accepted - validator: %v - expectedRequestID: %v, requestID: %v",
			validatorID, ss.RequestID, requestID)
		return nil
	}

	if !ss.gR.ConsumeRequested(message.AcceptedStateSummary, validatorID) {
		return nil
	}

	weight := uint64(0)
	if w, ok := ss.Beacons.GetWeight(validatorID); ok {
		weight = w
	}

	for _, key := range keys {
		previousWeight := ss.acceptedVotes[string(key)]
		newWeight, err := math.Add64(weight, previousWeight)
		if err != nil {
			ss.Ctx.Log.Error("Error calculating the Accepted votes - weight: %v, previousWeight: %v", weight, previousWeight)
			newWeight = stdmath.MaxUint64
		}
		ss.acceptedVotes[string(key)] = newWeight
	}

	if err := ss.sendGetAccepted(); err != nil {
		return err
	}

	// wait on pending responses
	if ss.gR.CountRequested(message.AcceptedStateSummary) != 0 {
		return nil
	}

	// We've received the filtered accepted frontier from every bootstrap validator
	// Accept all containers that have a sufficient weight behind them
	accepted := make([]common.Summary, 0, len(ss.acceptedVotes))
	for key, weight := range ss.acceptedVotes {
		if weight >= ss.Alpha {
			accepted = append(accepted, common.Summary{
				Key:     []byte(key),
				Content: ss.acceptedFrontierSet[key],
			})
		}
	}

	// if we don't have enough weight for the state summary to be accepted then retry or fail the state sync
	size := len(accepted)
	failedAccepted := ss.gR.GetAllFailed(message.AcceptedStateSummary)
	if size == 0 && ss.Beacons.Len() > 0 {
		// retry the bootstrap if the weight is not enough to bootstrap
		failedBeaconWeight, err := ss.Beacons.SubsetWeight(failedAccepted)
		if err != nil {
			return err
		}

		// in a zero network there will be no accepted votes but the voting weight will be greater than the failed weight
		if ss.Config.RetryBootstrap && ss.Beacons.Weight()-ss.Alpha < failedBeaconWeight {
			ss.Ctx.Log.Debug("Not enough votes received, restarting state sync... - Beacons: %d - Failed Bootstrappers: %d "+
				"- state sync attempt: %d", ss.Beacons.Len(), failedAccepted.Len(), ss.attempts)
			return ss.restartBootstrap(false)
		}
	}

	if !ss.Restarted {
		ss.Ctx.Log.Info("State sync started syncing with %d vertices in the accepted frontier", size)
	} else {
		ss.Ctx.Log.Debug("State sync started syncing with %d vertices in the accepted frontier", size)
	}

	return ss.stateSyncVM.StateSync(accepted)
}

// StateSyncHandler interface implementation
func (ss *stateSyncer) GetAcceptedStateSummaryFailed(validatorID ids.ShortID, requestID uint32) error {
	// ignores any late responses
	if requestID != ss.RequestID {
		ss.Ctx.Log.Debug("Received an Out-of-Sync GetAcceptedStateSummaryFailed - validator: %v - expectedRequestID: %v, requestID: %v",
			validatorID, ss.RequestID, requestID)
		return nil
	}

	if len(ss.StateSyncTestingBeacons) != 0 {
		// we are not able to obtain summaries from all StateSyncTestingBeacons; returning fatal error
		return fmt.Errorf("failed downloading summaries from StateSyncTestingBeacons")
	}

	if err := ss.gR.AddFailed(message.AcceptedStateSummary, validatorID); err != nil {
		return err
	}

	return ss.AcceptedStateSummary(validatorID, requestID, [][]byte{})
}

// StateSyncHandler interface implementation
func (ss *stateSyncer) GetStateSummaryFrontierFailed(validatorID ids.ShortID, requestID uint32) error {
	// ignores any late responses
	if requestID != ss.RequestID {
		ss.Ctx.Log.Debug("Received an Out-of-Sync GetStateSummaryFrontierFailed - validator: %v - expectedRequestID: %v, requestID: %v",
			validatorID, ss.RequestID, requestID)
		return nil
	}

	if err := ss.gR.AddFailed(message.StateSummaryFrontier, validatorID); err != nil {
		return err
	}

	return ss.StateSummaryFrontier(validatorID, requestID, []byte{}, []byte{})
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

	case common.StateSyncLastBlockMissing:
		// retrieve the blkID to request
		var err error
		ss.lastSummaryBlkID, err = ss.stateSyncVM.GetLastSummaryBlockID()
		if err != nil {
			ss.Ctx.Log.Warn("Could not retrieve last summary block ID to complete state sync. Err: %v", err)
			return err
		}
		return ss.requestBlk(ss.lastSummaryBlkID)

	case common.StateSyncDone:
		return ss.onDoneStateSyncing(ss.RequestID)

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
	ss.Sender.SendGet(valID, ss.RequestID, blkID)
	return nil
}

// FetchHandler interface implementation
func (ss *stateSyncer) Put(validatorID ids.ShortID, requestID uint32, container []byte) error {
	// ignores any late responses
	if requestID != ss.RequestID {
		ss.Ctx.Log.Debug("Received an Out-of-Sync Put - validator: %v - expectedRequestID: %v, requestID: %v",
			validatorID, ss.RequestID, requestID)
		return nil
	}

	if err := ss.stateSyncVM.SetLastSummaryBlock(container); err != nil {
		ss.Ctx.Log.Warn("Could not accept last summary block, err :%v. Retrying block download.", err)
		return ss.requestBlk(ss.lastSummaryBlkID)
	}

	return nil
}

func (ss *stateSyncer) GetFailed(validatorID ids.ShortID, requestID uint32) error {
	// ignores any late responses
	if requestID != ss.RequestID {
		ss.Ctx.Log.Debug("Received an Out-of-Sync GetFailed - validator: %v - expectedRequestID: %v, requestID: %v",
			validatorID, ss.RequestID, requestID)
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
