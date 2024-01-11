// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ids

// RequestID is a unique identifier for an in-flight request pending a response.
type RequestID struct {
	// The node this request came from
	NodeID NodeID
	// The chain this request came from
	SourceChainID ID
	// The chain the expected response should come from
	DestinationChainID ID
	// The unique identifier for this request
	RequestID uint32
	// The message opcode
	Op byte
}
