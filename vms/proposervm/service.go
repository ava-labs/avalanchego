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
