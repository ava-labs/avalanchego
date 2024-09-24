// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
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
	"github.com/ava-labs/avalanchego/utils/logging"
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

	_ p2p.Handler = (*SyncGetChangeProofHandler)(nil)
	_ p2p.Handler = (*SyncGetRangeProofHandler)(nil)
)

func maybeBytesToMaybe(mb *pb.MaybeBytes) maybe.Maybe[[]byte] {
	if mb != nil && !mb.IsNothing {
		return maybe.Some(mb.Value)
	}
	return maybe.Nothing[[]byte]()
}

func NewSyncGetChangeProofHandler(log logging.Logger, db DB) *SyncGetChangeProofHandler {
	return &SyncGetChangeProofHandler{
		log: log,
		db:  db,
	}
}

type SyncGetChangeProofHandler struct {
	log logging.Logger
	db  DB
}

func (*SyncGetChangeProofHandler) AppGossip(context.Context, ids.NodeID, []byte) {}

func (s *SyncGetChangeProofHandler) AppRequest(ctx context.Context, _ ids.NodeID, _ time.Time, requestBytes []byte) ([]byte, *common.AppError) {
	request := &pb.SyncGetChangeProofRequest{}
	if err := proto.Unmarshal(requestBytes, request); err != nil {
		return nil, &common.AppError{
			Code:    p2p.ErrUnexpected.Code,
			Message: fmt.Sprintf("failed to unmarshal request: %s", err),
		}
	}

	if err := validateChangeProofRequest(request); err != nil {
		return nil, &common.AppError{
			Code:    p2p.ErrUnexpected.Code,
			Message: fmt.Sprintf("invalid request: %s", err),
		}
	}

	// override limits if they exceed caps
	var (
		keyLimit   = min(request.KeyLimit, maxKeyValuesLimit)
		bytesLimit = min(int(request.BytesLimit), maxByteSizeLimit)
		start      = maybeBytesToMaybe(request.StartKey)
		end        = maybeBytesToMaybe(request.EndKey)
	)

	startRoot, err := ids.ToID(request.StartRootHash)
	if err != nil {
		return nil, &common.AppError{
			Code:    p2p.ErrUnexpected.Code,
			Message: fmt.Sprintf("failed to parse start root hash: %s", err),
		}
	}

	endRoot, err := ids.ToID(request.EndRootHash)
	if err != nil {
		return nil, &common.AppError{
			Code:    p2p.ErrUnexpected.Code,
			Message: fmt.Sprintf("failed to parse end root hash: %s", err),
		}
	}

	for keyLimit > 0 {
		changeProof, err := s.db.GetChangeProof(ctx, startRoot, endRoot, start, end, int(keyLimit))
		if err != nil {
			if !errors.Is(err, merkledb.ErrInsufficientHistory) {
				// We should only fail to get a change proof if we have insufficient history.
				// Other errors are unexpected.
				return nil, &common.AppError{
					Code:    p2p.ErrUnexpected.Code,
					Message: fmt.Sprintf("failed to get change proof: %s", err),
				}
			}
			if errors.Is(err, merkledb.ErrNoEndRoot) {
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
				s.db,
				&pb.SyncGetRangeProofRequest{
					RootHash:   request.EndRootHash,
					StartKey:   request.StartKey,
					EndKey:     request.EndKey,
					KeyLimit:   request.KeyLimit,
					BytesLimit: request.BytesLimit,
				},
				func(rangeProof *merkledb.RangeProof) ([]byte, error) {
					return proto.Marshal(&pb.SyncGetChangeProofResponse{
						Response: &pb.SyncGetChangeProofResponse_RangeProof{
							RangeProof: rangeProof.ToProto(),
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
		proofBytes, err := proto.Marshal(&pb.SyncGetChangeProofResponse{
			Response: &pb.SyncGetChangeProofResponse_ChangeProof{
				ChangeProof: changeProof.ToProto(),
			},
		})
		if err != nil {
			return nil, &common.AppError{
				Code:    p2p.ErrUnexpected.Code,
				Message: fmt.Sprintf("failed to marshal change proof: %s", err),
			}
		}

		if len(proofBytes) < bytesLimit {
			return proofBytes, nil
		}

		// The proof was too large. Try to shrink it.
		keyLimit = uint32(len(changeProof.KeyChanges)) / 2
	}

	return nil, &common.AppError{
		Code:    p2p.ErrUnexpected.Code,
		Message: fmt.Sprintf("failed to generate proof: %s", ErrMinProofSizeIsTooLarge),
	}
}

func (*SyncGetChangeProofHandler) CrossChainAppRequest(context.Context, ids.ID, time.Time, []byte) ([]byte, error) {
	return nil, nil
}

func NewSyncGetRangeProofHandler(log logging.Logger, db DB) *SyncGetRangeProofHandler {
	return &SyncGetRangeProofHandler{
		log: log,
		db:  db,
	}
}

type SyncGetRangeProofHandler struct {
	log logging.Logger
	db  DB
}

func (*SyncGetRangeProofHandler) AppGossip(context.Context, ids.NodeID, []byte) {}

func (s *SyncGetRangeProofHandler) AppRequest(ctx context.Context, _ ids.NodeID, _ time.Time, requestBytes []byte) ([]byte, *common.AppError) {
	request := &pb.SyncGetRangeProofRequest{}
	if err := proto.Unmarshal(requestBytes, request); err != nil {
		return nil, &common.AppError{
			Code:    p2p.ErrUnexpected.Code,
			Message: fmt.Sprintf("failed to unmarshal request: %s", err),
		}
	}

	if err := validateRangeProofRequest(request); err != nil {
		return nil, &common.AppError{
			Code:    p2p.ErrUnexpected.Code,
			Message: fmt.Sprintf("invalid range proof request: %s", err),
		}
	}

	// override limits if they exceed caps
	request.KeyLimit = min(request.KeyLimit, maxKeyValuesLimit)
	request.BytesLimit = min(request.BytesLimit, maxByteSizeLimit)

	proofBytes, err := getRangeProof(
		ctx,
		s.db,
		request,
		func(rangeProof *merkledb.RangeProof) ([]byte, error) {
			return proto.Marshal(rangeProof.ToProto())
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

func (*SyncGetRangeProofHandler) CrossChainAppRequest(context.Context, ids.ID, time.Time, []byte) ([]byte, error) {
	return nil, nil
}

// Get the range proof specified by [req].
// If the generated proof is too large, the key limit is reduced
// and the proof is regenerated. This process is repeated until
// the proof is smaller than [req.BytesLimit].
// When a sufficiently small proof is generated, returns it.
// If no sufficiently small proof can be generated, returns [ErrMinProofSizeIsTooLarge].
// TODO improve range proof generation so we don't need to iteratively
// reduce the key limit.
func getRangeProof(
	ctx context.Context,
	db DB,
	req *pb.SyncGetRangeProofRequest,
	marshalFunc func(*merkledb.RangeProof) ([]byte, error),
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
			maybeBytesToMaybe(req.StartKey),
			maybeBytesToMaybe(req.EndKey),
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

		if len(proofBytes) < int(req.BytesLimit) {
			return proofBytes, nil
		}

		// The proof was too large. Try to shrink it.
		keyLimit = len(rangeProof.KeyValues) / 2
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
	case bytes.Equal(req.EndRootHash, ids.Empty[:]):
		return merkledb.ErrEmptyProof
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
	case bytes.Equal(req.RootHash, ids.Empty[:]):
		return merkledb.ErrEmptyProof
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
