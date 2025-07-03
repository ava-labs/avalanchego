// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"encoding/json"
	"fmt"
	"net/http"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/api"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/formatting"
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

	reply.Height = avajson.Uint64(p.vm.lastAcceptedHeight)
	return nil
}

// GetProposerBlockArgs is the parameters supplied to the GetProposerBlockWrapper API
type GetProposerBlockArgs struct {
	ProposerID ids.ID              `json:"proposerID"`
	Encoding   formatting.Encoding `json:"encoding"`
}

func (p *ProposerAPI) GetProposerBlockWrapper(r *http.Request, args *GetProposerBlockArgs, reply *api.GetBlockResponse) error {
	p.vm.ctx.Log.Debug("API called",
		zap.String("service", "proposervm"),
		zap.String("method", "getProposerBlockWrapper"),
		zap.String("proposerID", args.ProposerID.String()),
		zap.String("encoding", args.Encoding.String()),
	)

	block, err := p.vm.GetBlock(r.Context(), args.ProposerID)
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
			return fmt.Errorf("couldn't encode block %s as %s: %w", args.ProposerID, args.Encoding, err)
		}
	}

	reply.Block, err = json.Marshal(result)
	return nil
}

type GetEpochResponse struct {
	Number       uint64 `json:"Number"`
	StartTime    int64  `json:"StartTime"`
	PChainHeight uint64 `json:"pChainHeight"`
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

	reply.Number = epochNumber
	reply.StartTime = epochStartTime.Unix()
	reply.PChainHeight = epochPChainHeight

	p.vm.ctx.Log.Info("ProposerAPI called",
		zap.String("service", "proposervm"),
		zap.String("method", "getEpoch"),
		zap.Uint64("epochNumber", epochNumber),
		zap.Int64("epochStartTime", epochStartTime.Unix()),
		zap.Uint64("pChainHeight", epochPChainHeight),
	)

	return nil
}
