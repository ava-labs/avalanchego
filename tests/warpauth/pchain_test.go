// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warpauth

import (
	"encoding/hex"
	"strings"
	"testing"

	"github.com/ava-labs/libevm/accounts/abi"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/vm/runtime"
	"github.com/ava-labs/libevm/params"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

type utxo struct {
	TxID        [32]byte
	OutputIndex uint32
	Amount      uint64
}

type owners struct {
	Locktime  uint64
	Threshold uint32
	Addrs     []common.Address
}

// The Solidity encoder must produce byte-identical output to txs.Codec.
func TestEncodeCreateSubnet(t *testing.T) {
	require := require.New(t)

	parsed, err := abi.JSON(strings.NewReader(PChainABI))
	require.NoError(err)
	initcode, err := hex.DecodeString(PChainBin)
	require.NoError(err)

	var (
		networkID   = uint32(12345)
		pChainID    = ids.ID{0x0a}
		avaxAssetID = ids.ID{0x0b}
		owner       = ids.ShortID{0xde, 0xad}
		ownerA      = ids.ShortID{0x01}
		ownerB      = ids.ShortID{0x02}
	)
	ctorArgs, err := parsed.Pack("", networkID, pChainID, avaxAssetID)
	require.NoError(err)

	statedb, err := state.New(common.Hash{}, state.NewDatabase(rawdb.NewMemoryDatabase()), nil)
	require.NoError(err)
	cfg := &runtime.Config{ChainConfig: params.MergedTestChainConfig, State: statedb, GasLimit: 10_000_000, Random: &common.Hash{}}
	_, contract, _, err := runtime.Create(append(initcode, ctorArgs...), cfg)
	require.NoError(err)

	call := func(ins []utxo, change uint64, o owners) ([]byte, error) {
		input, err := parsed.Pack("encodeCreateSubnet", common.Address(owner), ins, change, o)
		require.NoError(err)
		ret, _, err := runtime.Call(contract, input, cfg)
		if err != nil {
			return nil, err
		}
		var out []byte
		require.NoError(parsed.UnpackIntoInterface(&out, "encodeCreateSubnet", ret))
		return out, nil
	}

	ins := []utxo{
		{TxID: ids.ID{0x10}, OutputIndex: 2, Amount: 5},
		{TxID: ids.ID{0x10}, OutputIndex: 3, Amount: 6},
		{TxID: ids.ID{0x11}, OutputIndex: 0, Amount: 7},
	}
	subnetOwner := owners{Locktime: 9, Threshold: 2, Addrs: []common.Address{common.Address(ownerA), common.Address(ownerB)}}

	expectedIns := make([]*avax.TransferableInput, len(ins))
	for i, in := range ins {
		expectedIns[i] = &avax.TransferableInput{
			UTXOID: avax.UTXOID{TxID: in.TxID, OutputIndex: in.OutputIndex},
			Asset:  avax.Asset{ID: avaxAssetID},
			In:     &secp256k1fx.TransferInput{Amt: in.Amount, Input: secp256k1fx.Input{SigIndices: []uint32{0}}},
		}
	}
	expected := func(change uint64) []byte {
		tx := &txs.CreateSubnetTx{
			BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{NetworkID: networkID, BlockchainID: pChainID, Ins: expectedIns}},
			Owner:  &secp256k1fx.OutputOwners{Locktime: 9, Threshold: 2, Addrs: []ids.ShortID{ownerA, ownerB}},
		}
		if change != 0 {
			tx.Outs = []*avax.TransferableOutput{{
				Asset: avax.Asset{ID: avaxAssetID},
				Out:   &secp256k1fx.TransferOutput{Amt: change, OutputOwners: secp256k1fx.OutputOwners{Threshold: 1, Addrs: []ids.ShortID{owner}}},
			}}
		}
		var u txs.UnsignedTx = tx
		b, err := txs.Codec.Marshal(txs.CodecVersion, &u)
		require.NoError(err)
		return b
	}

	got, err := call(ins, 4, subnetOwner)
	require.NoError(err)
	require.Equal(expected(4), got)

	got, err = call(ins, 0, subnetOwner)
	require.NoError(err)
	require.Equal(expected(0), got)

	_, err = call([]utxo{ins[1], ins[0]}, 4, subnetOwner)
	require.ErrorContains(err, "execution reverted")
	_, err = call(ins, 4, owners{Threshold: 1, Addrs: []common.Address{common.Address(ownerB), common.Address(ownerA)}})
	require.ErrorContains(err, "execution reverted")
	_, err = call(ins, 4, owners{Threshold: 3, Addrs: subnetOwner.Addrs})
	require.ErrorContains(err, "execution reverted")
}
