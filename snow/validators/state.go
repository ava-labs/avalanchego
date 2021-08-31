package validators

import (
	"github.com/ava-labs/avalanchego/ids"
)

// State allows the lookup of validator sets on specified subnets at the
// requested P-chain height.
type State interface {
	// GetCurrentHeight returns the current height of the P-chain.
	GetCurrentHeight() (uint64, error)

	// GetValidatorSet returns the weights of the nodeIDs for the provided
	// subnet at the requested P-chain height.
	GetValidatorSet(height uint64, subnetID ids.ID) (map[ids.ShortID]uint64, error)
}
