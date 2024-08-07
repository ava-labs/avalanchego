// Copyright (C) 2022-2024, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package genesis

import (
	"encoding/hex"
	"errors"
	"fmt"
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/formatting/address"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/platformvm/deposit"
	"github.com/stretchr/testify/require"
)

var (
	sampleShortID              = ids.GenerateTestShortID()
	addressWithInvalidFormat   = ids.GenerateTestShortID().String()
	addressWithInvalidChecksum = "X-camino1859dz2uwazfgahey3j53ef2kqrans0c8htcu8n"
	xAddress                   = "X-camino1859dz2uwazfgahey3j53ef2kqrans0c8htcu7n"
	cAddress                   = "C-camino1859dz2uwazfgahey3j53ef2kqrans0c8htcu7n"
	toShortID                  = wrappers.IgnoreError(ids.ToShortID([]byte(cAddress))).(ids.ShortID)
)

func TestParse(t *testing.T) {
	tests := map[string]struct {
		unparsedCamino UnparsedCamino
		want           Camino
		err            error
	}{
		"Invalid address - no prefix": {
			unparsedCamino: UnparsedCamino{
				InitialAdmin: addressWithInvalidFormat,
			},
			err: errCannotParseInitialAdmin,
		},
		"Invalid address - bad checksum": {
			unparsedCamino: UnparsedCamino{
				InitialAdmin: addressWithInvalidChecksum,
			},
			err: errCannotParseInitialAdmin,
		},
		"Invalid allocation - missing eth address": {
			unparsedCamino: UnparsedCamino{
				InitialAdmin: xAddress,
				Allocations:  []UnparsedCaminoAllocation{{}},
			},
			err: errInvalidETHAddress,
		},
		"Invalid allocation - invalid eth address": {
			unparsedCamino: UnparsedCamino{
				InitialAdmin: xAddress,
				Allocations: []UnparsedCaminoAllocation{{
					ETHAddr: ids.GenerateTestShortID().String(),
				}},
			},
			err: errors.New("encoding/hex: invalid byte"),
		},
		"Invalid allocation - invalid avax address": {
			unparsedCamino: UnparsedCamino{
				InitialAdmin: xAddress,
				Allocations: []UnparsedCaminoAllocation{{
					ETHAddr:  "0x" + hex.EncodeToString(toShortID.Bytes()),
					AVAXAddr: addressWithInvalidFormat,
				}},
			},
			err: errors.New("no separator found in address"),
		},
		"Invalid allocation - invalid nodeID": {
			unparsedCamino: UnparsedCamino{
				VerifyNodeSignature: true,
				LockModeBondDeposit: true,
				InitialAdmin:        xAddress,
				DepositOffers:       nil,
				Allocations: []UnparsedCaminoAllocation{{
					ETHAddr:  "0x" + hex.EncodeToString(toShortID.Bytes()),
					AVAXAddr: xAddress,
					PlatformAllocations: []UnparsedPlatformAllocation{{
						Amount:            1,
						NodeID:            sampleShortID.String(),
						ValidatorDuration: 1,
					}},
				}},
			},
			err: fmt.Errorf("ID: %s is missing the prefix: %s", sampleShortID.String(), ids.NodeIDPrefix),
		},
		"Valid allocation": {
			unparsedCamino: UnparsedCamino{
				VerifyNodeSignature: true,
				LockModeBondDeposit: true,
				InitialAdmin:        xAddress,
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
					ETHAddr:  "0x" + hex.EncodeToString(toShortID.Bytes()),
					AVAXAddr: xAddress,
					PlatformAllocations: []UnparsedPlatformAllocation{{
						Amount:            1,
						NodeID:            ids.NodeIDPrefix + sampleShortID.String(),
						ValidatorDuration: 1,
						DepositDuration:   1,
						DepositOfferMemo:  "offer memo",
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
			want: Camino{
				VerifyNodeSignature: true,
				LockModeBondDeposit: true,
				InitialAdmin:        toAvaxAddr(xAddress),
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
					ETHAddr: func() ids.ShortID {
						i, _ := ids.ShortFromString("0x" + hex.EncodeToString(toShortID.Bytes()))
						return i
					}(),
					AVAXAddr: toAvaxAddr(xAddress),
					PlatformAllocations: []PlatformAllocation{{
						Amount:            1,
						NodeID:            ids.NodeID(sampleShortID),
						ValidatorDuration: 1,
						DepositDuration:   1,
						DepositOfferMemo:  "offer memo",
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
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got, err := tt.unparsedCamino.Parse(0)

			if tt.err != nil {
				require.ErrorContains(t, err, tt.err.Error())
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestParsingAndUnparsingDepositOffer(t *testing.T) {
	tests := map[string]struct {
		startTime uint64
		udo       UnparsedDepositOffer
	}{
		"Columbus 8% offer with OfferID": {
			startTime: uint64(1671058800),
			udo: UnparsedDepositOffer{
				InterestRateNominator:   80000,
				StartOffset:             0,
				EndOffset:               112795200,
				MinAmount:               1,
				MinDuration:             110376000,
				MaxDuration:             110376000,
				UnlockPeriodDuration:    31536000,
				NoRewardsPeriodDuration: 15768000,
				Flags: UnparsedDepositOfferFlags{
					Locked: true,
				},
			},
		},
		"Camino 8% offer with OfferID - diff StartTime & OfferID": {
			startTime: uint64(1670956381),
			udo: UnparsedDepositOffer{
				InterestRateNominator:   80000,
				StartOffset:             0,
				EndOffset:               112795200,
				MinAmount:               1,
				MinDuration:             110376000,
				MaxDuration:             110376000,
				UnlockPeriodDuration:    31536000,
				NoRewardsPeriodDuration: 15768000,
				Flags: UnparsedDepositOfferFlags{
					Locked: true,
				},
			},
		},
		"0% Camino with OfferID, no Flags": {
			startTime: uint64(1670956381),
			udo: UnparsedDepositOffer{
				InterestRateNominator:   0,
				StartOffset:             0,
				EndOffset:               158889600,
				MinAmount:               1,
				MinDuration:             157680000,
				MaxDuration:             157680000,
				UnlockPeriodDuration:    63072000,
				NoRewardsPeriodDuration: 31536000,
			},
		},
		"Template does require OfferID as well": {
			startTime: uint64(0),
			udo: UnparsedDepositOffer{
				InterestRateNominator:   11,
				StartOffset:             0,
				EndOffset:               63072000,
				MinAmount:               1,
				MinDuration:             60,
				MaxDuration:             31536000,
				UnlockPeriodDuration:    0,
				NoRewardsPeriodDuration: 0,
			},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			do, err := tt.udo.Parse(tt.startTime)
			require.NoError(t, err)

			// Don't check template equality
			if tt.startTime > 0 {
				udo, err := do.Unparse(tt.startTime)
				require.NoError(t, err)
				require.Equal(t, tt.udo, udo)
			}
		})
	}
}

func toAvaxAddr(id string) ids.ShortID {
	_, _, avaxAddrBytes, _ := address.Parse(id)
	avaxAddr, _ := ids.ToShortID(avaxAddrBytes)
	return avaxAddr
}
