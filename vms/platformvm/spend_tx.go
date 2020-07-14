package platformvm

import (
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/vms/components/ava"
	"github.com/ava-labs/gecko/vms/components/verify"
)

// SpendTx is a tx that spends AVA
type SpendTx interface {
	ID() ids.ID
	// Inputs to the tx
	Ins() []*ava.TransferableInput
	// Outputs of the tx
	Outs() []*ava.TransferableOutput
	// Credentials that proves the inputs may be spent
	Creds() []verify.Verifiable
}
