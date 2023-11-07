// (c) 2021-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"context"
	"encoding/json"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/ids"

	"github.com/ava-labs/subnet-evm/internal/ethapi"
	"github.com/ava-labs/subnet-evm/rpc"

	"github.com/ethereum/go-ethereum/log"
)

var _ CrossChainRequestHandler = &crossChainHandler{}

// crossChainHandler implements the CrossChainRequestHandler interface
type crossChainHandler struct {
	backend         ethapi.Backend
	crossChainCodec codec.Manager
}

// NewCrossChainHandler creates and returns a new instance of CrossChainRequestHandler
func NewCrossChainHandler(b ethapi.Backend, codec codec.Manager) CrossChainRequestHandler {
	return &crossChainHandler{
		backend:         b,
		crossChainCodec: codec,
	}
}

// HandleEthCallRequests returns an encoded EthCallResponse to the given [ethCallRequest]
// This function executes EVM Call against the state associated with [rpc.AcceptedBlockNumber] with the given
// transaction call object [ethCallRequest].
// This function does not return an error as errors are treated as FATAL to the node.
func (c *crossChainHandler) HandleEthCallRequest(ctx context.Context, requestingChainID ids.ID, requestID uint32, ethCallRequest EthCallRequest) ([]byte, error) {
	lastAcceptedBlockNumber := rpc.BlockNumber(c.backend.LastAcceptedBlock().NumberU64())
	lastAcceptedBlockNumberOrHash := rpc.BlockNumberOrHash{BlockNumber: &lastAcceptedBlockNumber}

	transactionArgs := ethapi.TransactionArgs{}
	err := json.Unmarshal(ethCallRequest.RequestArgs, &transactionArgs)
	if err != nil {
		log.Error("error occurred with JSON unmarshalling ethCallRequest.RequestArgs", "err", err)
		return nil, nil
	}

	result, err := ethapi.DoCall(
		ctx,
		c.backend,
		transactionArgs,
		lastAcceptedBlockNumberOrHash,
		nil,
		nil,
		c.backend.RPCEVMTimeout(),
		c.backend.RPCGasCap())
	if err != nil {
		log.Error("error occurred with EthCall", "err", err, "transactionArgs", ethCallRequest.RequestArgs, "blockNumberOrHash", lastAcceptedBlockNumberOrHash)
		return nil, nil
	}

	executionResult, err := json.Marshal(&result)
	if err != nil {
		log.Error("error occurred with JSON marshalling result", "err", err)
		return nil, nil
	}

	response := EthCallResponse{
		ExecutionResult: executionResult,
	}

	responseBytes, err := c.crossChainCodec.Marshal(Version, response)
	if err != nil {
		log.Error("error occurred with marshalling EthCallResponse", "err", err, "EthCallResponse", response)
		return nil, nil
	}

	return responseBytes, nil
}
