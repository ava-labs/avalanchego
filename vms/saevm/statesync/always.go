package statesync

import (
	"context"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/vms/saevm/adaptor"
	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
	"github.com/ava-labs/avalanchego/vms/saevm/hook"
	"github.com/ava-labs/avalanchego/vms/saevm/sae"
)

var _ adaptor.SyncableVM[*blocks.Block, *Summary] = (*SinceGenesis[hook.Transaction])(nil)

// SinceGenesis is a harness around a [VM], providing an `Initialize` method
// that treats the chain as being asynchronous since genesis.
type SinceGenesis[T hook.Transaction] struct {
	*VM[T] // created by [SinceGenesis.Initialize]

	hooks  hook.PointsG[T]
	config sae.Config
}

// Initialize implements [adaptor.SyncableVM].
func (s *SinceGenesis[T]) Initialize(
	ctx context.Context,
	chainCtx *snow.Context,
	avaDB database.Database,
	genesisBytes []byte,
	upgradeBytes []byte,
	configBytes []byte,
	fxs []*common.Fx,
	appSender common.AppSender,
) error {
	panic("unimplemented")
}

// Shutdown TODO.
func (s *SinceGenesis[T]) Shutdown(ctx context.Context) error {
	if s.VM == nil {
		return nil
	}
	return s.VM.Shutdown(ctx)
}
