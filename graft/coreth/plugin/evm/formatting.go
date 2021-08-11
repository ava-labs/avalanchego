// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ethereum/go-ethereum/common"
	ethcrypto "github.com/ethereum/go-ethereum/crypto"
)

// ParseLocalAddress takes in an address for this chain and produces the ID
func (vm *VM) ParseLocalAddress(addrStr string) (ids.ShortID, error) {
	chainID, addr, err := vm.ParseAddress(addrStr)
	if err != nil {
		return ids.ShortID{}, err
	}
	if chainID != vm.ctx.ChainID {
		return ids.ShortID{}, fmt.Errorf("expected chainID to be %q but was %q",
			vm.ctx.ChainID, chainID)
	}
	return addr, nil
}

// FormatLocalAddress takes in a raw address and produces the formatted address
func (vm *VM) FormatLocalAddress(addr ids.ShortID) (string, error) {
	return vm.FormatAddress(vm.ctx.ChainID, addr)
}

// FormatAddress takes in a chainID and a raw address and produces the formatted
// address
func (vm *VM) FormatAddress(chainID ids.ID, addr ids.ShortID) (string, error) {
	chainIDAlias, err := vm.ctx.BCLookup.PrimaryAlias(chainID)
	if err != nil {
		return "", err
	}
	hrp := constants.GetHRP(vm.ctx.NetworkID)
	return formatting.FormatAddress(chainIDAlias, hrp, addr.Bytes())
}

// ParseEthAddress parses [addrStr] and returns an Ethereum address
func ParseEthAddress(addrStr string) (common.Address, error) {
	if !common.IsHexAddress(addrStr) {
		return common.Address{}, errInvalidAddr
	}
	return common.HexToAddress(addrStr), nil
}

// FormatEthAddress formats [addr] into a string
func FormatEthAddress(addr common.Address) string {
	return addr.Hex()
}

// GetEthAddress returns the ethereum address derived from [privKey]
func GetEthAddress(privKey *crypto.PrivateKeySECP256K1R) common.Address {
	return PublicKeyToEthAddress(privKey.PublicKey().(*crypto.PublicKeySECP256K1R))
}

// PublicKeyToEthAddress returns the ethereum address derived from [pubKey]
func PublicKeyToEthAddress(pubKey *crypto.PublicKeySECP256K1R) common.Address {
	return ethcrypto.PubkeyToAddress(*(pubKey.ToECDSA()))
}
