package fastsyncer

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
)

var _ FastSyncer = &fastSyncer{}

type Config struct {
	VM block.ChainVM
}

type FastSyncer interface {
	common.FastSyncHandler

	Start() error
}

func NewFastSyncer(
	cfg Config,
	onDoneFastSyncing func() error,
) FastSyncer {
	return &fastSyncer{
		VM:                cfg.VM,
		onDoneFastSyncing: onDoneFastSyncing,
	}
}

type fastSyncer struct {
	VM                block.ChainVM
	onDoneFastSyncing func() error
}

func (fs *fastSyncer) Start() error {
	syncVM, ok := fs.VM.(block.StateSyncableVM)
	if !ok {
		// nothing to do, vm does not implement fast sync
		return fs.onDoneFastSyncing()
	}

	if !syncVM.Enabled() {
		// nothing to do, fast sync is implemented but not enabled
		return fs.onDoneFastSyncing()
	}

	// TODO: to implement
	return nil
}

func (fs *fastSyncer) GetStateSummaryFrontier(validatorID ids.ShortID, requestID uint32) error {
	// TODO: implement
	return nil
}

func (fs *fastSyncer) StateSummaryFrontier(validatorID ids.ShortID, requestID uint32, summary []byte) error {
	// TODO: implement
	return nil
}

func (fs *fastSyncer) GetAcceptedStateSummary(validatorID ids.ShortID, requestID uint32, summaries [][]byte) error {
	// TODO: implement
	return nil
}

func (fs *fastSyncer) AcceptedStateSummary(validatorID ids.ShortID, requestID uint32, summaries [][]byte) error {
	// TODO: implement
	return nil
}
