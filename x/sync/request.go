// (c) 2021-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sync

import (
	"context"
	"encoding/hex"
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
)

var (
	_ Request = (*RangeProofRequest)(nil)
	_ Request = (*ChangeProofRequest)(nil)
)

// A request to this node for a proof.
type Request interface {
	fmt.Stringer
	Handle(ctx context.Context, nodeID ids.NodeID, requestID uint32, h Handler) error
}

type rangeProofHandler interface {
	// Generates a range proof and sends it to [nodeID].
	// TODO danlaine how should we handle context cancellation?
	HandleRangeProofRequest(
		ctx context.Context,
		nodeID ids.NodeID,
		requestID uint32,
		request *RangeProofRequest,
	) error
}

type changeProofHandler interface {
	// Generates a change proof and sends it to [nodeID].
	// TODO danlaine how should we handle context cancellation?
	HandleChangeProofRequest(
		ctx context.Context,
		nodeID ids.NodeID,
		requestID uint32,
		request *ChangeProofRequest,
	) error
}

type Handler interface {
	rangeProofHandler
	changeProofHandler
}

type RangeProofRequest struct {
	Root  ids.ID `serialize:"true"`
	Start []byte `serialize:"true"`
	End   []byte `serialize:"true"`
	Limit uint16 `serialize:"true"`
}

func (r *RangeProofRequest) Handle(ctx context.Context, nodeID ids.NodeID, requestID uint32, h Handler) error {
	return h.HandleRangeProofRequest(ctx, nodeID, requestID, r)
}

func (r RangeProofRequest) String() string {
	return fmt.Sprintf(
		"RangeProofRequest(Root=%s, Start=%s, End=%s, Limit=%d)",
		r.Root,
		hex.EncodeToString(r.Start),
		hex.EncodeToString(r.End),
		r.Limit,
	)
}

// ChangeProofRequest is a request to receive trie leaves at specified Root within Start and End byte range
// Limit outlines maximum number of leaves to returns starting at Start
type ChangeProofRequest struct {
	StartingRoot ids.ID `serialize:"true"`
	EndingRoot   ids.ID `serialize:"true"`
	Start        []byte `serialize:"true"`
	End          []byte `serialize:"true"`
	Limit        uint16 `serialize:"true"`
}

func (r *ChangeProofRequest) Handle(ctx context.Context, nodeID ids.NodeID, requestID uint32, h Handler) error {
	return h.HandleChangeProofRequest(ctx, nodeID, requestID, r)
}

func (r ChangeProofRequest) String() string {
	return fmt.Sprintf(
		"ChangeProofRequest(StartRoot=%s, EndRoot=%s, Start=%s, End=%s, Limit=%d)",
		r.StartingRoot,
		r.EndingRoot,
		hex.EncodeToString(r.Start),
		hex.EncodeToString(r.End),
		r.Limit,
	)
}
