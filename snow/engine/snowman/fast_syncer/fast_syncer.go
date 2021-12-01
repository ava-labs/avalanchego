// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.
package snowsyncer

import (
	stdmath "math"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/math"
)

const (
	// MaxOutstandingFastSyncRequests is the maximum number of
	// messages sent but not responded to/failed for each relevant message type
	MaxOutstandingFastSyncRequests = 50
)

var _ FastSyncer = &fastSyncer{}

type FastSyncer interface {
	common.Engine
}

func NewFastSyncer(
	cfg Config,
	onDoneFastSyncing func(lastReqID uint32) error,
) FastSyncer {
	fsVM, _ := cfg.VM.(block.StateSyncableVM)
	gR := common.NewGearRequester(
		cfg.Ctx.Log,
		[]message.Op{
			message.StateSummaryFrontier,
			message.AcceptedStateSummary,
		})

	fs := &fastSyncer{
		FastSyncNoOps: FastSyncNoOps{
			Ctx: cfg.Ctx,
		},
		Config:            cfg,
		gR:                gR,
		fastSyncVM:        fsVM,
		onDoneFastSyncing: onDoneFastSyncing,
	}

	return fs
}

type fastSyncer struct {
	FastSyncNoOps
	Config

	gR common.GearRequester

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

	// Fast Sync specific fields
	fastSyncVM        block.StateSyncableVM
	onDoneFastSyncing func(lastReqID uint32) error
}

func (fs *fastSyncer) GetVM() common.VM { return fs.VM }

func (fs *fastSyncer) Notify(msg common.Message) error {
	// if fast sync and bootstrap is done, we shouldn't receive FastSyncDone from the VM
	fs.Ctx.Log.AssertTrue(!fs.IsBootstrapped(), "Notify received by FastSync after Bootstrap is done")
	fs.Ctx.Log.Verbo("snowman engine notified of %s from the vm", msg)
	switch msg {
	case common.PendingTxs:
		fs.Ctx.Log.Warn("Message %s received in fast sync. Dropped.", msg.String())
	case common.FastSyncDone:
		return fs.onDoneFastSyncing(fs.RequestID)
	default:
		fs.Ctx.Log.Warn("unexpected message from the VM: %s", msg)
	}
	return nil
}

// Connected implements the Engine interface.
func (fs *fastSyncer) Connected(nodeID ids.ShortID) error {
	// prevent nodes outside of the specified state sync
	// nodes from connecting to the vm. this is a temporary
	// workaround while we add more handshake information.
	if fs.Beacons.Contains(nodeID) {
		if err := fs.VM.Connected(nodeID); err != nil {
			return err
		}
	}

	if err := fs.Starter.AddWeightForNode(nodeID); err != nil {
		return err
	}

	if fs.Starter.CanStart() {
		fs.Starter.MarkStart()
		return fs.startup()
	}

	return nil
}

// Disconnected implements the Engine interface.
func (fs *fastSyncer) Disconnected(nodeID ids.ShortID) error {
	// temporary workaround, avoid sending disconnect messages
	// for nodes we did not send connect message.
	if fs.Beacons.Contains(nodeID) {
		if err := fs.VM.Disconnected(nodeID); err != nil {
			return err
		}
	}

	return fs.Starter.RemoveWeightForNode(nodeID)
}

func (fs *fastSyncer) Start(startReqID uint32) error {
	fs.RequestID = startReqID
	fs.Ctx.SetState(snow.FastSyncing)

	if fs.fastSyncVM == nil {
		// nothing to do, fast sync is not implemented
		return fs.onDoneFastSyncing(fs.RequestID)
	}

	enabled, err := fs.fastSyncVM.StateSyncEnabled()
	switch {
	case err == block.ErrStateSyncableVMNotImplemented:
		// nothing to do, fast sync is not implemented
		return fs.onDoneFastSyncing(fs.RequestID)
	case err != nil:
		return err
	case !enabled:
		// nothing to do, fast sync is implemented but not enabled
		return fs.onDoneFastSyncing(fs.RequestID)
	}

	return fs.startup()
}

