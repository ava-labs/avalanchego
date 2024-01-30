// (c) 2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vm

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
)

func TestIsProhibited(t *testing.T) {
	// reserved addresses (coreth)
	assert.True(t, IsProhibited(common.HexToAddress("0x0100000000000000000000000000000000000000")))
	assert.True(t, IsProhibited(common.HexToAddress("0x0100000000000000000000000000000000000010")))
	assert.True(t, IsProhibited(common.HexToAddress("0x01000000000000000000000000000000000000f0")))
	assert.True(t, IsProhibited(common.HexToAddress("0x01000000000000000000000000000000000000ff")))
	// reserved addresses (subnet-evm)
	assert.True(t, IsProhibited(common.HexToAddress("0x0200000000000000000000000000000000000000")))
	assert.True(t, IsProhibited(common.HexToAddress("0x0200000000000000000000000000000000000010")))
	assert.True(t, IsProhibited(common.HexToAddress("0x02000000000000000000000000000000000000f0")))
	assert.True(t, IsProhibited(common.HexToAddress("0x02000000000000000000000000000000000000ff")))
	// reserved addresses (custom precompiles)
	assert.True(t, IsProhibited(common.HexToAddress("0x0300000000000000000000000000000000000000")))
	assert.True(t, IsProhibited(common.HexToAddress("0x0300000000000000000000000000000000000010")))
	assert.True(t, IsProhibited(common.HexToAddress("0x03000000000000000000000000000000000000f0")))
	assert.True(t, IsProhibited(common.HexToAddress("0x03000000000000000000000000000000000000ff")))

	// allowed for use
	assert.False(t, IsProhibited(common.HexToAddress("0x00000000000000000000000000000000000000ff")))
	assert.False(t, IsProhibited(common.HexToAddress("0x00ffffffffffffffffffffffffffffffffffffff")))
	assert.False(t, IsProhibited(common.HexToAddress("0x0100000000000000000000000000000000000100")))
	assert.False(t, IsProhibited(common.HexToAddress("0x0200000000000000000000000000000000000100")))
	assert.False(t, IsProhibited(common.HexToAddress("0x0300000000000000000000000000000000000100")))
}
