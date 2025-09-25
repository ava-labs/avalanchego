// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"fmt"
	"net/http"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/api"

	avajson "github.com/ava-labs/avalanchego/utils/json"
)

type ProposerAPI struct {
	vm *VM
}

type GetEpochResponse struct {
	Number       avajson.Uint64 `json:"number"`
	StartTime    avajson.Uint64 `json:"startTime"`
	PChainHeight avajson.Uint64 `json:"pChainHeight"`
}

func (p *ProposerAPI) GetProposedHeight(r *http.Request, _ *struct{}, reply *api.GetHeightResponse) error {
	p.vm.ctx.Log.Debug("API called",
		zap.String("service", "proposervm"),
		zap.String("method", "getProposedHeight"),
	)

	height, err := p.vm.ctx.ValidatorState.GetMinimumHeight(r.Context())
	if err != nil {
		return fmt.Errorf("couldn't get minimum height %w", err)
	}

	reply.Height = avajson.Uint64(height)
	return nil
}

// Returns the epoch information that will be used for the next block to be proposed.
func (p *ProposerAPI) GetCurrentEpoch(r *http.Request, _ *struct{}, reply *GetEpochResponse) error {
	p.vm.ctx.Log.Debug("API called",
		zap.String("service", "proposervm"),
		zap.String("method", "getCurrentEpoch"),
	)

	// This will error if we haven't advanced past the genesis block.
	lastAccepted, err := p.vm.GetLastAccepted()
	if err != nil {
		return fmt.Errorf("couldn't get last accepted block ID %w", err)
	}

	latestBlock, err := p.vm.getPostForkBlock(r.Context(), lastAccepted)
	if err != nil {
		return fmt.Errorf("couldn't get latest block %s: %w", lastAccepted.String(), err)
	}

	epoch, err := latestBlock.pChainEpoch(r.Context())
	if err != nil {
		return fmt.Errorf("couldn't get latest block epoch %s: %w", lastAccepted.String(), err)
	}

	pChainHeight, err := latestBlock.pChainHeight(r.Context())
	if err != nil {
		return fmt.Errorf("couldn't get latest block p-chain height %s: %w", lastAccepted.String(), err)
	}

	nextEpoch := nextPChainEpoch(
		pChainHeight,
		epoch,
		latestBlock.Timestamp(),
		p.vm.Upgrades.GraniteEpochDuration,
	)

	// Latest block sealed the epoch.
	reply.Number = avajson.Uint64(nextEpoch.Number)
	reply.StartTime = avajson.Uint64(nextEpoch.StartTime.Unix())
	reply.PChainHeight = avajson.Uint64(nextEpoch.PChainHeight)

	return nil
}
