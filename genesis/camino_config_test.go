// Copyright (C) 2022-2023, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package genesis

import (
	"encoding/hex"
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/formatting/address"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/platformvm/deposit"
	"github.com/ava-labs/avalanchego/vms/platformvm/genesis"
	"github.com/stretchr/testify/require"
)

var (
	nodeID   = ids.GenerateTestNodeID()
	shortID2 = ids.GenerateTestShortID()
)

func TestUnparse(t *testing.T) {
	type args struct {
		networkID uint32
	}
	tests := map[string]struct {
		camino Camino
		args   args
		want   UnparsedCamino
		err    error
	}{
		"success": {
			args: args{networkID: 12345},
			camino: Camino{
				VerifyNodeSignature: true,
				LockModeBondDeposit: true,
				InitialAdmin:        sampleShortID,
				DepositOffers: []DepositOffer{{
					InterestRateNominator:   1,
					Start:                   2,
					End:                     3,
					MinAmount:               4,
					MinDuration:             5,
					MaxDuration:             6,
					UnlockPeriodDuration:    7,
					NoRewardsPeriodDuration: 8,
					Memo:                    "offer memo",
					Flags:                   deposit.OfferFlagLocked,
				}},
				Allocations: []CaminoAllocation{{
					ETHAddr:       sampleShortID,
					AVAXAddr:      sampleShortID,
					XAmount:       1,
					AddressStates: AddressStates{},
					PlatformAllocations: []PlatformAllocation{{
						Amount:            1,
						NodeID:            nodeID,
						ValidatorDuration: 1,
						DepositDuration:   1,
						DepositOfferMemo:  "deposit offer memo",
						TimestampOffset:   1,
						Memo:              "some str",
					}},
				}},
				InitialMultisigAddresses: []genesis.MultisigAlias{{
					Alias:     sampleShortID,
					Threshold: 1,
					Addresses: []ids.ShortID{shortID2},
				}},
			},
			want: UnparsedCamino{
				VerifyNodeSignature: true,
				LockModeBondDeposit: true,
				InitialAdmin:        "X-" + wrappers.IgnoreError(address.FormatBech32("local", sampleShortID.Bytes())).(string),
				DepositOffers: []UnparsedDepositOffer{{
					InterestRateNominator:   1,
					StartOffset:             2,
					EndOffset:               3,
					MinAmount:               4,
					MinDuration:             5,
					MaxDuration:             6,
					UnlockPeriodDuration:    7,
					NoRewardsPeriodDuration: 8,
					Memo:                    "offer memo",
					Flags: UnparsedDepositOfferFlags{
						Locked: true,
					},
				}},
				Allocations: []UnparsedCaminoAllocation{{
					ETHAddr:       "0x" + hex.EncodeToString(sampleShortID.Bytes()),
					AVAXAddr:      "X-" + wrappers.IgnoreError(address.FormatBech32("local", sampleShortID.Bytes())).(string),
					XAmount:       1,
					AddressStates: AddressStates{},
					PlatformAllocations: []UnparsedPlatformAllocation{{
						Amount:            1,
						NodeID:            nodeID.String(),
						ValidatorDuration: 1,
						DepositDuration:   1,
						DepositOfferMemo:  "deposit offer memo",
						TimestampOffset:   1,
						Memo:              "some str",
					}},
				}},
				InitialMultisigAddresses: []UnparsedMultisigAlias{{
					Alias:     wrappers.IgnoreError(address.Format(configChainIDAlias, "local", sampleShortID.Bytes())).(string),
					Threshold: 1,
					Addresses: []string{wrappers.IgnoreError(address.Format(configChainIDAlias, "local", shortID2.Bytes())).(string)},
				}},
			},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got, err := tt.camino.Unparse(tt.args.networkID, 0)

			if tt.err != nil {
				require.ErrorContains(t, err, tt.err.Error())
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}
