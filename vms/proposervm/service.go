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

func (p *ProposerAPI) GetCurrentEpoch(r *http.Request, _ *struct{}, reply *api.GetEpochResponse) error {
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

	if latestBlock.Timestamp().Before(epoch.StartTime.Add(p.vm.Upgrades.GraniteEpochDuration)) {
		// Latest block did not seal the epoch.
		reply.Number = avajson.Uint64(epoch.Number)
		reply.StartTime = avajson.Uint64(epoch.StartTime.Unix())
		reply.PChainHeight = avajson.Uint64(epoch.Height)
	} else {
		// Latest block sealed the epoch.
		pChainHeight, err := latestBlock.pChainHeight(r.Context())
		if err != nil {
			return fmt.Errorf("couldn't get latest block p-chain height %s: %w", lastAccepted.String(), err)
		}
		reply.Number = avajson.Uint64(epoch.Number + 1)
		reply.StartTime = avajson.Uint64(latestBlock.Timestamp().Unix())
		reply.PChainHeight = avajson.Uint64(pChainHeight)
	}

	return nil
}
