// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tx

import (
	"encoding/binary"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	chainsatomic "github.com/ava-labs/avalanchego/chains/atomic"
)

func TestImportWarpCredential(t *testing.T) {
	var (
		cChainID    = ids.GenerateTestID()
		avaxAssetID = ids.GenerateTestID()
		helper      = common.Address{0xaa}
		owner       = common.Address{1}
		utxoID      = avax.UTXOID{TxID: ids.GenerateTestID()}
		height      = uint64(7)
	)
	imp := &Import{
		BlockchainID: cChainID,
		SourceChain:  constants.PlatformChainID,
		ImportedInputs: []*avax.TransferableInput{{
			UTXOID: utxoID,
			Asset:  avax.Asset{ID: avaxAssetID},
			In:     &secp256k1fx.TransferInput{Amt: 100, Input: secp256k1fx.Input{SigIndices: []uint32{0}}},
		}},
		Outs: []Output{{Address: owner, Amount: 99, AssetID: avaxAssetID}},
	}
	unsigned, err := UnsignedBytes(imp)
	require.NoError(t, err)

	message := func(from common.Address, who common.Address, txBytes []byte) []byte {
		p := append(append(who.Bytes(), binary.BigEndian.AppendUint64(nil, height)...), txBytes...)
		call, err := payload.NewAddressedCall(from.Bytes(), p)
		require.NoError(t, err)
		msg, err := warp.NewUnsignedMessage(1, cChainID, call.Bytes())
		require.NoError(t, err)
		return msg.Bytes()
	}
	good := message(helper, owner, unsigned)
	trusting := func(id ids.ID, from common.Address, h uint64) bool {
		msg, err := warp.ParseUnsignedMessage(good)
		require.NoError(t, err)
		return from == helper && h == height && id == msg.ID()
	}

	tests := []struct {
		name    string
		utxoOut *secp256k1fx.TransferOutput
		cred    Credential
		auth    WarpAuth
		wantErr error
	}{
		{
			name:    "valid",
			utxoOut: &secp256k1fx.TransferOutput{Amt: 100, OutputOwners: secp256k1fx.OutputOwners{Threshold: 1, Addrs: []ids.ShortID{ids.ShortID(owner)}}},
			cred:    &WarpCredential{Message: good},
			auth:    trusting,
		},
		{
			name:    "owner_mismatch",
			utxoOut: &secp256k1fx.TransferOutput{Amt: 100, OutputOwners: secp256k1fx.OutputOwners{Threshold: 1, Addrs: []ids.ShortID{ids.ShortID(common.Address{2})}}},
			cred:    &WarpCredential{Message: good},
			auth:    trusting,
			wantErr: errWarpOwnerMismatch,
		},
		{
			name:    "other_tx_bytes",
			utxoOut: &secp256k1fx.TransferOutput{Amt: 100, OutputOwners: secp256k1fx.OutputOwners{Threshold: 1, Addrs: []ids.ShortID{ids.ShortID(owner)}}},
			cred:    &WarpCredential{Message: message(helper, owner, append(unsigned, 0))},
			auth:    trusting,
			wantErr: errWrongWarpPayload,
		},
		{
			name:    "untrusted_sender",
			utxoOut: &secp256k1fx.TransferOutput{Amt: 100, OutputOwners: secp256k1fx.OutputOwners{Threshold: 1, Addrs: []ids.ShortID{ids.ShortID(owner)}}},
			cred:    &WarpCredential{Message: message(common.Address{0xbb}, owner, unsigned)},
			auth:    trusting,
			wantErr: errUnknownWarpMessage,
		},
		{
			name:    "message_not_emitted",
			utxoOut: &secp256k1fx.TransferOutput{Amt: 100, OutputOwners: secp256k1fx.OutputOwners{Threshold: 1, Addrs: []ids.ShortID{ids.ShortID(owner)}}},
			cred:    &WarpCredential{Message: good},
			auth:    func(ids.ID, common.Address, uint64) bool { return false },
			wantErr: errUnknownWarpMessage,
		},
		{
			name:    "multisig_owner",
			utxoOut: &secp256k1fx.TransferOutput{Amt: 100, OutputOwners: secp256k1fx.OutputOwners{Threshold: 1, Addrs: []ids.ShortID{ids.ShortID(owner), ids.ShortID(common.Address{2})}}},
			cred:    &WarpCredential{Message: good},
			auth:    trusting,
			wantErr: errWarpOwnerMismatch,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			memory := chainsatomic.NewMemory(memdb.New())
			utxoBytes, err := MarshalUTXO(&avax.UTXO{UTXOID: utxoID, Asset: avax.Asset{ID: avaxAssetID}, Out: tt.utxoOut})
			require.NoError(t, err)
			inputID := utxoID.InputID()
			require.NoError(t, memory.NewSharedMemory(constants.PlatformChainID).Apply(map[ids.ID]*chainsatomic.Requests{
				cChainID: {PutRequests: []*chainsatomic.Element{{Key: inputID[:], Value: utxoBytes}}},
			}))
			tx := &Tx{Unsigned: imp, Creds: []Credential{tt.cred}}
			err = tx.VerifyCredentials(memory.NewSharedMemory(cChainID), tt.auth)
			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}
