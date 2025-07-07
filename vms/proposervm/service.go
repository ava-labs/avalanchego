// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"encoding/json"
	"fmt"
	"net/http"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/api"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/vms/proposervm/block"

	avajson "github.com/ava-labs/avalanchego/utils/json"
)

type ProposerVMServer interface {
	GetStatelessSignedBlock(blkID ids.ID) (block.SignedBlock, error)
	GetLastAcceptedHeight() uint64
}

type ProposerAPI struct {
	ctx *snow.Context
	vm  ProposerVMServer
}

func (p *ProposerAPI) GetProposedHeight(_ *http.Request, _ *struct{}, reply *api.GetHeightResponse) error {
	p.ctx.Log.Debug("API called",
		zap.String("service", "proposervm"),
		zap.String("method", "getProposedHeight"),
	)
	p.ctx.Lock.Lock()
	defer p.ctx.Lock.Unlock()

	reply.Height = avajson.Uint64(p.vm.GetLastAcceptedHeight())
	return nil
}

// GetProposerBlockArgs is the parameters supplied to the GetProposerBlockWrapper API
type GetProposerBlockArgs struct {
	ProposerBlockID ids.ID              `json:"proposerID"`
	Encoding        formatting.Encoding `json:"encoding"`
}

func (p *ProposerAPI) GetProposerBlockWrapper(_ *http.Request, args *GetProposerBlockArgs, reply *api.GetBlockResponse) error {
	p.ctx.Log.Debug("API called",
		zap.String("service", "proposervm"),
		zap.String("method", "getProposerBlockWrapper"),
		zap.String("proposerID", args.ProposerBlockID.String()),
		zap.String("encoding", args.Encoding.String()),
	)
	p.ctx.Lock.Lock()
	defer p.ctx.Lock.Unlock()

	block, err := p.vm.GetStatelessSignedBlock(args.ProposerBlockID)
	if err != nil {
		return err
	}
	reply.Encoding = args.Encoding

	var result any
	if args.Encoding == formatting.JSON {
		result = block
	} else {
		result, err = formatting.Encode(args.Encoding, block.Bytes())
		if err != nil {
			return fmt.Errorf("couldn't encode block %s as %s: %w", args.ProposerBlockID, args.Encoding, err)
		}
	}

	reply.Block, err = json.Marshal(result)
	return err
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

	lastAccepted, err := p.vm.LastAccepted(r.Context())
	if err != nil {
		return fmt.Errorf("couldn't get last accepted block ID: %w", err)
	}
	latestBlock, err := p.vm.getBlock(r.Context(), lastAccepted)
	if err != nil {
		return fmt.Errorf("couldn't get latest block: %w", err)
	}

	epochNumber, err := latestBlock.epochNumber(r.Context())
	if err != nil {
		return fmt.Errorf("couldn't get epoch number: %w", err)
	}
	epochStartTime, err := latestBlock.epochStartTime(r.Context())
	if err != nil {
		return fmt.Errorf("couldn't get epoch start time: %w", err)
	}
	epochPChainHeight, err := latestBlock.pChainEpochHeight(r.Context())
	if err != nil {
		return fmt.Errorf("couldn't get epoch P-Chain height: %w", err)
	}

	reply.Number = avajson.Uint64(epochNumber)
	reply.StartTime = avajson.Uint64(epochStartTime.Unix())
	reply.PChainHeight = avajson.Uint64(epochPChainHeight)

	return nil
}
