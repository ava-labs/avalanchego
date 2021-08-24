package transactions

import (
	"time"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
)

// UnsignedTx is an unsigned transaction
type UnsignedTx interface {
	Initialize(unsignedBytes, signedBytes []byte)
	ID() ids.ID
	UnsignedBytes() []byte
	Bytes() []byte
}

type DecisionTxSyntacticVerificationContext struct {
	Ctx        *snow.Context
	C          codec.Manager
	FeeAmount  uint64
	FeeAssetID ids.ID
}

type UnsignedDecisionTx interface {
	UnsignedTx

	// Attempts to verify this transactions is well formed
	SyntacticVerify(synCtx DecisionTxSyntacticVerificationContext) error
}

type ProposalTxSyntacticVerificationContext struct {
	Ctx               *snow.Context
	C                 codec.Manager
	MinDelegatorStake uint64
	MinValidatorStake uint64
	MaxValidatorStake uint64
	MinStakeDuration  time.Duration
	MaxStakeDuration  time.Duration
	MinDelegationFee  uint32

	FeeAmount  uint64
	FeeAssetID ids.ID
}

type UnsignedProposalTx interface {
	UnsignedTx

	// Attempts to verify this transactions is well formed
	SyntacticVerify(synCtx ProposalTxSyntacticVerificationContext) error
}

type AtomicTxSyntacticVerificationContext struct {
	Ctx        *snow.Context
	C          codec.Manager
	AvmID      ids.ID
	FeeAssetID ids.ID
	FeeAmount  uint64
}

type UnsignedAtomicTx interface {
	UnsignedTx

	// Attempts to verify this transactions is well formed
	SyntacticVerify(synCtx AtomicTxSyntacticVerificationContext) error
}
