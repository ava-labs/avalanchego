package generate

import (
	"bytes"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/multisig"
	"github.com/ava-labs/avalanchego/vms/platformvm/locked"
	"github.com/ava-labs/avalanchego/vms/platformvm/stakeable"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

// utxos

func UTXO(txID ids.ID, assetID ids.ID, amount uint64, outputOwners secp256k1fx.OutputOwners, depositTxID, bondTxID ids.ID, initID bool) *avax.UTXO {
	return UTXOWithIndex(txID, 0, assetID, amount, outputOwners, depositTxID, bondTxID, initID)
}

func StakeableUTXO(txID ids.ID, assetID ids.ID, amount, locktime uint64, outputOwners secp256k1fx.OutputOwners) *avax.UTXO {
	return &avax.UTXO{
		UTXOID: avax.UTXOID{TxID: txID},
		Asset:  avax.Asset{ID: assetID},
		Out: &stakeable.LockOut{
			Locktime: locktime,
			TransferableOut: &secp256k1fx.TransferOutput{
				Amt:          amount,
				OutputOwners: outputOwners,
			},
		},
	}
}

func UTXOWithIndex(txID ids.ID, outIndex uint32, assetID ids.ID, amount uint64, outputOwners secp256k1fx.OutputOwners, depositTxID, bondTxID ids.ID, init bool) *avax.UTXO {
	var out avax.TransferableOut = &secp256k1fx.TransferOutput{
		Amt:          amount,
		OutputOwners: outputOwners,
	}
	if depositTxID != ids.Empty || bondTxID != ids.Empty {
		out = &locked.Out{
			IDs: locked.IDs{
				DepositTxID: depositTxID,
				BondTxID:    bondTxID,
			},
			TransferableOut: out,
		}
	}
	testUTXO := &avax.UTXO{
		UTXOID: avax.UTXOID{
			TxID:        txID,
			OutputIndex: outIndex,
		},
		Asset: avax.Asset{ID: assetID},
		Out:   out,
	}
	if init {
		testUTXO.InputID()
	}
	return testUTXO
}

// outs

func OutFromUTXO(t *testing.T, utxo *avax.UTXO, depositTxID, bondTxID ids.ID) *avax.TransferableOutput {
	t.Helper()

	out := utxo.Out
	if lockedOut, ok := out.(*locked.Out); ok {
		out = lockedOut.TransferableOut
	}
	secpOut, ok := out.(*secp256k1fx.TransferOutput)
	if !ok {
		require.FailNow(t, "not secp256k1fx.TransferOutput")
	}
	var innerOut avax.TransferableOut = &secp256k1fx.TransferOutput{
		Amt:          secpOut.Amt,
		OutputOwners: secpOut.OutputOwners,
	}
	if depositTxID != ids.Empty || bondTxID != ids.Empty {
		innerOut = &locked.Out{
			IDs: locked.IDs{
				DepositTxID: depositTxID,
				BondTxID:    bondTxID,
			},
			TransferableOut: innerOut,
		}
	}
	return &avax.TransferableOutput{
		Asset: avax.Asset{ID: utxo.AssetID()},
		Out:   innerOut,
	}
}

func Out(assetID ids.ID, amount uint64, outputOwners secp256k1fx.OutputOwners, depositTxID, bondTxID ids.ID) *avax.TransferableOutput {
	var out avax.TransferableOut = &secp256k1fx.TransferOutput{
		Amt:          amount,
		OutputOwners: outputOwners,
	}
	if depositTxID != ids.Empty || bondTxID != ids.Empty {
		out = &locked.Out{
			IDs: locked.IDs{
				DepositTxID: depositTxID,
				BondTxID:    bondTxID,
			},
			TransferableOut: out,
		}
	}
	return &avax.TransferableOutput{
		Asset: avax.Asset{ID: assetID},
		Out:   out,
	}
}

func CrossOut(assetID ids.ID, amount uint64, outputOwners secp256k1fx.OutputOwners, recipient ids.ShortID) *avax.TransferableOutput {
	var out avax.TransferableOut = &secp256k1fx.CrossTransferOutput{
		TransferOutput: secp256k1fx.TransferOutput{
			Amt:          amount,
			OutputOwners: outputOwners,
		},
		Recipient: recipient,
	}
	return &avax.TransferableOutput{
		Asset: avax.Asset{ID: assetID},
		Out:   out,
	}
}

func StakeableOut(assetID ids.ID, amount, locktime uint64, outputOwners secp256k1fx.OutputOwners) *avax.TransferableOutput {
	return &avax.TransferableOutput{
		Asset: avax.Asset{ID: assetID},
		Out: &stakeable.LockOut{
			Locktime: locktime,
			TransferableOut: &secp256k1fx.TransferOutput{
				Amt:          amount,
				OutputOwners: outputOwners,
			},
		},
	}
}

// ins

func InWithTxID(txID, assetID ids.ID, amount uint64, depositTxID, bondTxID ids.ID, sigIndices []uint32) *avax.TransferableInput {
	var in avax.TransferableIn = &secp256k1fx.TransferInput{
		Amt: amount,
		Input: secp256k1fx.Input{
			SigIndices: sigIndices,
		},
	}
	if depositTxID != ids.Empty || bondTxID != ids.Empty {
		in = &locked.In{
			IDs: locked.IDs{
				DepositTxID: depositTxID,
				BondTxID:    bondTxID,
			},
			TransferableIn: in,
		}
	}
	return &avax.TransferableInput{
		UTXOID: avax.UTXOID{TxID: txID},
		Asset:  avax.Asset{ID: assetID},
		In:     in,
	}
}

func In(assetID ids.ID, amount uint64, depositTxID, bondTxID ids.ID, sigIndices []uint32) *avax.TransferableInput {
	var in avax.TransferableIn = &secp256k1fx.TransferInput{
		Amt: amount,
		Input: secp256k1fx.Input{
			SigIndices: sigIndices,
		},
	}
	if depositTxID != ids.Empty || bondTxID != ids.Empty {
		in = &locked.In{
			IDs: locked.IDs{
				DepositTxID: depositTxID,
				BondTxID:    bondTxID,
			},
			TransferableIn: in,
		}
	}
	return &avax.TransferableInput{
		UTXOID: avax.UTXOID{
			TxID: ids.GenerateTestID(),
		},
		Asset: avax.Asset{ID: assetID},
		In:    in,
	}
}

func StakeableIn(assetID ids.ID, amount, locktime uint64, sigIndices []uint32) *avax.TransferableInput {
	return &avax.TransferableInput{
		Asset: avax.Asset{ID: assetID},
		In: &stakeable.LockIn{
			Locktime: locktime,
			TransferableIn: &secp256k1fx.TransferInput{
				Amt: amount,
				Input: secp256k1fx.Input{
					SigIndices: sigIndices,
				},
			},
		},
	}
}

func InFromUTXO(t *testing.T, utxo *avax.UTXO, sigIndices []uint32, initID bool) *avax.TransferableInput {
	t.Helper()

	var in avax.TransferableIn
	switch out := utxo.Out.(type) {
	case *secp256k1fx.TransferOutput:
		in = &secp256k1fx.TransferInput{
			Amt:   out.Amount(),
			Input: secp256k1fx.Input{SigIndices: sigIndices},
		}
	case *locked.Out:
		in = &locked.In{
			IDs: out.IDs,
			TransferableIn: &secp256k1fx.TransferInput{
				Amt:   out.Amount(),
				Input: secp256k1fx.Input{SigIndices: sigIndices},
			},
		}
	case *stakeable.LockOut:
		in = &stakeable.LockIn{
			Locktime: 1,
			TransferableIn: &secp256k1fx.TransferInput{
				Amt:   out.Amount(),
				Input: secp256k1fx.Input{SigIndices: sigIndices},
			},
		}
	default:
		require.FailNow(t, "unknown utxo.Out type")
		t.FailNow()
	}

	// to be sure that utxoid.id is set in both entities
	input := &avax.TransferableInput{
		UTXOID: avax.UTXOID{
			TxID:        utxo.TxID,
			OutputIndex: utxo.OutputIndex,
		},
		Asset: utxo.Asset,
		In:    in,
	}
	if initID {
		input.InputID()
	}
	return input
}

func InsFromUTXOs(t *testing.T, utxos []*avax.UTXO) []*avax.TransferableInput {
	return InsFromUTXOsWithSigIndices(t, utxos, []uint32{0})
}

func InsFromUTXOsWithSigIndices(t *testing.T, utxos []*avax.UTXO, sigIndices []uint32) []*avax.TransferableInput {
	ins := make([]*avax.TransferableInput, len(utxos))
	for i := range utxos {
		ins[i] = InFromUTXO(t, utxos[i], sigIndices, false)
	}
	return ins
}

// other

func KeyAndOwner(t *testing.T, key *secp256k1.PrivateKey) (*secp256k1.PrivateKey, ids.ShortID, secp256k1fx.OutputOwners) {
	t.Helper()
	addr := key.Address()
	return key, addr, secp256k1fx.OutputOwners{
		Locktime:  0,
		Threshold: 1,
		Addrs:     []ids.ShortID{addr},
	}
}

func MsigAliasAndKeys(
	keys []*secp256k1.PrivateKey,
	threshold uint32,
	sorted bool,
) (
	_ []*secp256k1.PrivateKey,
	alias *multisig.AliasWithNonce,
	aliasOwner *secp256k1fx.OutputOwners,
	aliasDefinition *secp256k1fx.OutputOwners,
) {
	msgOwners := msgOwnersWithKeys{
		Owners: &secp256k1fx.OutputOwners{
			Threshold: threshold,
			Addrs:     make([]ids.ShortID, len(keys)),
		},
		Keys: make([]*secp256k1.PrivateKey, len(keys)),
	}
	copy(msgOwners.Keys, keys)

	for i := 0; i < len(keys); i++ {
		msgOwners.Owners.Addrs[i] = msgOwners.Keys[i].Address()
	}

	if sorted {
		sort.Sort(msgOwners)
	} else {
		sort.Sort(sort.Reverse(msgOwners))
	}

	alias = &multisig.AliasWithNonce{Alias: multisig.Alias{
		ID:     ids.GenerateTestShortID(),
		Owners: msgOwners.Owners,
	}}

	return msgOwners.Keys, alias, msgOwners.Owners, &secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs:     []ids.ShortID{alias.ID},
	}
}

// msgOwnersWithKeys is created in order to be able to sort both keys and owners by address
type msgOwnersWithKeys struct {
	Owners *secp256k1fx.OutputOwners
	Keys   []*secp256k1.PrivateKey
}

func (mo msgOwnersWithKeys) Len() int {
	return len(mo.Keys)
}

func (mo msgOwnersWithKeys) Swap(i, j int) {
	mo.Owners.Addrs[i], mo.Owners.Addrs[j] = mo.Owners.Addrs[j], mo.Owners.Addrs[i]
	mo.Keys[i], mo.Keys[j] = mo.Keys[j], mo.Keys[i]
}

func (mo msgOwnersWithKeys) Less(i, j int) bool {
	return bytes.Compare(mo.Owners.Addrs[i].Bytes(), mo.Owners.Addrs[j].Bytes()) < 0
}
