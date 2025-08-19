// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package core

import (
	"math/big"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/core/vm"
	"github.com/ava-labs/libevm/crypto"

	"github.com/ava-labs/subnet-evm/consensus/dummy"
	"github.com/ava-labs/subnet-evm/params"
	"github.com/ava-labs/subnet-evm/params/extras"
	"github.com/ava-labs/subnet-evm/precompile/contracts/txallowlist"
	"github.com/ava-labs/subnet-evm/utils"

	ethparams "github.com/ava-labs/libevm/params"
)

// TestBadTxAllowListBlock tests the output generated when the
// blockchain imports a bad block with a transaction from a
// non-whitelisted TX Allow List address.
func TestBadTxAllowListBlock(t *testing.T) {
	var (
		db       = rawdb.NewMemoryDatabase()
		testAddr = common.HexToAddress("0x71562b71999873DB5b286dF957af199Ec94617F7")

		config = params.WithExtra(
			&params.ChainConfig{
				ChainID:             big.NewInt(1),
				HomesteadBlock:      big.NewInt(0),
				EIP150Block:         big.NewInt(0),
				EIP155Block:         big.NewInt(0),
				EIP158Block:         big.NewInt(0),
				ByzantiumBlock:      big.NewInt(0),
				ConstantinopleBlock: big.NewInt(0),
				PetersburgBlock:     big.NewInt(0),
				IstanbulBlock:       big.NewInt(0),
				MuirGlacierBlock:    big.NewInt(0),
				BerlinBlock:         big.NewInt(0),
				LondonBlock:         big.NewInt(0),
			},
			&extras.ChainConfig{
				FeeConfig: params.DefaultFeeConfig,
				NetworkUpgrades: extras.NetworkUpgrades{
					SubnetEVMTimestamp: utils.NewUint64(0),
				},
				GenesisPrecompiles: extras.Precompiles{
					txallowlist.ConfigKey: txallowlist.NewConfig(utils.NewUint64(0), nil, nil, nil),
				},
			},
		)
		signer     = types.LatestSigner(config)
		testKey, _ = crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")

		gspec = &Genesis{
			Config: config,
			Alloc: GenesisAlloc{
				testAddr: GenesisAccount{
					Balance: big.NewInt(1000000000000000000), // 1 ether
					Nonce:   0,
				},
			},
			GasLimit: params.GetExtra(config).FeeConfig.GasLimit.Uint64(),
		}
		blockchain, _ = NewBlockChain(db, DefaultCacheConfig, gspec, dummy.NewCoinbaseFaker(), vm.Config{}, common.Hash{}, false)
	)
	defer blockchain.Stop()

	mkDynamicTx := func(nonce uint64, to common.Address, gasLimit uint64, gasTipCap, gasFeeCap *big.Int) *types.Transaction {
		tx, _ := types.SignTx(types.NewTx(&types.DynamicFeeTx{
			Nonce:     nonce,
			GasTipCap: gasTipCap,
			GasFeeCap: gasFeeCap,
			Gas:       gasLimit,
			To:        &to,
			Value:     big.NewInt(0),
		}), signer, testKey)
		return tx
	}

	defer blockchain.Stop()
	for i, tt := range []struct {
		txs  []*types.Transaction
		want string
	}{
		{ // Nonwhitelisted address
			txs: []*types.Transaction{
				mkDynamicTx(0, common.Address{}, ethparams.TxGas, big.NewInt(0), big.NewInt(225000000000)),
			},
			want: "could not apply tx 0 [0xc5725e8baac950b2925dd4fea446ccddead1cc0affdae18b31a7d910629d9225]: cannot issue transaction from non-allow listed address: 0x71562b71999873DB5b286dF957af199Ec94617F7",
		},
	} {
		block := GenerateBadBlock(gspec.ToBlock(), dummy.NewCoinbaseFaker(), tt.txs, gspec.Config)
		_, err := blockchain.InsertChain(types.Blocks{block})
		if err == nil {
			t.Fatal("block imported without errors")
		}
		if have, want := err.Error(), tt.want; have != want {
			t.Errorf("test %d:\nhave \"%v\"\nwant \"%v\"\n", i, have, want)
		}
	}
}
