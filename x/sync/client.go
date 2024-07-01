// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sync

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/utils/maybe"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/x/merkledb"
)

const (
	initialRetryWait = 10 * time.Millisecond
	maxRetryWait     = time.Second
	retryWaitFactor  = 1.5 // Larger --> timeout grows more quickly
)

var (
	errInvalidRangeProof             = errors.New("failed to verify range proof")
	errInvalidChangeProof            = errors.New("failed to verify change proof")
	errTooManyKeys                   = errors.New("response contains more than requested keys")
	errTooManyBytes                  = errors.New("response contains more than requested bytes")
	errUnexpectedChangeProofResponse = errors.New("unexpected response type")
)

type Client interface {
	AppRequest(
		ctx context.Context,
		nodeIDs set.Set[ids.NodeID],
		appRequestBytes []byte,
		onResponse p2p.AppResponseCallback,
	) error
	AppRequestAny(
		ctx context.Context,
		appRequestBytes []byte,
		onResponse p2p.AppResponseCallback,
	) error
}

// Verify [rangeProof] is a valid range proof for keys in [start, end] for
// root [rootBytes]. Returns [errTooManyKeys] if the response contains more
// than [keyLimit] keys.
func verifyRangeProof(
	ctx context.Context,
	rangeProof *merkledb.RangeProof,
	keyLimit int,
	start maybe.Maybe[[]byte],
	end maybe.Maybe[[]byte],
	rootBytes []byte,
	tokenSize int,
	hasher merkledb.Hasher,
) error {
	root, err := ids.ToID(rootBytes)
	if err != nil {
		return err
	}

	// Ensure the response does not contain more than the maximum requested number of leaves.
	if len(rangeProof.KeyValues) > keyLimit {
		return fmt.Errorf(
			"%w: (%d) > %d)",
			errTooManyKeys, len(rangeProof.KeyValues), keyLimit,
		)
	}

	if err := rangeProof.Verify(
		ctx,
		start,
		end,
		root,
		tokenSize,
		hasher,
	); err != nil {
		return fmt.Errorf("%w due to %w", errInvalidRangeProof, err)
	}
	return nil
}

func calculateBackoff(attempt int) time.Duration {
	if attempt == 0 {
		return 0
	}

	retryWait := initialRetryWait * time.Duration(math.Pow(retryWaitFactor, float64(attempt)))
	if retryWait > maxRetryWait {
		retryWait = maxRetryWait
	}

	return retryWait
}
