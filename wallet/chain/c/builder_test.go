// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package c

import (
	"math/big"
	"testing"

	"github.com/ava-labs/libevm/params"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/tx"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/tx/txtest"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary/common"

	ethcommon "github.com/ava-labs/libevm/common"
)

var (
	avaxAssetID = ids.GenerateTestID()
	cChainID    = ids.GenerateTestID()
	xChainID    = ids.GenerateTestID()

	testContext = &Context{
		NetworkID:    constants.UnitTestID,
		BlockchainID: cChainID,
		AVAXAssetID:  avaxAssetID,
	}
)

const baseFee gas.Price = 25 * params.GWei

// Fees, in nAVAX, at [baseFee], using the gas of golden mainnet transactions of
// the same shape in package tx.
const (
	oneInOneOutFee = 11_230 * 25
	twoInTwoOutFee = 12_378 * 25
)

// TestNewImportTx checks that the fee pays for the output. The e2e tests
// overpay, so they would accept a fee that is slightly too low.
func TestNewImportTx(t *testing.T) {
	key := txtest.NewKey(t)
	utxo := txtest.NewUTXO(1_000_000, avaxAssetID, key.Address())
	utxos := common.NewUTXOs()
	require.NoError(t, utxos.AddUTXO(t.Context(), xChainID, cChainID, utxo), "AddUTXO()")

	kc := secp256k1fx.NewKeychain(key)
	backend := NewBackend(common.NewChainUTXOs(cChainID, utxos), nil)
	builder := NewBuilder(kc.Addresses(), kc.EthAddresses(), testContext, backend)

	to := ethcommon.Address{1}
	got, err := builder.NewImportTx(xChainID, to, baseFee)
	require.NoError(t, err, "NewImportTx()")

	want := &tx.Import{
		NetworkID:    constants.UnitTestID,
		BlockchainID: cChainID,
		SourceChain:  xChainID,
		ImportedInputs: []*avax.TransferableInput{{
			UTXOID: utxo.UTXOID,
			Asset:  utxo.Asset,
			FxID:   secp256k1fx.ID,
			In: &secp256k1fx.TransferInput{
				Amt:   1_000_000,
				Input: secp256k1fx.Input{SigIndices: []uint32{0}},
			},
		}},
		Outs: []tx.Output{{
			Address: to,
			Amount:  1_000_000 - oneInOneOutFee,
			AssetID: avaxAssetID,
		}},
	}
	require.Equal(t, want, got, "NewImportTx()")
}

// TestNewExportTx checks the fee and output order of an export with more than
// one input and output, which the e2e tests don't build.
func TestNewExportTx(t *testing.T) {
	// Neither account can pay alone. The balances are chosen so that the inputs
	// don't depend on the order the accounts are used in.
	const (
		balanceA = 700_000
		balanceB = 1_000_000 + twoInTwoOutFee - balanceA
	)
	keyA := txtest.NewKey(t)
	keyB := txtest.NewKey(t)
	accounts := map[ethcommon.Address]*Account{
		keyA.EthAddress(): {Balance: big.NewInt(balanceA * params.GWei)},
		keyB.EthAddress(): {Balance: big.NewInt(balanceB * params.GWei)},
	}

	kc := secp256k1fx.NewKeychain(keyA, keyB)
	backend := NewBackend(common.NewChainUTXOs(cChainID, common.NewUTXOs()), accounts)
	builder := NewBuilder(kc.Addresses(), kc.EthAddresses(), testContext, backend)

	to := ids.ShortID{1}
	outputs := []*secp256k1fx.TransferOutput{
		txtest.NewTransferOutput(600_000, to),
		txtest.NewTransferOutput(400_000, to),
	}
	got, err := builder.NewExportTx(xChainID, outputs, baseFee)
	require.NoError(t, err, "NewExportTx()")

	want := &tx.Export{
		NetworkID:        constants.UnitTestID,
		BlockchainID:     cChainID,
		DestinationChain: xChainID,
		Ins: []tx.Input{
			{Address: keyA.EthAddress(), Amount: balanceA, AssetID: avaxAssetID},
			{Address: keyB.EthAddress(), Amount: balanceB, AssetID: avaxAssetID},
		},
		// Sorted by canonical bytes, which lead with the amount.
		ExportedOutputs: []*avax.TransferableOutput{
			{Asset: avax.Asset{ID: avaxAssetID}, FxID: secp256k1fx.ID, Out: outputs[1]},
			{Asset: avax.Asset{ID: avaxAssetID}, FxID: secp256k1fx.ID, Out: outputs[0]},
		},
	}
	utils.Sort(want.Ins) // The keys, and so their addresses, are random.
	// The builder initializes the outputs' unexported context.
	opt := cmpopts.IgnoreUnexported(secp256k1fx.OutputOwners{})
	if diff := cmp.Diff(want, got, opt); diff != "" {
		t.Errorf("NewExportTx() diff (-want +got):\n%s", diff)
	}
}
