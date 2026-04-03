// Copyright (C) 2025-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blocks

import (
	"errors"
	"fmt"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/rpc"

	"github.com/ava-labs/strevm/types"
)

type (
	// A Chain provides access to the full life cycle of a [Block].
	Chain interface {
		ConsensusCritical
		Frontier
		DB() ethdb.Database
		XDB() types.ExecutionResults
	}

	// ConsensusCritical blocks are currently in use by a consensus mechanism,
	// at any point in the life cycle, including recently rejected. There is no
	// guarantee of access to every [Block] being used by consensus nor is there
	// a guarantee that a returned [Block] is still being used, but every
	// returned [Block] MUST be treated as consensus-critical.
	//
	// A ConsensusCritical source can be thought of as a cache of ready-made
	// [Block] instances, and consumers SHOULD treat return values as read-only.
	// Sources MUST be thread-safe and every returned [Block] MUST uphold all
	// life-cycle invariants.
	ConsensusCritical interface {
		ConsensusCriticalBlock(common.Hash) (*Block, bool)
	}

	// The Frontier is a thread-safe view of all life-cycle stages of a [Block].
	Frontier interface {
		AcceptanceFrontier
		ExecutionFrontier
		SettlementFrontier
	}
	// The AcceptanceFrontier is a thread-safe view of the last accepted
	// [Block].
	AcceptanceFrontier interface {
		LastAccepted() *Block
	}
	// The ExecutionFrontier is a thread-safe view of the last executed [Block].
	ExecutionFrontier interface {
		LastExecuted() *Block
	}
	// The SettlementFrontier is a thread-safe view of the last settled [Block].
	SettlementFrontier interface {
		LastSettled() *Block
	}
)

// ErrNotFound is returned by the Resolve*() and From*() functions when the
// requested [Block] could not be discerned or loaded, respectively.
//
// Note that the From*() functions DO NOT intercept return arguments. If a
// [DBReaderWithErr] returns `nil, nil`—as is common in geth when the `T` is not
// found—then so too will the From*() function return a nil error even though
// ErrNotFound is more appropriate. This pattern MUST NOT be considered as
// precedence as it is a foot gun and MUST be limited to, and contained within,
// packages in which it is idiomatic behaviour because of reliance on equivalent
// geth functionality.
var ErrNotFound = errors.New("block not found")

// ErrFutureBlockNotResolved is a specific case of [ErrNotFound], which it
// wraps, returned when attempting to resolve an [rpc.BlockNumber] not yet
// accepted by consensus. Such numbers are ambiguous as there may be more than
// one matching [Block].
var ErrFutureBlockNotResolved = fmt.Errorf("%w: not accepted yet", ErrNotFound)

// ErrNonCanonicalBlock is a specific case of [ErrFutureBlockNotResolved], which
// it wraps, returned when an unambiguous [Block] could be resolved (by its
// hash), but (a) it has not yet been accepted by consensus; and (b) the
// [rpc.BlockNumberOrHash.RequireCanonical] field was set to `true`.
var ErrNonCanonicalBlock = fmt.Errorf("%w: canonical block required", ErrFutureBlockNotResolved)

