// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"context"
	"fmt"
	"strings"

	"github.com/ava-labs/libevm/common"

	"github.com/ava-labs/avalanchego/ids"
)

var _ Request = (*CodeRequest)(nil)

// CodeRequest is a request to retrieve contract code for the specified hashes.
type CodeRequest struct {
	// Hashes is a list of contract code hashes to retrieve.
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

// NewCodeRequest creates a new CodeRequest with the given hashes.
func NewCodeRequest(hashes []common.Hash) CodeRequest {
	return CodeRequest{
		Hashes: hashes,
	}
}

// CodeResponse is a response to a CodeRequest.
// The crypto.Keccak256Hash of each element in Data is expected to equal
// the corresponding element in CodeRequest.Hashes.
type CodeResponse struct {
	Data [][]byte `serialize:"true"`
}