func (fs *fastSyncer) startup() error {
	fs.Config.Ctx.Log.Info("starting fast sync")
	fs.Starter.MarkStart()

	beacons, err := fs.Beacons.Sample(fs.Config.SampleK)
	if err != nil {
		return err
	}

	fs.sampledBeacons = validators.NewSet()
	err = fs.sampledBeacons.Set(beacons)
	if err != nil {
		return err
	}

	fs.gR.ClearToRequest(message.StateSummaryFrontier)
	for _, vdr := range beacons {
		vdrID := vdr.ID()
		if err := fs.gR.PushToRequest(message.StateSummaryFrontier, vdrID); err != nil {
			return err
		}
	}
	fs.gR.ClearRequested(message.StateSummaryFrontier)
	fs.gR.ClearFailed(message.StateSummaryFrontier)
	fs.acceptedFrontierSet = make(map[string][]byte)

	fs.gR.ClearToRequest(message.AcceptedStateSummary)
	for _, vdr := range fs.Beacons.List() {
		vdrID := vdr.ID()
		if err := fs.gR.PushToRequest(message.AcceptedStateSummary, vdrID); err != nil {
			return err
		}
	}

	fs.gR.ClearRequested(message.AcceptedStateSummary)
	fs.gR.ClearFailed(message.AcceptedStateSummary)
	fs.acceptedVotes = make(map[string]uint64)

	fs.attempts++
	if !fs.gR.HasToRequest(message.StateSummaryFrontier) {
		fs.Ctx.Log.Info("Fast syncing skipped due to no provided bootstraps")
		return fs.fastSyncVM.StateSync(nil)
	}

	fs.RequestID++
	return fs.sendGetStateSummaryFrontiers()
}

// Ask up to [MaxOutstandingFastSyncRequests] state sync validators to send
// their accepted state summary
func (fs *fastSyncer) sendGetStateSummaryFrontiers() error {
	validators := ids.NewShortSet(1)

	frontiersToRequest := MaxOutstandingFastSyncRequests - fs.gR.CountRequested(message.StateSummaryFrontier)
	vdrsList := fs.gR.PopToRequest(message.StateSummaryFrontier, frontiersToRequest)
	if err := fs.gR.RecordRequested(message.StateSummaryFrontier, vdrsList); err != nil {
		return err
	}
	validators.Add(vdrsList...)

	if validators.Len() > 0 {
		fs.Sender.SendGetStateSummaryFrontier(validators, fs.RequestID)
	}

	return nil
}

// Ask up to [MaxOutstandingFastSyncRequests] bootstrap validators to send
// their filtered accepted frontier
func (fs *fastSyncer) sendGetAccepted() error {
	vdrs := ids.NewShortSet(1)

	acceptedFrontiersToRequest := MaxOutstandingFastSyncRequests - fs.gR.CountRequested(message.AcceptedStateSummary)
	vdrsList := fs.gR.PopToRequest(message.AcceptedStateSummary, acceptedFrontiersToRequest)
	if err := fs.gR.RecordRequested(message.AcceptedStateSummary, vdrsList); err != nil {
		return err
	}
	vdrs.Add(vdrsList...)

	if vdrs.Len() > 0 {
		fs.Ctx.Log.Debug("sent %d more GetAccepted messages with %d more to send",
			vdrs.Len(), fs.gR.CountRequested(message.AcceptedStateSummary))
		fs.Sender.SendGetAcceptedStateSummary(vdrs, fs.RequestID, fs.acceptedKeys)
	}

	return nil
}

func (fs *fastSyncer) GetStateSummaryFrontier(validatorID ids.ShortID, requestID uint32) error {
	summary, err := fs.fastSyncVM.StateSyncGetLastSummary()
	if err != nil {
		return err
	}
	fs.Sender.SendStateSummaryFrontier(validatorID, requestID, summary.Key, summary.State)
	return nil
}

