// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"fmt"
	"net/http"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/api"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/proposervm/block"

	avajson "github.com/ava-labs/avalanchego/utils/json"
)

type ProposerVMServer interface {
	GetStatelessSignedBlock(blkID ids.ID) (block.SignedBlock, error)
	GetLastAcceptedHeight() uint64
}

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

	reply.Height = 0 // TODO
	return nil
}

type GetEpochResponse struct {
	Number       avajson.Uint64 `json:"Number"`
	StartTime    avajson.Uint64 `json:"StartTime"`
	PChainHeight avajson.Uint64 `json:"pChainHeight"`
}

func (p *ProposerAPI) GetEpoch(r *http.Request, _ *struct{}, reply *GetEpochResponse) error {
	p.vm.ctx.Log.Debug("API called",
		zap.String("service", "proposervm"),
		zap.String("method", "getEpoch"),
	)

	lastAccepted, err := p.vm.GetLastAccepted()
	if err != nil {
		return fmt.Errorf("couldn't get last accepted block ID: %w", err)
	}
	latestBlock, err := p.vm.getPostForkBlock(r.Context(), lastAccepted)
	if err != nil {
		return fmt.Errorf("couldn't get latest block: %w", err)
	}

	epoch, err := latestBlock.pChainEpoch(r.Context())
	if err != nil {
		return fmt.Errorf("couldn't get epoch P-Chain height: %w", err)
	}

	reply.Number = avajson.Uint64(epoch.Epoch)
	reply.StartTime = avajson.Uint64(epoch.StartTime.Unix())
	reply.PChainHeight = avajson.Uint64(epoch.Height)

	return nil
}
