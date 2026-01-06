// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sync

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/x/sync/protoutils"

	pb "github.com/ava-labs/avalanchego/proto/pb/sync"
)

const (
	// Maximum number of key-value pairs to return in a proof.
	// This overrides any other Limit specified in a RangeProofRequest
	// or ChangeProofRequest if the given Limit is greater.
	MaxKeyValuesLimit = 2048
	// Estimated max overhead, in bytes, of putting a proof into a message.
	// We use this to ensure that the proof we generate is not too large to fit in a message.
	// TODO: refine this estimate. This is almost certainly a large overestimate.
	estimatedMessageOverhead = 4 * units.KiB
	maxByteSizeLimit         = constants.DefaultMaxMessageSize - estimatedMessageOverhead
)

var (
	_ p2p.Handler = (*GetChangeProofHandler[any, any])(nil)
	_ p2p.Handler = (*GetRangeProofHandler[any, any])(nil)

	ErrMinProofSizeIsTooLarge = errors.New("cannot generate any proof within the requested limit")

	errInvalidBytesLimit    = errors.New("bytes limit must be greater than 0")
	errInvalidKeyLimit      = errors.New("key limit must be greater than 0")
	errInvalidStartRootHash = fmt.Errorf("start root hash must have length %d", hashing.HashLen)
	errInvalidEndRootHash   = fmt.Errorf("end root hash must have length %d", hashing.HashLen)
	errInvalidBounds        = errors.New("start key is greater than end key")
	errInvalidRootHash      = fmt.Errorf("root hash must have length %d", hashing.HashLen)
	errEmptyProof           = errors.New("proof for empty trie requested")
)

func NewGetChangeProofHandler[R any, C any](db DB[R, C], rangeProofMarshaler Marshaler[R], changeProofMarshaler Marshaler[C]) *GetChangeProofHandler[R, C] {
	return &GetChangeProofHandler[R, C]{
		db:                   db,
		rangeProofMarshaler:  rangeProofMarshaler,
		changeProofMarshaler: changeProofMarshaler,
	}
}

type GetChangeProofHandler[R any, C any] struct {
	db                   DB[R, C]
	rangeProofMarshaler  Marshaler[R]
	changeProofMarshaler Marshaler[C]
}

func (*GetChangeProofHandler[_, _]) AppGossip(context.Context, ids.NodeID, []byte) {}

