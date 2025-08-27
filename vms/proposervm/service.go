// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"net/http"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/api"

	avajson "github.com/ava-labs/avalanchego/utils/json"
)

type ProposerAPI struct {
	vm *VM
}

func (p *ProposerAPI) GetProposedHeight(_ *http.Request, _ *struct{}, reply *api.GetHeightResponse) error {
	p.vm.ctx.Log.Debug("API called",
		zap.String("service", "proposervm"),
		zap.String("method", "getProposedHeight"),
	)
	p.vm.ctx.Lock.Lock()
	defer p.vm.ctx.Lock.Unlock()

	reply.Height = avajson.Uint64(p.vm.lastAcceptedHeight)
	return nil
}
