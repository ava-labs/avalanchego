// Copyright (C) 2022-2024, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package genesis

import (
	"encoding/hex"
	"fmt"
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/formatting/address"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/platformvm/deposit"
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
				InitialMultisigAddresses: []MultisigAlias{{
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

func TestSameMSigDefinitionsResultedWithSameAlias(t *testing.T) {
	msig1 := MultisigAlias{
		Threshold: 1,
		Addresses: []ids.ShortID{ids.ShortEmpty},
		Memo:      "",
	}
	msig2 := MultisigAlias{
		Threshold: 1,
		Addresses: []ids.ShortID{ids.ShortEmpty},
		Memo:      "",
	}
	require.Equal(t, msig1, msig2)
	require.Equal(t, msig1.ComputeAlias(ids.Empty), msig2.ComputeAlias(ids.Empty))
}

func TestTxIDIsPartOfAliasComputation(t *testing.T) {
	msig := MultisigAlias{
		Threshold: 1,
		Addresses: []ids.ShortID{ids.ShortEmpty},
		Memo:      "",
	}
	require.NotEqual(t, msig.ComputeAlias(ids.ID{1}), msig.ComputeAlias(ids.ID{2}))
}

func TestMemoIsPartOfTheMsigAliasComputation(t *testing.T) {
	msig1 := MultisigAlias{
		Threshold: 1,
		Addresses: []ids.ShortID{ids.ShortEmpty},
		Memo:      "",
	}
	msig2 := MultisigAlias{
		Threshold: 1,
		Addresses: []ids.ShortID{ids.ShortEmpty},
		Memo:      "memo",
	}
	require.NotEqual(t, msig1.ComputeAlias(ids.Empty), msig2.ComputeAlias(ids.Empty))
}

func TestKnownValueAliasComputationTests(t *testing.T) {
	knownValueTests := []struct {
		txID          ids.ID
		addresses     []ids.ShortID
		threshold     uint32
		memo          string
		expectedAlias string
	}{
		{
			txID:          ids.Empty,
			addresses:     []ids.ShortID{ids.ShortEmpty},
			threshold:     1,
			expectedAlias: "GaD29bC73t6v6hfMfvgFFkT2EuKdSranB",
		},
		{
			txID:          ids.ID{1},
			addresses:     []ids.ShortID{ids.ShortEmpty},
			threshold:     1,
			expectedAlias: "Ku5QCiKfFu8qPzs8gdcFmkT7HXnEMReUT",
		},
		{
			txID:          ids.Empty,
			addresses:     []ids.ShortID{ids.ShortEmpty},
			threshold:     1,
			memo:          "Camino Go!",
			expectedAlias: "A2JCzPKavqD1D87YgNRZoC36rehf5EZmR",
		},
		{
			txID:          ids.Empty,
			addresses:     []ids.ShortID{ids.ShortEmpty, {1}},
			threshold:     2,
			expectedAlias: "9zT6zU8VuiqcyrqfDWniTsYM2a3NHxiYh",
		},
		{
			txID:          ids.Empty,
			addresses:     []ids.ShortID{ids.ShortEmpty, {1}, {2}},
			threshold:     2,
			expectedAlias: "88s5CJ4AatRWp3JEb3vDxgd5Ds6Hq2W4u",
		},
	}

	for _, tt := range knownValueTests {
		t.Run(fmt.Sprintf("t-%d-%s-%s", tt.threshold, tt.memo, tt.expectedAlias[:7]), func(t *testing.T) {
			msig := MultisigAlias{
				Threshold: tt.threshold,
				Addresses: tt.addresses,
				Memo:      tt.memo,
			}
			require.Equal(t, tt.expectedAlias, msig.ComputeAlias(tt.txID).String())
		})
	}
}
