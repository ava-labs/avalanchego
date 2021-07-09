package validators

import (
	"errors"

	"github.com/ava-labs/avalanchego/ids"
)

// Following the introduction of snowman++, P-Chain validator needs to be exposed
// to other VMs to match them with the right time window.
// The VM represents this behavior

var ErrNotValidatorsVM = errors.New("VM does not implement validators.VM interface")

type VM interface {
	GetCurrentHeight() (uint64, error)
	GetValidatorSet(height uint64, subnetID ids.ID) (map[ids.ShortID]uint64, error)
}
