// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package crosschain

import (
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/components/avax"
)

func TestImportCalldataRoundTrip(t *testing.T) {
	want := []avax.UTXOID{{TxID: ids.GenerateTestID(), OutputIndex: 3}, {TxID: ids.GenerateTestID()}}
	args := make([]UTXOID, len(want))
	for i, u := range want {
		args[i] = UTXOID{TxID: u.TxID, OutputIndex: u.OutputIndex}
	}
	data, err := ABI.Pack("importUTXOs", args, common.Address{7})
	require.NoError(t, err)

	got, ok := ImportCalldata(types.NewTx(&types.DynamicFeeTx{To: &ContractAddress, Data: data}))
	require.True(t, ok)
	require.Equal(t, want, got)

	forOwners, err := ABI.Pack("importForOwners", args)
	require.NoError(t, err)
	got, ok = ImportCalldata(types.NewTx(&types.DynamicFeeTx{To: &ContractAddress, Data: forOwners}))
	require.True(t, ok)
	require.Equal(t, want, got)

	_, to, err := unpackImportArgs(data[4:])
	require.NoError(t, err)
	require.Equal(t, common.Address{7}, to)

	other := common.Address{1}
	_, ok = ImportCalldata(types.NewTx(&types.DynamicFeeTx{To: &other, Data: data}))
	require.False(t, ok)
	exportData, err := ABI.Pack("exportAVAX", [32]byte(constants.PlatformChainID), other)
	require.NoError(t, err)
	_, ok = ImportCalldata(types.NewTx(&types.DynamicFeeTx{To: &ContractAddress, Data: exportData}))
	require.False(t, ok)

	var in exportInput
	require.NoError(t, ABI.UnpackInputIntoInterface(&in, "exportAVAX", exportData[4:]))
	require.Equal(t, exportInput{DestinationChainID: constants.PlatformChainID, To: other}, in)
}

func TestFromReceipts(t *testing.T) {
	from, to := common.Address{1}, common.Address{2}
	exportTopics, exportData, err := ABI.PackEvent("Exported", from, [32]byte(constants.PlatformChainID), to, uint64(42))
	require.NoError(t, err)
	utxoID := avax.UTXOID{TxID: ids.GenerateTestID(), OutputIndex: 7}
	importTopics, importData, err := ABI.PackEvent("Imported", to, [32]byte(utxoID.TxID), utxoID.OutputIndex, uint64(9))
	require.NoError(t, err)
	txHash := common.Hash{9}
	ok := &types.Receipt{Status: types.ReceiptStatusSuccessful, Logs: []*types.Log{
		{Address: ContractAddress, Topics: exportTopics, Data: exportData, TxHash: txHash, Index: 1},
		{Address: ContractAddress, Topics: importTopics, Data: importData, TxHash: txHash, Index: 2},
		{Address: from, Topics: exportTopics, Data: exportData, TxHash: txHash, Index: 3},
	}}
	failed := &types.Receipt{Status: types.ReceiptStatusFailed, Logs: ok.Logs}

	exports, imports, err := FromReceipts(types.Receipts{ok, failed})
	require.NoError(t, err)
	require.Equal(t, []Export{{
		From: from, Destination: constants.PlatformChainID, To: ids.ShortID(to), Amount: 42,
		UTXOID: avax.UTXOID{TxID: ids.ID(txHash), OutputIndex: 1},
	}}, exports)
	require.Equal(t, []Import{{Recipient: to, UTXOID: utxoID}}, imports)
}
