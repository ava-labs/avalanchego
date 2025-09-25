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
	log := p.vm.ctx.Log.With(
		zap.String("service", "proposervm"),
		zap.String("method", "GetProposedHeight"),
	)

	log.Debug("JSON-RPC called")

	id, err := p.vm.State.GetLastAccepted()
	if err != nil {
		log.Error("failed to get last accepted block", zap.Error(err))
		return fmt.Errorf("failed to get last accepted block: %w", err)
	}

	blk, err := p.vm.getBlock(r.Context(), id)
	if err != nil {
		log.Error("failed to get block", zap.Error(err))
		return fmt.Errorf("failed to get block: %w", err)
	}

	height, err := blk.selectChildPChainHeight(r.Context())
	if err != nil {
		log.Error("failed to get child p-chain height", zap.Error(err))
		return fmt.Errorf("failed to get child p-chain height: %w", err)
	}

	reply.Height = avajson.Uint64(height)
	return nil
}
