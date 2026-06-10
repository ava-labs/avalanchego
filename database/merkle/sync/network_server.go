// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sync

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/protobuf/proto"

	"github.com/ava-labs/avalanchego/database/merkle/sync/protoutils"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/units"

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
	_ p2p.Handler = (*ProofHandler[any, any])(nil)

	errMinProofSizeIsTooLarge = errors.New("cannot generate any proof within the requested limit")

	errInvalidBytesLimit    = errors.New("bytes limit must be greater than 0")
	errInvalidKeyLimit      = errors.New("key limit must be greater than 0")
	errInvalidStartRootHash = fmt.Errorf("start root hash must have length %d", hashing.HashLen)
	errInvalidEndRootHash   = fmt.Errorf("end root hash must have length %d", hashing.HashLen)
	errInvalidBounds        = errors.New("start key is greater than end key")
	errInvalidRootHash      = fmt.Errorf("root hash must have length %d", hashing.HashLen)
	errEmptyProof           = errors.New("proof for empty trie requested")
)

func NewProofHandler[R any, C any](
	db DB[R, C],
	rangeProofMarshaler Marshaler[R],
	changeProofMarshaler Marshaler[C],
	registerer prometheus.Registerer,
) (*ProofHandler[R, C], error) {
	metrics, err := newHandlerMetrics("sync", registerer)
	if err != nil {
		return nil, err
	}
	return &ProofHandler[R, C]{
		db:                   db,
		rangeProofMarshaler:  rangeProofMarshaler,
		changeProofMarshaler: changeProofMarshaler,
		metrics:              metrics,
	}, nil
}

type ProofHandler[R any, C any] struct {
	db                   DB[R, C]
	rangeProofMarshaler  Marshaler[R]
	changeProofMarshaler Marshaler[C]
	metrics              *handlerMetrics
}

func (*ProofHandler[_, _]) AppGossip(context.Context, ids.NodeID, []byte) {}

func (h *ProofHandler[R, C]) AppRequest(ctx context.Context, _ ids.NodeID, _ time.Time, requestBytes []byte) ([]byte, *common.AppError) {
	req := &pb.ProofRequest{}
	if err := proto.Unmarshal(requestBytes, req); err != nil {
		return nil, &common.AppError{
			Code:    p2p.ErrUnexpected.Code,
			Message: fmt.Sprintf("failed to unmarshal request: %s", err),
		}
	}

	var (
		resp []byte
		err  error
	)
	switch r := req.Request.(type) {
	case *pb.ProofRequest_RangeProof:
		resp, err = h.handleRangeProofRequest(ctx, r.RangeProof)
	case *pb.ProofRequest_ChangeProof:
		resp, err = h.handleChangeProofRequest(ctx, r.ChangeProof)
	default:
		err = fmt.Errorf("unknown request type: %T", r)
	}
	if err != nil {
		return nil, &common.AppError{
			Code:    p2p.ErrUnexpected.Code,
			Message: fmt.Sprintf("failed to handle request: %s", err),
		}
	}
	return resp, nil
}

func (h *ProofHandler[R, C]) handleRangeProofRequest(ctx context.Context, req *pb.RangeProofRequest) ([]byte, error) {
	if err := validateRangeProofRequest(req); err != nil {
		return nil, err
	}

	// override limits if they exceed caps
	var (
		keyLimit   = min(int(req.KeyLimit), MaxKeyValuesLimit)
		bytesLimit = min(req.BytesLimit, maxByteSizeLimit)
		startKey   = protoutils.ProtoToMaybe(req.StartKey)
		endKey     = protoutils.ProtoToMaybe(req.EndKey)
	)

	root, err := ids.ToID(req.RootHash)
	if err != nil {
		return nil, err
	}

	for keyLimit > 0 {
		generationStart := time.Now()
		rangeProof, err := h.db.GetRangeProofAtRoot(
			ctx,
			root,
			startKey,
			endKey,
			keyLimit,
		)
		h.metrics.proofGenerated(proofTypeRange, time.Since(generationStart), err)
		if err != nil {
			if errors.Is(err, ErrInsufficientHistory) {
				return nil, nil // drop request
			}
			return nil, err
		}

		innerBytes, err := h.rangeProofMarshaler.Marshal(rangeProof)
		if err != nil {
			return nil, err
		}

		proofBytes, err := proto.Marshal(&pb.ProofResponse{
			Response: &pb.ProofResponse_RangeProof{
				RangeProof: innerBytes,
			},
		})
		if err != nil {
			return nil, err
		}

		if len(proofBytes) < int(bytesLimit) {
			h.metrics.proofServed(proofTypeRange, len(proofBytes))
			return proofBytes, nil
		}
		// The proof was too large. Try to shrink it.
		keyLimit /= 2
		h.metrics.proofShrunk(proofTypeRange, keyLimit)
	}

	return nil, errMinProofSizeIsTooLarge
}

func (h *ProofHandler[R, C]) handleChangeProofRequest(ctx context.Context, req *pb.ChangeProofRequest) ([]byte, error) {
	if err := validateChangeProofRequest(req); err != nil {
		return nil, err
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
		return nil, err
	}

	endRoot, err := ids.ToID(req.EndRootHash)
	if err != nil {
		return nil, err
	}

	for keyLimit > 0 {
		generationStart := time.Now()
		changeProof, err := h.db.GetChangeProof(ctx, startRoot, endRoot, start, end, int(keyLimit))
		h.metrics.proofGenerated(proofTypeChange, time.Since(generationStart), err)
		if err != nil {
			if !errors.Is(err, ErrInsufficientHistory) {
				// We should only fail to get a change proof if we have insufficient history.
				// Other errors are unexpected.
				// TODO define custom errors
				return nil, err
			}
			if errors.Is(err, ErrNoEndRoot) {
				// g.db doesn't have endRoot in its history.
				// We can't generate a change or range proof.
				return nil, err
			}

			// g.db doesn't have sufficient history to generate change proof.
			// Generate a range proof for the end root ID instead.
			return h.handleRangeProofRequest(
				ctx,
				&pb.RangeProofRequest{
					RootHash:   req.EndRootHash,
					StartKey:   req.StartKey,
					EndKey:     req.EndKey,
					KeyLimit:   req.KeyLimit,
					BytesLimit: req.BytesLimit,
				},
			)
		}

		// We generated a change proof. See if it's small enough.
		changeProofBytes, err := h.changeProofMarshaler.Marshal(changeProof)
		if err != nil {
			return nil, err
		}
		responseBytes, err := proto.Marshal(&pb.ProofResponse{
			Response: &pb.ProofResponse_ChangeProof{
				ChangeProof: changeProofBytes,
			},
		})
		if err != nil {
			return nil, err
		}

		if len(responseBytes) < bytesLimit {
			h.metrics.proofServed(proofTypeChange, len(responseBytes))
			return responseBytes, nil
		}

		// The proof was too large. Try to shrink it.
		keyLimit /= 2
		h.metrics.proofShrunk(proofTypeChange, int(keyLimit))
	}

	return nil, errMinProofSizeIsTooLarge
}

// Returns nil iff [req] is well-formed.
func validateChangeProofRequest(req *pb.ChangeProofRequest) error {
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
func validateRangeProofRequest(req *pb.RangeProofRequest) error {
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