func (g *GetChangeProofHandler[R, _]) AppRequest(ctx context.Context, _ ids.NodeID, _ time.Time, requestBytes []byte) ([]byte, *common.AppError) {
	req := &pb.GetChangeProofRequest{}
	if err := proto.Unmarshal(requestBytes, req); err != nil {
		return nil, &common.AppError{
			Code:    p2p.ErrUnexpected.Code,
			Message: fmt.Sprintf("failed to unmarshal request: %s", err),
		}
	}

	if err := validateChangeProofRequest(req); err != nil {
		return nil, &common.AppError{
			Code:    p2p.ErrUnexpected.Code,
			Message: fmt.Sprintf("invalid request: %s", err),
		}
	}

	// override limits if they exceed caps
	var (
		keyLimit   = min(req.KeyLimit, MaxKeyValuesLimit)
		bytesLimit = min(int(req.BytesLimit), maxByteSizeLimit)
		start      = protoutils.ProtoToMaybe(req.StartKey)
		end        = protoutils.ProtoToMaybe(req.EndKey)
	)

	startRoot, err := ids.ToID(req.StartRootHash)
	if err != nil {
		return nil, &common.AppError{
			Code:    p2p.ErrUnexpected.Code,
			Message: fmt.Sprintf("failed to parse start root hash: %s", err),
		}
	}

	endRoot, err := ids.ToID(req.EndRootHash)
	if err != nil {
		return nil, &common.AppError{
			Code:    p2p.ErrUnexpected.Code,
			Message: fmt.Sprintf("failed to parse end root hash: %s", err),
		}
	}

	for keyLimit > 0 {
		changeProof, err := g.db.GetChangeProof(ctx, startRoot, endRoot, start, end, int(keyLimit))
		if err != nil {
			if !errors.Is(err, ErrInsufficientHistory) {
				// We should only fail to get a change proof if we have insufficient history.
				// Other errors are unexpected.
				// TODO define custom errors
				return nil, &common.AppError{
					Code:    p2p.ErrUnexpected.Code,
					Message: fmt.Sprintf("failed to get change proof: %s", err),
				}
			}
			if errors.Is(err, ErrNoEndRoot) {
				// [s.db] doesn't have [endRoot] in its history.
				// We can't generate a change/range proof. Drop this request.
				return nil, &common.AppError{
					Code:    p2p.ErrUnexpected.Code,
					Message: fmt.Sprintf("failed to get change proof: %s", err),
				}
			}

			// [s.db] doesn't have sufficient history to generate change proof.
			// Generate a range proof for the end root ID instead.
			proofBytes, err := getRangeProof(
				ctx,
				g.db,
				&pb.GetRangeProofRequest{
					RootHash:   req.EndRootHash,
					StartKey:   req.StartKey,
					EndKey:     req.EndKey,
					KeyLimit:   req.KeyLimit,
					BytesLimit: req.BytesLimit,
				},
				func(rangeProof R) ([]byte, error) {
					proofBytes, err := g.rangeProofMarshaler.Marshal(rangeProof)
					if err != nil {
						return nil, err
					}

					return proto.Marshal(&pb.GetChangeProofResponse{
						Response: &pb.GetChangeProofResponse_RangeProof{
							RangeProof: proofBytes,
						},
					})
				},
			)
			if err != nil {
				return nil, &common.AppError{
					Code:    p2p.ErrUnexpected.Code,
					Message: fmt.Sprintf("failed to get range proof: %s", err),
				}
			}

			return proofBytes, nil
		}

		// We generated a change proof. See if it's small enough.
		changeProofBytes, err := g.changeProofMarshaler.Marshal(changeProof)
		if err != nil {
			return nil, &common.AppError{
				Code:    p2p.ErrUnexpected.Code,
				Message: fmt.Sprintf("failed to marshal change proof: %s", err),
			}
		}
		responseBytes, err := proto.Marshal(&pb.GetChangeProofResponse{
			Response: &pb.GetChangeProofResponse_ChangeProof{
				ChangeProof: changeProofBytes,
			},
		})
		if err != nil {
			return nil, &common.AppError{
				Code:    p2p.ErrUnexpected.Code,
				Message: fmt.Sprintf("failed to marshal change proof: %s", err),
			}
		}

		if len(responseBytes) < bytesLimit {
			return responseBytes, nil
		}

		// The proof was too large. Try to shrink it.
		keyLimit /= 2
	}

	return nil, &common.AppError{
		Code:    p2p.ErrUnexpected.Code,
		Message: fmt.Sprintf("failed to generate proof: %s", ErrMinProofSizeIsTooLarge),
	}
}

func NewGetRangeProofHandler[R any, C any](db DB[R, C], rangeProofMarshaler Marshaler[R]) *GetRangeProofHandler[R, C] {
	return &GetRangeProofHandler[R, C]{
		db:                  db,
		rangeProofMarshaler: rangeProofMarshaler,
	}
}

type GetRangeProofHandler[R any, C any] struct {
	db                  DB[R, C]
	rangeProofMarshaler Marshaler[R]
}

func (*GetRangeProofHandler[_, _]) AppGossip(context.Context, ids.NodeID, []byte) {}

