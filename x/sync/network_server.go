// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sync

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
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
)

func maybeBytesToMaybe(mb *pb.MaybeBytes) maybe.Maybe[[]byte] {
	if mb != nil && !mb.IsNothing {
		return maybe.Some(mb.Value)
	}
	return maybe.Nothing[[]byte]()
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
