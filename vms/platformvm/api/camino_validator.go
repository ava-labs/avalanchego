// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package api

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/genesis"
	"github.com/ava-labs/avalanchego/vms/platformvm/locked"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/txheap"
	"github.com/ava-labs/avalanchego/vms/platformvm/validator"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

func getCaminoValidators(args *BuildGenesisArgs, vdrs txheap.TimedHeap, utxos []*genesis.UTXO) error {
	for _, vdr := range args.Validators {
		weight := uint64(0)
		bond := make([]*avax.TransferableOutput, len(vdr.Staked))
		sortUTXOs(vdr.Staked)
		for i, apiUTXO := range vdr.Staked {
			addrID, err := bech32ToID(apiUTXO.Address)
			if err != nil {
				return err
			}

			output := &avax.TransferableOutput{
				Asset: avax.Asset{ID: args.AvaxAssetID},
				Out: &locked.Out{
					IDs: locked.IDs{}.Lock(locked.StateBonded),
					TransferableOut: &secp256k1fx.TransferOutput{
						Amt: uint64(apiUTXO.Amount),
						OutputOwners: secp256k1fx.OutputOwners{
							Locktime:  0,
							Threshold: 1,
							Addrs:     []ids.ShortID{addrID},
						},
					},
				},
			}

			bond[i] = output

			newWeight, err := math.Add64(weight, uint64(apiUTXO.Amount))
			if err != nil {
				return errStakeOverflow
			}
			weight = newWeight
		}

		if weight == 0 {
			return errValidatorAddsNoValue
		}
		if uint64(vdr.EndTime) <= uint64(args.Time) {
			return errValidatorAddsNoValue
		}

		owner := &secp256k1fx.OutputOwners{
			Locktime:  uint64(vdr.RewardOwner.Locktime),
			Threshold: uint32(vdr.RewardOwner.Threshold),
		}
		for _, addrStr := range vdr.RewardOwner.Addresses {
			addrID, err := bech32ToID(addrStr)
			if err != nil {
				return err
			}
			owner.Addrs = append(owner.Addrs, addrID)
		}
		ids.SortShortIDs(owner.Addrs)

		delegationFee := uint32(0)
		if vdr.ExactDelegationFee != nil {
			delegationFee = uint32(*vdr.ExactDelegationFee)
		}

		tx := &txs.Tx{Unsigned: &txs.CaminoAddValidatorTx{
			AddValidatorTx: txs.AddValidatorTx{
				BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
					NetworkID:    uint32(args.NetworkID),
					BlockchainID: ids.Empty,
					Outs:         bond,
				}},
				Validator: validator.Validator{
					NodeID: vdr.NodeID,
					Start:  uint64(args.Time),
					End:    uint64(vdr.EndTime),
					Wght:   weight,
				},
				RewardsOwner:     owner,
				DelegationShares: delegationFee,
			},
		}}
		if err := tx.Sign(txs.GenesisCodec, nil); err != nil {
			return err
		}

		txID := tx.ID()

		for i, output := range bond {
			messageBytes, err := formatting.Decode(args.Encoding, vdr.Staked[i].Message)
			if err != nil {
				return fmt.Errorf("problem decoding UTXO message bytes: %w", err)
			}

			out := output.Out
			if lockedOut, ok := out.(*locked.Out); ok {
				utxoLockedOut := *lockedOut
				utxoLockedOut.FixLockID(txID, locked.StateBonded)
				out = &utxoLockedOut
			}

			utxos = append(utxos, &genesis.UTXO{
				UTXO: avax.UTXO{
					UTXOID: avax.UTXOID{
						TxID:        txID,
						OutputIndex: uint32(i),
					},
					Asset: avax.Asset{ID: args.AvaxAssetID},
					Out:   out,
				},
				Message: messageBytes,
			})
		}

		vdrs.Add(tx)
	}

	return nil
}