// ResolveRPCNumber converts an [rpc.BlockNumber] into the specific number of
// the corresponding block, treating named blocks as relative to execution:
//
//   - [rpc.PendingBlockNumber] is that returned by the [AcceptanceFrontier],
//     its execution status being unknown but eventually guaranteed.
//   - [rpc.LatestBlockNumber] is that returned by the [ExecutionFrontier].
//   - [rpc.SafeBlockNumber] and [rpc.FinalizedBlockNumber] are both that
//     returned by the [SettlementFrontier].
//
// Explicit block numbers are returned unchanged, as long as a corresponding
// [Block] has been accepted, otherwise [ErrFutureBlockNotResolved] is returned
// instead.
//
// # Important note re finality
//
// "Safe" and "finalized", as being separate to "last", are both Ethereum
// concepts with imperfect translation to SAE. Finality is the property of a
// canonical block being permanently so, without the possibility of re-org,
// which occurs immediately at acceptance under SAE. In Ethereum, "safety" is an
// interim stage towards finality, with greater but still imperfect guarantees.
//
// In keeping with "pending" and "latest", the point of reference for safety is
// relative to execution results, and the only non-determinism that can arise is
// due to (very rare) disk corruption. Settlement demonstrates consensus of
// execution results, such that they can be considered "safe" in the lay sense
// of the word. The "finalized" block is defined identically because we wish to
// maintain monotonicity of the labels, and no further guarantees are possible
// after settlement.
func ResolveRPCNumber(f Frontier, bn rpc.BlockNumber) (uint64, error) {
	tip := f.LastAccepted().Height()

	switch bn {
	case rpc.PendingBlockNumber:
		return tip, nil
	case rpc.LatestBlockNumber:
		return f.LastExecuted().Height(), nil
	case rpc.SafeBlockNumber, rpc.FinalizedBlockNumber:
		return f.LastSettled().Height(), nil
	}

	if bn < 0 {
		return 0, fmt.Errorf("%s block unsupported", bn.String())
	}
	n := uint64(bn) //nolint:gosec // Non-negative check performed above
	if n > tip {
		return 0, fmt.Errorf("%w: block %d", ErrFutureBlockNotResolved, n)
	}
	return n, nil
}

// Errors returned when resolving an invalid [rpc.BlockNumberOrHash].
var (
	ErrNeitherNumberNorHash = fmt.Errorf("%T carrying neither number nor hash", rpc.BlockNumberOrHash{})
	ErrBothNumberAndHash    = fmt.Errorf("%T carrying both number and hash", rpc.BlockNumberOrHash{})
)

// ResolveRPCNumberOrHash converts an [rpc.BlockNumberOrHash] into the specific
// number and hash of the corresponding [Block]. See [ResolveRPCNumber] for
// treatment of named block numbers.
//
// The [Block] with the returned [common.Hash] is guaranteed to have the
// returned number but is only guaranteed to be the canonical block of said
// number if (a) it was specified by [rpc.BlockNumberOrHash.Number], or (b) the
// [rpc.BlockNumberOrHash.RequireCanonical] field is `true`.
func ResolveRPCNumberOrHash(c Chain, numOrHash rpc.BlockNumberOrHash) (uint64, common.Hash, error) {
	rpcNum, isNum := numOrHash.Number()
	hash, isHash := numOrHash.Hash()

	switch {
	case isNum && isHash:
		return 0, common.Hash{}, ErrBothNumberAndHash

	case isNum:
		num, err := ResolveRPCNumber(c, rpcNum)
		if err != nil {
			return 0, common.Hash{}, err
		}
		// [ResolveRPCNumber] is documented as only returning canonical blocks,
		// so we don't need to check for a zero hash.
		return num, rawdb.ReadCanonicalHash(c.DB(), num), nil

	case isHash:
		if bl, ok := c.ConsensusCriticalBlock(hash); ok {
			n := bl.NumberU64()
			// TODO(JonathanOppenheimer): avoid the DB read to confirm if canonical
			if numOrHash.RequireCanonical && hash != rawdb.ReadCanonicalHash(c.DB(), n) {
				return 0, common.Hash{}, fmt.Errorf("%w: hash %#x", ErrNonCanonicalBlock, hash)
			}
			return n, hash, nil
		}

		numPtr := rawdb.ReadHeaderNumber(c.DB(), hash)
		if numPtr == nil {
			return 0, common.Hash{}, fmt.Errorf("%w: hash %#x", ErrNotFound, hash)
		}
		// We only write canonical blocks to the database so there's no need to
		// perform a check.
		return *numPtr, hash, nil

	default:
		return 0, common.Hash{}, ErrNeitherNumberNorHash
	}
}

type (
	// A DBReader returns any block-related artefact from the database. It is
	// typically one of the [rawdb] `Read*()` functions.
	DBReader[T any] func(db ethdb.Reader, hash common.Hash, num uint64) *T
	// A DBReaderWithErr is equivalent to a [DBReader] except that it MAY return
	// an error.
	DBReaderWithErr[T any] func(db ethdb.Reader, hash common.Hash, num uint64) (*T, error)
	// An Extractor returns any artefact from a [Block]. It is typically one of
	// the type's nulladic methods.
	Extractor[T any] func(*Block) *T
)