func (g *GetRangeProofHandler[R, _]) AppRequest(ctx context.Context, _ ids.NodeID, _ time.Time, requestBytes []byte) ([]byte, *common.AppError) {
	req := &pb.GetRangeProofRequest{}
	if err := proto.Unmarshal(requestBytes, req); err != nil {
		return nil, &common.AppError{
			Code:    p2p.ErrUnexpected.Code,
			Message: fmt.Sprintf("failed to unmarshal request: %s", err),
		}
	}

	if err := validateRangeProofRequest(req); err != nil {
		return nil, &common.AppError{
			Code:    p2p.ErrUnexpected.Code,
			Message: fmt.Sprintf("invalid range proof request: %s", err),
		}
	}

	// override limits if they exceed caps
	req.KeyLimit = min(req.KeyLimit, MaxKeyValuesLimit)
	req.BytesLimit = min(req.BytesLimit, maxByteSizeLimit)

	proofBytes, err := getRangeProof(
		ctx,
		g.db,
		req,
		func(rangeProof R) ([]byte, error) {
			return g.rangeProofMarshaler.Marshal(rangeProof)
		},
	)
	if err != nil {
		return nil, &common.AppError{
			Code:    p2p.ErrUnexpected.Code,
			Message: fmt.Sprintf("failed to get range proof: %s", err),
		}
	}

	return proofBytes, nil
}

// Get the range proof specified by [req].
// If the generated proof is too large, the key limit is reduced
// and the proof is regenerated. This process is repeated until
// the proof is smaller than [req.BytesLimit].
// When a sufficiently small proof is generated, returns it.
// If no sufficiently small proof can be generated, returns [ErrMinProofSizeIsTooLarge].
// TODO improve range proof generation so we don't need to iteratively
// reduce the key limit.
func getRangeProof[R any, C any](
	ctx context.Context,
	db DB[R, C],
	req *pb.GetRangeProofRequest,
	marshalFunc func(R) ([]byte, error),
) ([]byte, error) {
	root, err := ids.ToID(req.RootHash)
	if err != nil {
		return nil, err
	}

	keyLimit := int(req.KeyLimit)

	for keyLimit > 0 {
		rangeProof, err := db.GetRangeProofAtRoot(
			ctx,
			root,
			protoutils.ProtoToMaybe(req.StartKey),
			protoutils.ProtoToMaybe(req.EndKey),
			keyLimit,
		)
		if err != nil {
			if errors.Is(err, ErrInsufficientHistory) {
				return nil, nil // drop request
			}
			return nil, err
		}

		proofBytes, err := marshalFunc(rangeProof)
		if err != nil {
			return nil, err
		}

		if len(proofBytes) < int(req.BytesLimit) {
			return proofBytes, nil
		}

		// The proof was too large. Try to shrink it.
		keyLimit /= 2
	}
	return nil, ErrMinProofSizeIsTooLarge
}

// Returns nil iff [req] is well-formed.
func validateChangeProofRequest(req *pb.GetChangeProofRequest) error {
	switch {
	case req.BytesLimit == 0:
		return errInvalidBytesLimit
	case req.KeyLimit == 0:
		return errInvalidKeyLimit
	case len(req.StartRootHash) != hashing.HashLen:
		return errInvalidStartRootHash
	case len(req.EndRootHash) != hashing.HashLen:
		return errInvalidEndRootHash
	case bytes.Equal(req.EndRootHash, ids.Empty[:]):
		return errEmptyProof
	case req.StartKey != nil && req.EndKey != nil && bytes.Compare(req.StartKey.Value, req.EndKey.Value) > 0:
		return errInvalidBounds
	default:
		return nil
	}
}

// Returns nil iff [req] is well-formed.
func validateRangeProofRequest(req *pb.GetRangeProofRequest) error {
	switch {
	case req.BytesLimit == 0:
		return errInvalidBytesLimit
	case req.KeyLimit == 0:
		return errInvalidKeyLimit
	case len(req.RootHash) != ids.IDLen:
		return errInvalidRootHash
	case bytes.Equal(req.RootHash, ids.Empty[:]):
		return errEmptyProof
	case req.StartKey != nil && req.EndKey != nil && bytes.Compare(req.StartKey.Value, req.EndKey.Value) > 0:
		return errInvalidBounds
	default:
		return nil
	}
}
