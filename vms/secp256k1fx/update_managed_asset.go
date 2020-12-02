package secp256k1fx

import (
	"errors"

	"github.com/ava-labs/avalanchego/vms/components/verify"
)

var (
	errNilUpdateManagedAssetStatusOperation = errors.New("UpdateManagedAssetStatusOperation is nil")
)

// ManagedAssetStatusOutput represents the status of a managed asset
// [Frozen] is true iff the asset is frozen
// [OutputOwners] may move any UTXOs with this asset, and may freeze/unfreeze
// all UTXOs of the asset.
type ManagedAssetStatusOutput struct {
	Frozen  bool         `serialize:"true"`
	Manager OutputOwners `serialize:"true"`
}

// Verify ...
func (s *ManagedAssetStatusOutput) Verify() error {
	return s.Manager.Verify()
}

// VerifyState ...
func (s *ManagedAssetStatusOutput) VerifyState() error {
	return s.Manager.VerifyState()
}

// Verify ...
func (s *ManagedAssetStatusOutput) Addresses() [][]byte {
	return s.Manager.Addresses()
}

// UpdateManagedAssetStatusOperation updates the status
// of a managed asset
type UpdateManagedAssetStatusOperation struct {
	Input                    `serialize:"true"`
	ManagedAssetStatusOutput `serialize:"true"`
}

// Verify ...
func (op *UpdateManagedAssetStatusOperation) Verify() error {
	switch {
	case op == nil:
		return errNilUpdateManagedAssetStatusOperation
	default:
		return verify.All(&op.Input, &op.ManagedAssetStatusOutput)
	}
}

// Outs ...
func (op *UpdateManagedAssetStatusOperation) Outs() []verify.State {
	return []verify.State{&op.ManagedAssetStatusOutput}
}
