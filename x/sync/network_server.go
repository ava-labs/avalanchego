// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
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
	"github.com/ava-labs/avalanchego/utils/maybe"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/x/merkledb"

	pb "github.com/ava-labs/avalanchego/proto/pb/sync"
)

const (
	// Maximum number of key-value pairs to return in a proof.
	// This overrides any other Limit specified in a RangeProofRequest
	// or ChangeProofRequest if the given Limit is greater.
	maxKeyValuesLimit = 2048
	// Estimated max overhead, in bytes, of putting a proof into a message.
	// We use this to ensure that the proof we generate is not too large to fit in a message.
	// TODO: refine this estimate. This is almost certainly a large overestimate.
	estimatedMessageOverhead = 4 * units.KiB
	maxByteSizeLimit         = constants.DefaultMaxMessageSize - estimatedMessageOverhead
)

var (
	ErrMinProofSizeIsTooLarge = errors.New("cannot generate any proof within the requested limit")

	errInvalidBytesLimit    = errors.New("bytes limit must be greater than 0")
	errInvalidKeyLimit      = errors.New("key limit must be greater than 0")
	errInvalidStartRootHash = fmt.Errorf("start root hash must have length %d", hashing.HashLen)
	errInvalidEndRootHash   = fmt.Errorf("end root hash must have length %d", hashing.HashLen)
	errInvalidStartKey      = errors.New("start key is Nothing but has value")
	errInvalidEndKey        = errors.New("end key is Nothing but has value")
	errInvalidBounds        = errors.New("start key is greater than end key")
	errInvalidRootHash      = fmt.Errorf("root hash must have length %d", hashing.HashLen)

	_ ProofCreator = (*merkleProofCreator)(nil)
	_ p2p.Handler  = (*GetChangeProofHandler)(nil)
	_ p2p.Handler  = (*GetRangeProofHandler)(nil)
)

func maybeBytesToMaybe(mb *pb.MaybeBytes) maybe.Maybe[[]byte] {
	if mb != nil && !mb.IsNothing {
		return maybe.Some(mb.Value)
	}
	return maybe.Nothing[[]byte]()
}

type merkleProofCreator struct {
	db DB
}

func NewProofCreator(db DB) ProofCreator {
	return &merkleProofCreator{
		db: db,
	}
}

func NewGetChangeProofHandler(db DB) *GetChangeProofHandler {
	return &GetChangeProofHandler{
		proofCreator: NewProofCreator(db),
	}
}

type GetChangeProofHandler struct {
	proofCreator ProofCreator
}

func (*GetChangeProofHandler) AppGossip(context.Context, ids.NodeID, []byte) {}

func (g *GetChangeProofHandler) AppRequest(ctx context.Context, _ ids.NodeID, _ time.Time, requestBytes []byte) ([]byte, *common.AppError) {
	req := &pb.SyncGetChangeProofRequest{}
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
		keyLimit   = min(req.KeyLimit, maxKeyValuesLimit)
		bytesLimit = min(int(req.BytesLimit), maxByteSizeLimit)
		start      = maybeBytesToMaybe(req.StartKey)
		end        = maybeBytesToMaybe(req.EndKey)
	)

	proofBytes, err := g.proofCreator.ChangeProof(
		ctx,
		req.StartRootHash,
		req.EndRootHash,
		start,
		end,
		int(keyLimit),
		bytesLimit,
	)
	if err != nil {
		return nil, &common.AppError{
			Code:    p2p.ErrUnexpected.Code,
			Message: err.Error(),
		}
	}

	return proofBytes, nil
}

