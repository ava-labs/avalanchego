package platformvm

import (
	"errors"

	"github.com/ava-labs/gecko/vms/components/verify"

	"github.com/ava-labs/gecko/database"
	"github.com/ava-labs/gecko/database/versiondb"
	"github.com/ava-labs/gecko/vms/components/ava"
)

var errBaseTxUninitialized = errors.New("BaseTx is uninitialized")

// ProposalTx is an operation that can be proposed
type ProposalTx interface {
	initialize(vm *VM) error
	// Attempts to verify this transaction with the provided state.
	SemanticVerify(database.Database) (
		onCommitDB *versiondb.Database,
		onAbortDB *versiondb.Database,
		onCommitFunc func(),
		onAbortFunc func(),
		err TxError,
	)
	InitiallyPrefersCommit() bool
}

// BaseProposalTx is embedded in structs that implement ProposalTx
type BaseProposalTx struct {
	*BaseTx       `serialize:"true"`
	OnCommitIns   []*ava.TransferableInput  `serialize:"true"`
	OnCommitOuts  []*ava.TransferableOutput `serialize:"true"`
	OnCommitCreds []verify.Verifiable       `serialize:"true"`
	OnAbortIns    []*ava.TransferableInput  `serialize:"true"`
	OnAbortOuts   []*ava.TransferableOutput `serialize:"true"`
	OnAbortCreds  []verify.Verifiable       `serialize:"true"`
}

// // Outs returns this transaction's outputs
// func (tx *BaseProposalTx) Outs() []*ava.TransferableOutput {
// 	return tx.Outs
// }

// SyntacticVerify returns nil iff this tx is well formed
func (tx *BaseProposalTx) SyntacticVerify() error {
	if tx.BaseTx == nil {
		return errBaseTxUninitialized
	} else if err := tx.BaseTx.SyntacticVerify(); err != nil {
		return err
	}
	return nil
}
