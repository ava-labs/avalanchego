// (c) 2021-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"context"
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ethereum/go-ethereum/common"
)

var _ Request = CodeRequest{}

// CodeRequest is a request to retrieve a contract code with specified Hash
type CodeRequest struct {
	// Hash is the contract code hash
	Hash common.Hash `serialize:"true"`
}

func (c CodeRequest) String() string {
	return fmt.Sprintf("CodeRequest(Hash=%s)", c.Hash)
}

func (c CodeRequest) Handle(ctx context.Context, nodeID ids.ShortID, requestID uint32, handler RequestHandler) ([]byte, error) {
	return handler.HandleCodeRequest(ctx, nodeID, requestID, c)
}

func NewCodeRequest(hash common.Hash) CodeRequest {
	return CodeRequest{
		Hash: hash,
	}
}

// CodeResponse is a response to a CodeRequest
// crypto.Keccak256Hash of Data is expected to equal CodeRequest.Hash
// handler: handlers.CodeRequestHandler
type CodeResponse struct {
	Data []byte `serialize:"true"`
}
