// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"context"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/vms/platformvm/block"

	smblock "github.com/ava-labs/avalanchego/snow/engine/snowman/block"
)

const ActivationLog = `
   _____               .__                       .__             .____    ____
  /  _  \___  _______  |  | _____    ____   ____ |  |__   ____   |    |  /_   | ______
 /  /_\  \  \/ /\__  \ |  | \__  \  /    \_/ ___\|  |  \_/ __ \  |    |   |   |/  ___/
/    |    \   /  / __ \|  |__/ __ \|   |  \  \___|   Y  \  ___/  |    |___|   |\___ \
\____|__  /\_/  (____  /____(____  /___|  /\___  >___|  /\___  > |_______ \___/____  >
        \/           \/          \/     \/     \/     \/     \/          \/        \/
  ___        _____          __  .__               __             .___   ___
 / _ \_/\   /  _  \   _____/  |_|__|__  _______ _/  |_  ____   __| _/  / _ \_/\
 \/ \___/  /  /_\  \_/ ___\   __\  \  \/ /\__  \\   __\/ __ \ / __ |   \/ \___/ ,_ o
          /    |    \  \___|  | |  |\   /  / __ \|  | \  ___// /_/ |            / //\,
          \____|__  /\___  >__| |__| \_/  (____  /__|  \___  >____ |             \>> |
                  \/     \/                    \/          \/     \/              \\

`

var EtnaActivationWasLogged bool

var (
	_ snowman.Block             = (*Block)(nil)
	_ snowman.OracleBlock       = (*Block)(nil)
	_ smblock.WithVerifyContext = (*Block)(nil)
)

// Exported for testing in platformvm package.
type Block struct {
	block.Block
	manager *manager
}

func (*Block) ShouldVerifyWithContext(context.Context) (bool, error) {
	return true, nil
}

func (b *Block) VerifyWithContext(ctx context.Context, blockContext *smblock.Context) error {
	blkID := b.ID()
	if blkState, ok := b.manager.blkIDToState[blkID]; ok {
		if !blkState.verifiedHeights.Contains(blockContext.PChainHeight) {
			// Only the validity of warp messages need to be verified because
			// this block was already executed with a different block context.
			err := VerifyWarpMessages(
				ctx,
				b.manager.ctx.NetworkID,
				b.manager.ctx.ValidatorState,
				blockContext.PChainHeight,
				b,
			)
			if err != nil {
				return err
			}

			blkState.verifiedHeights.Add(blockContext.PChainHeight)
		}

		return nil // This block has already been executed.
	}

	// Verify the warp messages in the block.
	err := VerifyWarpMessages(
		ctx,
		b.manager.ctx.NetworkID,
		b.manager.ctx.ValidatorState,
		blockContext.PChainHeight,
		b,
	)
	if err != nil {
		return err
	}

	// Since the warp messages are valid, we need to execute the rest of the
	// validity checks.
	return b.Visit(&verifier{
		backend:           b.manager.backend,
		txExecutorBackend: b.manager.txExecutorBackend,
		pChainHeight:      blockContext.PChainHeight,
	})
}

func (b *Block) Verify(ctx context.Context) error {
	return b.VerifyWithContext(
		ctx,
		&smblock.Context{
			PChainHeight: 0,
		},
	)
}

func (b *Block) Accept(context.Context) error {
	if err := b.Visit(b.manager.acceptor); err != nil {
		return err
	}

	currentTime := b.manager.state.GetTimestamp()
	if !b.manager.txExecutorBackend.Config.UpgradeConfig.IsEtnaActivated(currentTime) {
		return nil
	}

	if !EtnaActivationWasLogged && b.manager.txExecutorBackend.Bootstrapped.Get() {
		fmt.Print(ActivationLog)
	}
	EtnaActivationWasLogged = true
	return nil
}

func (b *Block) Reject(context.Context) error {
	return b.Visit(b.manager.rejector)
}

func (b *Block) Timestamp() time.Time {
	return b.manager.getTimestamp(b.ID())
}

func (b *Block) Options(context.Context) ([2]snowman.Block, error) {
	options := options{
		log:                     b.manager.ctx.Log,
		primaryUptimePercentage: b.manager.txExecutorBackend.Config.UptimePercentage,
		uptimes:                 b.manager.txExecutorBackend.Uptimes,
		state:                   b.manager.backend.state,
	}
	if err := b.Block.Visit(&options); err != nil {
		return [2]snowman.Block{}, err
	}

	return [2]snowman.Block{
		b.manager.NewBlock(options.preferredBlock),
		b.manager.NewBlock(options.alternateBlock),
	}, nil
}
