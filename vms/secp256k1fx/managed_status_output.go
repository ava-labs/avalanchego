package secp256k1fx

import "errors"

var (
	errManagerThresholdZero = errors.New("ManagedAssetStatusOutput.Manager can't be empty")
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
	if err := s.Manager.Verify(); err != nil {
		return err
	}
	// Make sure the manager is not empty
	if s.Manager.Threshold == 0 {
		return errManagerThresholdZero
	}
	return nil
}

// VerifyState ...
func (s *ManagedAssetStatusOutput) VerifyState() error {
	return s.Verify()
}

// Verify ...
func (s *ManagedAssetStatusOutput) Addresses() [][]byte {
	return s.Manager.Addresses()
}