// WithNilErr converts the [DBReader] into a [DBReaderWithErr] that always
// returns a nil error.
func (r DBReader[T]) WithNilErr() DBReaderWithErr[T] {
	return func(db ethdb.Reader, h common.Hash, n uint64) (*T, error) {
		return r(db, h, n), nil
	}
}

// FromNumber resolves the canonical [Block] for the given [rpc.BlockNumber]
// and returns the result of calling `fromDB` with its number and hash.
func FromNumber[T any](c Chain, n rpc.BlockNumber, fromDB DBReaderWithErr[T]) (*T, error) {
	num, err := ResolveRPCNumber(c, n)
	if err != nil {
		return nil, err
	}
	return fromDB(c.DB(), rawdb.ReadCanonicalHash(c.DB(), num), num)
}

// FromHash returns `fromConsensus()` if a [Block] with the specified hash is
// returned by the [ConsensusCritical] method of the [Chain], otherwise it returns
// `fromDB()` i.f.f. the block was previously accepted. If `fromDB()` is called
// then the block is guaranteed to exist if read with [rawdb] functions.
func FromHash[T any](c Chain, hash common.Hash, requireCanonical bool, fromConsensus Extractor[T], fromDB DBReaderWithErr[T]) (*T, error) {
	if blk, ok := c.ConsensusCriticalBlock(hash); ok {
		// TODO(JonathanOppenheimer): avoid the DB read to confirm if canonical
		if requireCanonical && hash != rawdb.ReadCanonicalHash(c.DB(), blk.NumberU64()) {
			return nil, fmt.Errorf("%w: hash %#x", ErrNonCanonicalBlock, hash)
		}
		return fromConsensus(blk), nil
	}
	num := rawdb.ReadHeaderNumber(c.DB(), hash)
	if num == nil {
		return nil, ErrNotFound
	}
	return fromDB(c.DB(), hash, *num)
}

// FromNumberOrHash resolves the [Block] for the given [rpc.BlockNumberOrHash]
// and returns the result of `fromConsensus` or `fromDB`, preferring the former.
// See [ResolveRPCNumberOrHash] for canonicality guarantees.
func FromNumberOrHash[T any](c Chain, blockNrOrHash rpc.BlockNumberOrHash, fromConsensus Extractor[T], fromDB DBReaderWithErr[T]) (*T, error) {
	n, hash, err := ResolveRPCNumberOrHash(c, blockNrOrHash)
	if err != nil {
		return nil, err
	}
	if blk, ok := c.ConsensusCriticalBlock(hash); ok {
		return fromConsensus(blk), nil
	}
	return fromDB(c.DB(), hash, n)
}

// FromNumberAndHash behaves like [FromNumberOrHash] except that it accepts both
// the number and hash as separate, required arguments. It verifies that any
// [Block] found in consensus has the expected number, and disallows named block
// numbers such as [rpc.LatestBlockNumber].
//
// Unlike [FromNumberOrHash], there are no canonicality guarantees as the
// provision of a hash resolves the ambiguity described in the comment on
// [ErrFutureBlockNotResolved].
func FromNumberAndHash[T any](c Chain, hash common.Hash, rpcNum rpc.BlockNumber, fromConsensus Extractor[T], fromDB DBReaderWithErr[T]) (*T, error) {
	if hash == (common.Hash{}) {
		return nil, errors.New("empty block hash")
	}
	if rpcNum < 0 {
		return nil, errors.New("named blocks not supported")
	}
	n := uint64(rpcNum) //nolint:gosec // Non-negative check performed above
	if b, ok := c.ConsensusCriticalBlock(hash); ok {
		if b.NumberU64() != n {
			return nil, fmt.Errorf("%w: found block number %d for hash %#x, expected %d", ErrNotFound, b.NumberU64(), hash, n)
		}
		return fromConsensus(b), nil
	}
	return fromDB(c.DB(), hash, n)
}
