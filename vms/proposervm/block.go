// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

// ProposerBlock is a decorator for a snowman.Block, created to handle block headers introduced with snowman++
// ProposerBlock is made up of a ProposerBlockHeader, carrying all the new fields introduced with snowman++, and
// a core block, which is a snowman.Block.
// ProposerBlock serialization is a two step process: the header is serialized at proposervm level, while core block
// serialization is deferred to the core VM. The structure marshallingProposerBLock encapsulates
// the serialization logic
// Contract:
// * Parent ProposerBlock wraps Parent CoreBlock of CoreBlock wrapped into Child ProposerBlock.
// * Only one call to each coreBlock's Verify() is issued from proposerVM. However Verify is memory only, so we won't persist
// core blocks over which Verify has been called
// * VERIFY FAILS ON GENESIS TODO: fix maybe
// * Rejection of ProposerBlock does not constitute Rejection of wrapped CoreBlock

import (
	"errors"
	"time"

	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
)

const (
	// allowable block issuance in the future
	syncBound = 10 * time.Second
)

var (
	errUnsignedChild            = errors.New("expected child to be signed")
	errInnerParentMismatch      = errors.New("inner parentID didn't match expected parent")
	errTimeNotMonotonic         = errors.New("time must monotonically increase")
	errPChainHeightNotMonotonic = errors.New("p chain height must monotonically increase")
	errTimeTooAdvanced          = errors.New("time is too far advanced")
	errProposerWindowNotStarted = errors.New("proposer window hasn't started")

	errProposersNotActivated = errors.New("proposers haven't been activated yet")
	errProposersActivated    = errors.New("proposers have been activated")
)

type Block interface {
	snowman.Block

	verifyPreForkChild(child *preForkBlock) error
	verifyPostForkChild(child *postForkBlock) error

	buildChild(innerBlock snowman.Block) (Block, error)
}
