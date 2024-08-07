// Copyright (C) 2022-2024, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package utxo

import (
	"errors"
	"testing"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/multisig"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/stretchr/testify/require"
)

func TestUTXOWithMsigVerify(t *testing.T) {
	address := ids.ShortID{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08}
	utxoWithMSig := avax.UTXOWithMSig{
		UTXO: avax.UTXO{
			UTXOID: avax.UTXOID{
				TxID:        ids.GenerateTestID(),
				OutputIndex: 0,
			},
			Asset: avax.Asset{
				ID: ids.GenerateTestID(),
			},
			Out: &secp256k1fx.TransferOutput{
				Amt: uint64(1),
				OutputOwners: secp256k1fx.OutputOwners{
					Locktime:  0,
					Threshold: 1,
					Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
				},
			},
		},
		Aliases: nil,
	}

	tests := map[string]struct {
		aliases []verify.State
		err     error
	}{
		"Successful": {
			aliases: []verify.State{
				&multisig.Alias{
					ID: address,
					Owners: &secp256k1fx.OutputOwners{
						Threshold: 1,
						Addrs:     []ids.ShortID{ids.ShortEmpty},
					},
				},
			},
			err: nil,
		},
		"Threshold exceeds Addrs length": {
			aliases: []verify.State{
				&multisig.Alias{
					ID: address,
					Owners: &secp256k1fx.OutputOwners{
						Threshold: 2,
						Addrs:     []ids.ShortID{ids.ShortEmpty},
					},
				},
			},
			err: errors.New("output is unspendable"),
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			utxoWithMSig.Aliases = test.aliases
			err := utxoWithMSig.Verify()
			if test.err != nil {
				require.Error(t, err)
				require.Equal(t, test.err, err)
			}
		})
	}
}

func TestUTXOWithMSigSerialized(t *testing.T) {
	// Create a new codec manager and linear codec instance
	manager := codec.NewDefaultManager()
	c := linearcodec.NewDefault()

	// Register all relevant types with the codec
	errs := wrappers.Errs{}
	errs.Add(
		c.RegisterType(&secp256k1fx.MintOutput{}),
		c.RegisterType(&secp256k1fx.TransferOutput{}),
		c.RegisterType(&secp256k1fx.Input{}),
		c.RegisterType(&secp256k1fx.TransferInput{}),
		c.RegisterType(&secp256k1fx.Credential{}),
		c.RegisterType(&secp256k1fx.OutputOwners{}),
		c.RegisterType(&multisig.AliasWithNonce{}),

		manager.RegisterCodec(0, c),
	)

	require.False(t, errs.Errored(), errs.Err)

	// Create a new UTXO with extended `Out` object
	utxo := avax.UTXO{
		UTXOID: avax.UTXOID{
			TxID:        ids.GenerateTestID(),
			OutputIndex: 0,
		},
		Asset: avax.Asset{
			ID: ids.GenerateTestID(),
		},
		Out: &secp256k1fx.TransferOutput{
			Amt: uint64(1),
			OutputOwners: secp256k1fx.OutputOwners{
				Locktime:  0,
				Threshold: 1,
				Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
			},
		},
	}
	alias := &multisig.AliasWithNonce{
		Alias: multisig.Alias{
			Owners: &secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
			},
			Memo: make([]byte, avax.MaxMemoSize+1),
			ID:   hashing.ComputeHash160Array(ids.Empty[:]),
		},
	}
	utxoWithMSig := avax.UTXOWithMSig{
		UTXO:    utxo,
		Aliases: []verify.State{alias},
	}

	// Marshal the UTXOWithMSig object into a byte array using the codec manager
	mUTXO, err := manager.Marshal(0, &utxoWithMSig)
	require.NoError(t, err)

	// Create a new UTXOWithMSig object to unmarshal the byte array into
	var newUTXO avax.UTXOWithMSig

	// Unmarshal the byte array into the new UTXOWithMSig object using the codec manager
	_, err = manager.Unmarshal(mUTXO, &newUTXO)
	require.NoError(t, err)

	// Check if the unmarshaled object matches the original object
	require.Equal(t, utxoWithMSig, newUTXO)
}
