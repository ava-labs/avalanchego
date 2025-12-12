// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.
//
// This file is a derived work, based on the go-ethereum library whose original
// notices appear below.
//
// It is distributed under a license compatible with the licensing terms of the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********
// Copyright 2014 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package core

import (
	_ "embed"
	"math/big"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/params"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contract"
	"github.com/stretchr/testify/require"
)

var (
	//go:embed TrieStressTest.bin
	stressBinStr string
	//go:embed TrieStressTest.abi
	stressABIStr string
)

func BenchmarkTrie(t *testing.B) {
	benchInsertChain(t, true, stressTestTrieDb(t, 100, 6, 50, 1202102))
}

func stressTestTrieDb(t *testing.B, numContracts int, callsPerBlock int, elements int64, gasTxLimit uint64) func(int, *BlockGen) {
	require := require.New(t)
	config := params.TestChainConfig
	signer := types.LatestSigner(config)
	testKey, _ := crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")

	contractAddr := make([]common.Address, numContracts)
	contractTxs := make([]*types.Transaction, numContracts)

	gasPrice := big.NewInt(225000000000)
	gasCreation := uint64(258000)
	deployedContracts := 0

	for i := 0; i < numContracts; i++ {
		nonce := uint64(i)
		tx, _ := types.SignTx(types.NewTx(&types.LegacyTx{
			Nonce:    nonce,
			Value:    big.NewInt(0),
			Gas:      gasCreation,
			GasPrice: gasPrice,
			Data:     common.FromHex(stressBinStr),
		}), signer, testKey)
		sender, _ := types.Sender(signer, tx)
		contractTxs[i] = tx
		contractAddr[i] = crypto.CreateAddress(sender, nonce)
	}

	stressABI := contract.ParseABI(stressABIStr)
	txPayload, _ := stressABI.Pack(
		"writeValues",
		big.NewInt(elements),
	)

	return func(i int, gen *BlockGen) {
		if len(contractTxs) != deployedContracts {
			// This is the first stage of the bench, the preparation stage, at
			// this state the requested amount of smart contract will be
			// deployed to be used in the second stage
			//
			// This block will be executed until all contracts are deployed, the
			// code will make sure that enough instances are deployed per block
			block := gen.PrevBlock(i - 1)
			gas := block.GasLimit()
			for ; deployedContracts < len(contractTxs) && gasCreation < gas; deployedContracts++ {
				gen.AddTx(contractTxs[deployedContracts])
				require.Equal(gen.receipts[len(gen.receipts)-1].Status, types.ReceiptStatusSuccessful, "Execution of last transaction failed")
				gas -= gasCreation
			}
			return
		}

		// Benchmark itself, this is the second stage
		for e := 0; e < callsPerBlock; e++ {
			contractId := (i + e) % deployedContracts
			tx, err := types.SignTx(types.NewTx(&types.LegacyTx{
				Nonce:    gen.TxNonce(benchRootAddr),
				To:       &contractAddr[contractId],
				Value:    big.NewInt(0),
				Gas:      gasTxLimit,
				GasPrice: gasPrice,
				Data:     txPayload,
			}), signer, testKey)
			require.NoError(err)
			gen.AddTx(tx)
			require.Equal(gen.receipts[len(gen.receipts)-1].Status, types.ReceiptStatusSuccessful, "Execution of last transaction failed")
		}
	}
}
