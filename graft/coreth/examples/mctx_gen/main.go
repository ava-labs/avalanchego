// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"flag"
	"fmt"
	"math/big"
	"strings"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/coreth"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/core/vm"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rlp"
)

func checkError(err error) {
	if err != nil {
		panic(err)
	}
}

var mcAbiJSON = `[{"inputs":[],"name":"enableMultiCoin","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"uint256","name":"coinid","type":"uint256"}],"name":"getBalance","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"address payable","name":"recipient","type":"address"},{"internalType":"uint256","name":"amount","type":"uint256"},{"internalType":"uint256","name":"coinid","type":"uint256"},{"internalType":"uint256","name":"amount2","type":"uint256"}],"name":"transfer","outputs":[],"stateMutability":"nonpayable","type":"function"}]`

func main() {
	var fChainID int64
	var fKey string
	var fNonce uint64
	var fGasPrice int64
	var fGasLimit uint64
	var fTo string
	var fAmount int64
	var fAssetID string
	flag.Int64Var(&fChainID, "chainid", 43112, "tx.chainId")
	flag.Uint64Var(&fNonce, "nonce", 0, "tx.nonce")
	flag.Int64Var(&fGasPrice, "gasprice", 470000000000, "tx.gasPrice")
	flag.Uint64Var(&fGasLimit, "gaslimit", 21000, "tx.gasLimit")
	flag.Int64Var(&fAmount, "amount", 100, "tx.amount")
	flag.StringVar(&fKey, "key", "0x56289e99c94b6912bfc12adc093c9b51124f0dc54ac7a766b2bc5ccf558d8027", "private key (hex with \"0x\")")
	flag.StringVar(&fTo, "to", "0x1f0e5C64AFdf53175f78846f7125776E76FA8F34", "tx.to (hex with \"0x\")")
	flag.StringVar(&fAssetID, "assetid", "va6gCYWu3boo3NKQgzdwfzB73dmTYKZn2EZ7nz5pbDBXAsaj4", "assetID")
	flag.Parse()

	_pkey, err := crypto.HexToECDSA(fKey[2:])
	checkError(err)
	pkey := coreth.NewKeyFromECDSA(_pkey)

	// info required to generate a transaction
	chainID := big.NewInt(fChainID)
	nonce := fNonce
	gasPrice := big.NewInt(fGasPrice)
	gasLimit := fGasLimit
	to := common.HexToAddress(fTo)
	assetID, err := ids.FromString(fAssetID)
	checkError(err)
	mcAbi, err := abi.JSON(strings.NewReader(mcAbiJSON))
	data, err := mcAbi.Pack("transfer", to, big.NewInt(0), common.Hash(assetID).Big(), big.NewInt(fAmount))
	checkError(err)
	tx := types.NewTransaction(nonce, vm.BuiltinAddr, big.NewInt(0), uint64(gasLimit), gasPrice, data)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(chainID), pkey.PrivateKey)
	checkError(err)
	txJSON, err := signedTx.MarshalJSON()
	checkError(err)
	fmt.Printf("json: %s\n", string(txJSON))
	b, err := rlp.EncodeToBytes(signedTx)
	fmt.Printf("hex: %s\n", hexutil.Encode(b))
}
