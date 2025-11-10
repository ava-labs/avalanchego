// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ethapi

import (
	"context"
	"errors"
	"fmt"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/common/hexutil"
	"github.com/ava-labs/libevm/rlp"

	"github.com/ava-labs/avalanchego/graft/coreth/core"
	"github.com/ava-labs/avalanchego/graft/coreth/params"
	"github.com/ava-labs/avalanchego/graft/coreth/rpc"
)

type DetailedExecutionResult struct {
	UsedGas    uint64        `json:"gas"`        // Total used gas but include the refunded gas
	ErrCode    int           `json:"errCode"`    // EVM error code
	Err        string        `json:"err"`        // Any error encountered during the execution(listed in core/vm/errors.go)
	ReturnData hexutil.Bytes `json:"returnData"` // Data from evm(function result or data supplied with revert opcode)
}

// GetChainConfig returns the chain config.
func (api *BlockChainAPI) GetChainConfig(context.Context) *params.ChainConfig {
	return api.b.ChainConfig()
}

// CallDetailed performs the same call as Call, but returns the full context
func (s *BlockChainAPI) CallDetailed(ctx context.Context, args TransactionArgs, blockNrOrHash rpc.BlockNumberOrHash, overrides *StateOverride) (*DetailedExecutionResult, error) {
	result, err := DoCall(ctx, s.b, args, blockNrOrHash, overrides, nil, s.b.RPCEVMTimeout(), s.b.RPCGasCap())
	if err != nil {
		return nil, err
	}

	reply := &DetailedExecutionResult{
		UsedGas:    result.UsedGas,
		ReturnData: result.ReturnData,
	}
	if result.Err != nil {
		if err, ok := result.Err.(rpc.Error); ok {
			reply.ErrCode = err.ErrorCode()
		}
		reply.Err = result.Err.Error()
	}
	// If the result contains a revert reason, try to unpack and return it.
	if len(result.Revert()) > 0 {
		err := newRevertError(result.Revert())
		reply.ErrCode = err.ErrorCode()
		reply.Err = err.Error()
	}
	return reply, nil
}

// Note: this API is moved directly from ./eth/api.go to ensure that it is available under an API that is enabled by
// default without duplicating the code and serving the same API in the original location as well without creating a
// cyclic import.
//
// BadBlockArgs represents the entries in the list returned when bad blocks are queried.
type BadBlockArgs struct {
	Hash   common.Hash            `json:"hash"`
	Block  map[string]interface{} `json:"block"`
	RLP    string                 `json:"rlp"`
	Reason *core.BadBlockReason   `json:"reason"`
}

// GetBadBlocks returns a list of the last 'bad blocks' that the client has seen on the network
// and returns them as a JSON list of block hashes.
func (s *BlockChainAPI) GetBadBlocks(context.Context) ([]*BadBlockArgs, error) {
	var (
		badBlocks, reasons = s.b.BadBlocks()
		results            = make([]*BadBlockArgs, 0, len(badBlocks))
	)
	for i, block := range badBlocks {
		var (
			blockRlp  string
			blockJSON map[string]interface{}
		)
		if rlpBytes, err := rlp.EncodeToBytes(block); err != nil {
			blockRlp = err.Error() // Hacky, but hey, it works
		} else {
			blockRlp = fmt.Sprintf("%#x", rlpBytes)
		}
		blockJSON = RPCMarshalBlock(block, true, true, s.b.ChainConfig())
		results = append(results, &BadBlockArgs{
			Hash:   block.Hash(),
			RLP:    blockRlp,
			Block:  blockJSON,
			Reason: reasons[i],
		})
	}
	return results, nil
}

// stateQueryBlockNumberAllowed returns a nil error if:
//   - the node is configured to accept any state query (the query window is zero)
//   - the block given has its number within the query window before the last accepted block.
//     This query window is set to [core.TipBufferSize] when running in a non-archive mode.
//
// Otherwise, it returns a non-nil error containing block number information.
func (s *BlockChainAPI) stateQueryBlockNumberAllowed(blockNumOrHash rpc.BlockNumberOrHash) (err error) {
	queryWindow := s.b.HistoricalProofQueryWindow()
	if s.b.IsArchive() && queryWindow == 0 {
		return nil
	}

	lastAcceptedNumber := s.b.LastAcceptedBlock().NumberU64()

	var number uint64
	if blockNumOrHash.BlockNumber != nil {
		number = uint64(blockNumOrHash.BlockNumber.Int64())
	} else if blockHash, ok := blockNumOrHash.Hash(); ok {
		block, err := s.b.BlockByHash(context.Background(), blockHash)
		if err != nil {
			return fmt.Errorf("failed to get block from hash: %w", err)
		} else if block == nil {
			return fmt.Errorf("block from hash %s doesn't exist", blockHash)
		}
		number = block.NumberU64()
	} else {
		return errors.New("block number or hash not provided")
	}

	var oldestAllowed uint64
	if lastAcceptedNumber > queryWindow {
		oldestAllowed = lastAcceptedNumber - queryWindow
	}
	if number >= oldestAllowed {
		return nil
	}
	return fmt.Errorf("block number %d is before the oldest allowed block number %d (window of %d blocks)",
		number, oldestAllowed, queryWindow)
}
