// (c) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"context"
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/subnet-evm/warp/aggregator"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

// API introduces snowman specific functionality to the evm
type API struct {
	backend    Backend
	aggregator *aggregator.Aggregator
}

func NewAPI(backend Backend, aggregator *aggregator.Aggregator) *API {
	return &API{
		backend:    backend,
		aggregator: aggregator,
	}
}

// GetSignature returns the BLS signature associated with a messageID.
func (a *API) GetSignature(ctx context.Context, messageID ids.ID) (hexutil.Bytes, error) {
	signature, err := a.backend.GetSignature(messageID)
	if err != nil {
		return nil, fmt.Errorf("failed to get signature for with error %w", err)
	}
	return signature[:], nil
}

// GetAggregateSignature fetches the aggregate signature for the requested [messageID]
func (a *API) GetAggregateSignature(ctx context.Context, messageID ids.ID, quorumNum uint64) (signedMessageBytes hexutil.Bytes, err error) {
	unsignedMessage, err := a.backend.GetMessage(messageID)
	if err != nil {
		return nil, err
	}

	signatureResult, err := a.aggregator.AggregateSignatures(ctx, unsignedMessage, quorumNum)
	if err != nil {
		return nil, err
	}
	// TODO: return the signature and total weight as well to the caller for more complete details
	// Need to decide on the best UI for this and write up documentation with the potential
	// gotchas that could impact signed messages becoming invalid.
	return hexutil.Bytes(signatureResult.Message.Bytes()), nil
}
