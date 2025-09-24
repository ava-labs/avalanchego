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
		p.vm.ctx.Log.Error("failed to get minimum height",
			zap.String("method", "getProposedHeight"),
			zap.Error(err))
		return fmt.Errorf("could not get minimum height from validator state: %w", err)
	}

	reply.Height = avajson.Uint64(height)
	return nil
}