func (pc *merkleProofCreator) ChangeProof(ctx context.Context, startRootBytes []byte, endRootBytes []byte, start maybe.Maybe[[]byte], end maybe.Maybe[[]byte], keyLimit, byteLimit int) ([]byte, error) {
	startRoot, err := ids.ToID(startRootBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse start root hash: %w", err)
	}

	endRoot, err := ids.ToID(endRootBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse end root hash: %w", err)
	}

	// If the end root is empty, the proof will delete everything.
	if endRoot == ids.Empty {
		proofBytes, err := proto.Marshal(&pb.SyncGetChangeProofResponse{
			Response: &pb.SyncGetChangeProofResponse_RangeProof{
				RangeProof: (&merkledb.RangeProof{}).ToProto(),
			},
		})
		if err != nil {
			return nil, &common.AppError{
				Code:    p2p.ErrUnexpected.Code,
				Message: fmt.Sprintf("failed to marshal empty change proof: %s", err),
			}
		}
		return proofBytes, nil
	}

	for keyLimit > 0 {
		changeProof, err := pc.db.GetChangeProof(ctx, startRoot, endRoot, start, end, keyLimit)
		if err != nil {
			if !errors.Is(err, merkledb.ErrInsufficientHistory) {
				// We should only fail to get a change proof if we have insufficient history.
				// Other errors are unexpected.
				// TODO define custom errors
				return nil, fmt.Errorf("failed to get change proof: %w", err)
			}
			if errors.Is(err, merkledb.ErrNoEndRoot) {
				// [merkledb.ErrNoEndRoot] is a special case of [merkledb.ErrInsufficientHistory].
				// [s.db] doesn't have [endRoot] in its history.
				// We can't generate a change/range proof. Drop this request.
				return nil, fmt.Errorf("failed to get change proof: %w", err)
			}

			// [s.db] doesn't have sufficient history to generate change proof.
			// Generate a range proof for the end root ID instead.
			proofBytes, err := pc.rangeProof(
				ctx,
				endRoot,
				start,
				end,
				keyLimit,
				byteLimit,
				func(rangeProof *merkledb.RangeProof) ([]byte, error) {
					return proto.Marshal(&pb.SyncGetChangeProofResponse{
						Response: &pb.SyncGetChangeProofResponse_RangeProof{
							RangeProof: rangeProof.ToProto(),
						},
					})
				},
			)
			if err != nil {
				return nil, fmt.Errorf("failed to get range proof: %w", err)
			}
			return proofBytes, nil
		}

		// We generated a change proof. See if it's small enough.
		proofBytes, err := proto.Marshal(&pb.SyncGetChangeProofResponse{
			Response: &pb.SyncGetChangeProofResponse_ChangeProof{
				ChangeProof: changeProof.ToProto(),
			},
		})
		if err != nil {
			return nil, fmt.Errorf("failed to marshal change proof: %w", err)
		}

		if len(proofBytes) < byteLimit {
			// The proof is small enough. Return it.
			return proofBytes, nil
		}

		keyLimit = len(changeProof.KeyChanges) / 2
	}
	return nil, ErrMinProofSizeIsTooLarge
}

func NewGetRangeProofHandler(db DB) *GetRangeProofHandler {
	return &GetRangeProofHandler{
		pc: NewProofCreator(db),
	}
}

type GetRangeProofHandler struct {
	pc ProofCreator
}

func (*GetRangeProofHandler) AppGossip(context.Context, ids.NodeID, []byte) {}

func (g *GetRangeProofHandler) AppRequest(ctx context.Context, _ ids.NodeID, _ time.Time, requestBytes []byte) ([]byte, *common.AppError) {
	req := &pb.SyncGetRangeProofRequest{}
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
	req.KeyLimit = min(req.KeyLimit, maxKeyValuesLimit)
	req.BytesLimit = min(req.BytesLimit, maxByteSizeLimit)

	proofBytes, err := g.pc.RangeProof(
		ctx,
		req.RootHash,
		maybeBytesToMaybe(req.StartKey),
		maybeBytesToMaybe(req.EndKey),
		int(req.KeyLimit),
		int(req.BytesLimit),
	)
	if err != nil {
		return nil, &common.AppError{
			Code:    p2p.ErrUnexpected.Code,
			Message: err.Error(),
		}
	}

	return proofBytes, nil
}

