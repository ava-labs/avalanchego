// (c) 2021-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"context"
	"fmt"
	"strings"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ethereum/go-ethereum/common"
)

var _ Request = CodeRequest{}

// CodeRequest is a request to retrieve a contract code with specified Hash
type CodeRequest struct {
	// Hashes is a list of contract code hashes
	Hashes []common.Hash `serialize:"true"`
}

func (c CodeRequest) String() string {
	hashStrs := make([]string, len(c.Hashes))
	for i, hash := range c.Hashes {
		hashStrs[i] = hash.String()
	}
	return fmt.Sprintf("CodeRequest(Hashes=%s)", strings.Join(hashStrs, ", "))
}

func (c CodeRequest) Handle(ctx context.Context, nodeID ids.NodeID, requestID uint32, handler RequestHandler) ([]byte, error) {
	return handler.HandleCodeRequest(ctx, nodeID, requestID, c)
}

func NewCodeRequest(hashes []common.Hash) CodeRequest {
	return CodeRequest{
		Hashes: hashes,
	}
}

// CodeResponse is a response to a CodeRequest
// crypto.Keccak256Hash of each element in Data is expected to equal
// the corresponding element in CodeRequest.Hashes
// handler: handlers.CodeRequestHandler
type CodeResponse struct {
	Data [][]byte `serialize:"true"`
}
