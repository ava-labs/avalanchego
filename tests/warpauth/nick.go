// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warpauth

import (
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"

	"github.com/ava-labs/libevm/accounts/abi"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
)

const (
	nickGasLimit  = 4_000_000
	nickGasFeeCap = 100_000_000_000 // 100 nAVAX
)

// Nick's method: a signature nobody computed, so nobody holds the deployer
// key. The deployer address is recovered from the tx, which includes the
// initcode, so the contract address commits to the exact code.
var nickSig = common.Hex2Bytes("1820182018201820182018201820182018201820182018201820182018201820" +
	"1820182018201820182018201820182018201820182018201820182018201820")

// NickDeployCost is the AVAX (in wei) the deployer address must hold.
func NickDeployCost() *big.Int {
	return new(big.Int).Mul(big.NewInt(nickGasLimit), big.NewInt(nickGasFeeCap))
}

// NickDeployTx returns the keyless PChain.sol deployment tx for an EVM chain
// and the deployer address it recovers to. The contract lands at
// crypto.CreateAddress(deployer, 0).
func NickDeployTx(evmChainID *big.Int, networkID uint32, avaxAssetID ids.ID) (*types.Transaction, common.Address, error) {
	parsed, err := abi.JSON(strings.NewReader(PChainABI))
	if err != nil {
		return nil, common.Address{}, err
	}
	initcode, err := hex.DecodeString(PChainBin)
	if err != nil {
		return nil, common.Address{}, err
	}
	ctorArgs, err := parsed.Pack("", networkID, constants.PlatformChainID, avaxAssetID)
	if err != nil {
		return nil, common.Address{}, err
	}
	signer := types.NewLondonSigner(evmChainID)
	unsigned := types.NewTx(&types.DynamicFeeTx{
		ChainID:   evmChainID,
		Nonce:     0,
		GasTipCap: big.NewInt(0),
		GasFeeCap: big.NewInt(nickGasFeeCap),
		Gas:       nickGasLimit,
		Data:      append(initcode, ctorArgs...),
	})
	for v := byte(0); v < 2; v++ {
		tx, err := unsigned.WithSignature(signer, append(append([]byte{}, nickSig...), v))
		if err != nil {
			continue
		}
		deployer, err := types.Sender(signer, tx)
		if err != nil {
			continue
		}
		return tx, deployer, nil
	}
	return nil, common.Address{}, fmt.Errorf("no recoverable signature for chain %s", evmChainID)
}
