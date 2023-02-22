// (c) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// TODO: replace with gomock
package precompileconfig

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
)

var _ Config = &noopStatefulPrecompileConfig{}

type noopStatefulPrecompileConfig struct {
}

func NewNoopStatefulPrecompileConfig() *noopStatefulPrecompileConfig {
	return &noopStatefulPrecompileConfig{}
}

func (n *noopStatefulPrecompileConfig) Key() string {
	return ""
}

func (n *noopStatefulPrecompileConfig) Address() common.Address {
	return common.Address{}
}

func (n *noopStatefulPrecompileConfig) Timestamp() *big.Int {
	return new(big.Int)
}

func (n *noopStatefulPrecompileConfig) IsDisabled() bool {
	return false
}

func (n *noopStatefulPrecompileConfig) Equal(Config) bool {
	return false
}

func (n *noopStatefulPrecompileConfig) Verify() error {
	return nil
}
