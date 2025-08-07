// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
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

	avajson "github.com/ava-labs/avalanchego/utils/json"
)

type ProposerAPI struct {
	ctx *snow.Context
	vm  VMInterface
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

func (p *ProposerAPI) GetProposerBlockWrapper(r *http.Request, args *GetProposerBlockArgs, reply *api.GetBlockResponse) error {
	p.ctx.Log.Debug("API called",
		zap.String("service", "proposervm"),
		zap.String("method", "getProposerBlockWrapper"),
		zap.String("proposerID", args.ProposerBlockID.String()),
		zap.String("encoding", args.Encoding.String()),
	)
	p.ctx.Lock.Lock()
	defer p.ctx.Lock.Unlock()

	block, err := p.vm.GetBlock(r.Context(), args.ProposerBlockID)
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