func (fs *fastSyncer) StateSummaryFrontier(validatorID ids.ShortID, requestID uint32, key, summary []byte) error {
	// ignores any late responses
	if requestID != fs.RequestID {
		fs.Ctx.Log.Debug("Received an Out-of-Sync AcceptedFrontier - validator: %v - expectedRequestID: %v, requestID: %v",
			validatorID, fs.RequestID, requestID)
		return nil
	}

	if !fs.gR.ConsumeRequested(message.StateSummaryFrontier, validatorID) {
		return nil
	}

	fs.acceptedFrontierSet[string(key)] = summary
	if err := fs.sendGetStateSummaryFrontiers(); err != nil {
		return err
	}

	// still waiting on requests
	if fs.gR.CountRequested(message.StateSummaryFrontier) != 0 {
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

	newAlpha := float64(fs.sampledBeacons.Weight()*fs.Alpha) / float64(fs.Beacons.Weight())

	failedAcceptedFrontier := fs.gR.GetAllFailed(message.StateSummaryFrontier)
	failedBeaconWeight, err := fs.Beacons.SubsetWeight(failedAcceptedFrontier)
	if err != nil {
		return err
	}

	// fail the bootstrap if the weight is not enough to bootstrap
	if float64(fs.sampledBeacons.Weight())-newAlpha < float64(failedBeaconWeight) {
		if fs.Config.RetryBootstrap {
			fs.Ctx.Log.Debug("Not enough frontiers received, restarting state sync... - Beacons: %d - Failed Bootstrappers: %d "+
				"- state sync attempt: %d", fs.Beacons.Len(), failedAcceptedFrontier.Len(), fs.attempts)
			return fs.RestartBootstrap(false)
		}

		fs.Ctx.Log.Debug("Didn't receive enough frontiers - failed validators: %d, "+
			"state sync attempt: %d", failedAcceptedFrontier.Len(), fs.attempts)
	}

	fs.RequestID++
	acceptedFrontierList := make([][]byte, 0)
	for _, acceptedFrontier := range fs.acceptedFrontierSet {
		acceptedFrontierList = append(acceptedFrontierList, acceptedFrontier)
	}
	fs.acceptedKeys = acceptedFrontierList

	return fs.sendGetAccepted()
}

func (fs *fastSyncer) GetAcceptedStateSummary(validatorID ids.ShortID, requestID uint32, keys [][]byte) error {
	acceptedKeys := make([][]byte, 0, len(keys))
	for _, key := range keys {
		if accepted, err := fs.fastSyncVM.StateSyncIsSummaryAccepted(key); accepted && err == nil {
			acceptedKeys = append(acceptedKeys, key)
		} else if err != nil {
			return err
		}
	}
	fs.Sender.SendAcceptedStateSummary(validatorID, requestID, acceptedKeys)
	return nil
}

func (fs *fastSyncer) AcceptedStateSummary(validatorID ids.ShortID, requestID uint32, keys [][]byte) error {
	// ignores any late responses
	if requestID != fs.RequestID {
		fs.Ctx.Log.Debug("Received an Out-of-Sync Accepted - validator: %v - expectedRequestID: %v, requestID: %v",
			validatorID, fs.RequestID, requestID)
		return nil
	}

	if !fs.gR.ConsumeRequested(message.AcceptedStateSummary, validatorID) {
		return nil
	}

	weight := uint64(0)
	if w, ok := fs.Beacons.GetWeight(validatorID); ok {
		weight = w
	}

	for _, key := range keys {
		previousWeight := fs.acceptedVotes[string(key)]
		newWeight, err := math.Add64(weight, previousWeight)
		if err != nil {
			fs.Ctx.Log.Error("Error calculating the Accepted votes - weight: %v, previousWeight: %v", weight, previousWeight)
			newWeight = stdmath.MaxUint64
		}
		fs.acceptedVotes[string(key)] = newWeight
	}

	if err := fs.sendGetAccepted(); err != nil {
		return err
	}

	// wait on pending responses
	if fs.gR.CountRequested(message.AcceptedStateSummary) != 0 {
		return nil
	}

	// We've received the filtered accepted frontier from every bootstrap validator
	// Accept all containers that have a sufficient weight behind them
	accepted := make([]block.Summary, 0, len(fs.acceptedVotes))
	for key, weight := range fs.acceptedVotes {
		if weight >= fs.Alpha {
			accepted = append(accepted, block.Summary{
				Key:   []byte(key),
				State: fs.acceptedFrontierSet[key],
			})
		}
	}

	// if we don't have enough weight for the state summary to be accepted then retry or fail the state sync
	size := len(accepted)
	failedAccepted := fs.gR.GetAllFailed(message.AcceptedStateSummary)
	if size == 0 && fs.Beacons.Len() > 0 {
		// retry the bootstrap if the weight is not enough to bootstrap
		failedBeaconWeight, err := fs.Beacons.SubsetWeight(failedAccepted)
		if err != nil {
			return err
		}

		// in a zero network there will be no accepted votes but the voting weight will be greater than the failed weight
		if fs.Config.RetryBootstrap && fs.Beacons.Weight()-fs.Alpha < failedBeaconWeight {
			fs.Ctx.Log.Debug("Not enough votes received, restarting state sync... - Beacons: %d - Failed Bootstrappers: %d "+
				"- fast sync attempt: %d", fs.Beacons.Len(), failedAccepted.Len(), fs.attempts)
			return fs.RestartBootstrap(false)
		}
	}

	if !fs.Restarted {
		fs.Ctx.Log.Info("State sync started syncing with %d vertices in the accepted frontier", size)
	} else {
		fs.Ctx.Log.Debug("State sync started syncing with %d vertices in the accepted frontier", size)
	}

	return fs.fastSyncVM.StateSync(accepted)
}

// Failed messages
// GetStateSummaryFrontierFailed implements the Engine interface.
func (fs *fastSyncer) GetStateSummaryFrontierFailed(validatorID ids.ShortID, requestID uint32) error {
	// ignores any late responses
	if requestID != fs.RequestID {
		fs.Ctx.Log.Debug("Received an Out-of-Sync GetStateSummaryFrontierFailed - validator: %v - expectedRequestID: %v, requestID: %v",
			validatorID, fs.RequestID, requestID)
		return nil
	}

	if err := fs.gR.AddFailed(message.StateSummaryFrontier, validatorID); err != nil {
		return err
	}

	return fs.StateSummaryFrontier(validatorID, requestID, []byte{}, []byte{}) // TODO ABENEGIA: Why empty key and summary?
}

// GetAcceptedStateSummaryFailed implements the Engine interface.
func (fs *fastSyncer) GetAcceptedStateSummaryFailed(validatorID ids.ShortID, requestID uint32) error {
	// ignores any late responses
	if requestID != fs.RequestID {
		fs.Ctx.Log.Debug("Received an Out-of-Sync GetAcceptedStateSummaryFailed - validator: %v - expectedRequestID: %v, requestID: %v",
			validatorID, fs.RequestID, requestID)
		return nil
	}

	if err := fs.gR.AddFailed(message.AcceptedStateSummary, validatorID); err != nil {
		return err
	}

	return fs.AcceptedStateSummary(validatorID, requestID, [][]byte{}) // TODO ABENEGIA: just pick keys here and reask for them!
}

// AppRequest implements the Engine interface
func (fs *fastSyncer) AppRequest(nodeID ids.ShortID, requestID uint32, deadline time.Time, request []byte) error {
	return fs.VM.AppRequest(nodeID, requestID, deadline, request)
}

// AppResponse implements the Engine interface
func (fs *fastSyncer) AppResponse(nodeID ids.ShortID, requestID uint32, response []byte) error {
	return fs.VM.AppResponse(nodeID, requestID, response)
}

// AppRequestFailed implements the Engine interface
func (fs *fastSyncer) AppRequestFailed(nodeID ids.ShortID, requestID uint32) error {
	return fs.VM.AppRequestFailed(nodeID, requestID)
}

func (fs *fastSyncer) RestartBootstrap(reset bool) error {
	// resets the attempts when we're pulling blocks/vertices we don't want to
	// fail the bootstrap at that stage
	if reset {
		fs.Ctx.Log.Debug("Checking for new fast sync frontiers")

		fs.Restarted = true
		fs.attempts = 0
	}

	if fs.attempts > 0 && fs.attempts%fs.RetryBootstrapWarnFrequency == 0 {
		fs.Ctx.Log.Debug("continuing to attempt to fast sync after %d failed attempts. Is this node connected to the internet?",
			fs.attempts)
	}

	return fs.startup()
}