// Get the range proof specified.
// If the generated proof is too large, the key limit is reduced
// and the proof is regenerated. This process is repeated until
// the proof is smaller than [bytesLimit].
// When a sufficiently small proof is generated, returns it.
// If no sufficiently small proof can be generated, returns [ErrMinProofSizeIsTooLarge].
// TODO improve range proof generation so we don't need to iteratively
// reduce the key limit.
func (pc *merkleProofCreator) RangeProof(ctx context.Context, rootBytes []byte, start maybe.Maybe[[]byte], end maybe.Maybe[[]byte], keyLimit, byteLimit int) ([]byte, error) {
	root, err := ids.ToID(rootBytes)
	if err != nil {
		return nil, err
	}

	proofBytes, err := pc.rangeProof(
		ctx,
		root,
		start,
		end,
		keyLimit,
		byteLimit,
		func(rangeProof *merkledb.RangeProof) ([]byte, error) {
			return proto.Marshal(rangeProof.ToProto())
		},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get range proof: %w", err)
	}
	return proofBytes, nil
}

func (pc *merkleProofCreator) rangeProof(ctx context.Context, root ids.ID, start maybe.Maybe[[]byte], end maybe.Maybe[[]byte], keyLimit, byteLimit int, marshalFunc func(*merkledb.RangeProof) ([]byte, error)) ([]byte, error) {
	if root == ids.Empty {
		// If the root is empty, we can't generate a proof.
		// Return an empty proof.
		return marshalFunc(&merkledb.RangeProof{})
	}

	for keyLimit > 0 {
		rangeProof, err := pc.db.GetRangeProofAtRoot(
			ctx,
			root,
			start,
			end,
			keyLimit,
		)
		if err != nil {
			if errors.Is(err, merkledb.ErrInsufficientHistory) {
				return nil, nil // drop request
			}
			return nil, err
		}

		proofBytes, err := marshalFunc(rangeProof)
		if err != nil {
			return nil, err
		}

		if len(proofBytes) < byteLimit {
			return proofBytes, nil
		}

		// The proof was too large. Try to shrink it.
		keyLimit = len(rangeProof.KeyChanges) / 2
	}
	return nil, ErrMinProofSizeIsTooLarge
}

// Returns nil iff [req] is well-formed.
func validateChangeProofRequest(req *pb.SyncGetChangeProofRequest) error {
	switch {
	case req.BytesLimit == 0:
		return errInvalidBytesLimit
	case req.KeyLimit == 0:
		return errInvalidKeyLimit
	case len(req.StartRootHash) != hashing.HashLen:
		return errInvalidStartRootHash
	case len(req.EndRootHash) != hashing.HashLen:
		return errInvalidEndRootHash
	case req.StartKey != nil && req.StartKey.IsNothing && len(req.StartKey.Value) > 0:
		return errInvalidStartKey
	case req.EndKey != nil && req.EndKey.IsNothing && len(req.EndKey.Value) > 0:
		return errInvalidEndKey
	case req.StartKey != nil && req.EndKey != nil && !req.StartKey.IsNothing &&
		!req.EndKey.IsNothing && bytes.Compare(req.StartKey.Value, req.EndKey.Value) > 0:
		return errInvalidBounds
	default:
		return nil
	}
}

// Returns nil iff [req] is well-formed.
func validateRangeProofRequest(req *pb.SyncGetRangeProofRequest) error {
	switch {
	case req.BytesLimit == 0:
		return errInvalidBytesLimit
	case req.KeyLimit == 0:
		return errInvalidKeyLimit
	case len(req.RootHash) != ids.IDLen:
		return errInvalidRootHash
	case req.StartKey != nil && req.StartKey.IsNothing && len(req.StartKey.Value) > 0:
		return errInvalidStartKey
	case req.EndKey != nil && req.EndKey.IsNothing && len(req.EndKey.Value) > 0:
		return errInvalidEndKey
	case req.StartKey != nil && req.EndKey != nil && !req.StartKey.IsNothing &&
		!req.EndKey.IsNothing && bytes.Compare(req.StartKey.Value, req.EndKey.Value) > 0:
		return errInvalidBounds
	default:
		return nil
	}
}
