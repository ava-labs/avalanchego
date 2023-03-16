// (c) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"context"
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

// WarpAPI introduces snowman specific functionality to the evm
type WarpAPI struct {
	Backend WarpBackend
}

// GetSignature returns the BLS signature associated with a messageID.
func (api *WarpAPI) GetSignature(ctx context.Context, messageID ids.ID) (hexutil.Bytes, error) {
	signature, err := api.Backend.GetSignature(messageID)
	if err != nil {
		return nil, fmt.Errorf("failed to get signature for with error %w", err)
	}
	return signature[:], nil
}
