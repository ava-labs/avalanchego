// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
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
	"github.com/ava-labs/avalanchego/vms/platformvm/genesis"
	"github.com/stretchr/testify/require"
)

var (
	sampleID                    = ids.GenerateTestID()
	sampleIDWithInvalidChecksum = "Zda4gsqTjRaX6XVZekVNi3ovMFPHDRQiGbzYuAb7Nwqy1rGFc"
	sampleShortID               = ids.GenerateTestShortID()
	addressWithInvalidFormat    = ids.GenerateTestShortID().String()
	addressWithInvalidChecksum  = "X-camino1859dz2uwazfgahey3j53ef2kqrans0c8htcu8n"
	xAddress                    = "X-camino1859dz2uwazfgahey3j53ef2kqrans0c8htcu7n"
	cAddress                    = "C-camino1859dz2uwazfgahey3j53ef2kqrans0c8htcu7n"
	toShortID                   = wrappers.IgnoreError(ids.ToShortID([]byte(cAddress))).(ids.ShortID)
)

func TestParse(t *testing.T) {
	type fields struct {
		VerifyNodeSignature   bool
		LockModeBondDeposit   bool
		InitialAdmin          string
		DepositOffers         []UnparsedDepositOffer
		Allocations           []UnparsedCaminoAllocation
		UnparsedMultisigAlias []UnparsedMultisigAlias
	}
	tests := map[string]struct {
		fields fields
		want   Camino
		err    error
	}{
		"Invalid address - no prefix": {
			fields: fields{
				InitialAdmin: addressWithInvalidFormat,
			},
			err: errCannotParseInitialAdmin,
		},
		"Invalid address - bad checksum": {
			fields: fields{
				InitialAdmin: addressWithInvalidChecksum,
			},
			err: errCannotParseInitialAdmin,
		},
		"Invalid allocation - missing eth address": {
			fields: fields{
				InitialAdmin: xAddress,
				Allocations:  []UnparsedCaminoAllocation{{}},
			},
			err: errInvalidETHAddress,
		},
		"Invalid allocation - invalid eth address": {
			fields: fields{
				InitialAdmin: xAddress,
				Allocations: []UnparsedCaminoAllocation{{
					ETHAddr: ids.GenerateTestShortID().String(),
				}},
			},
			err: errors.New("encoding/hex: invalid byte"),
		},
		"Invalid allocation - invalid avax address": {
			fields: fields{
				InitialAdmin: xAddress,
				Allocations: []UnparsedCaminoAllocation{{
					ETHAddr:  "0x" + hex.EncodeToString(toShortID.Bytes()),
					AVAXAddr: addressWithInvalidFormat,
				}},
			},
			err: errors.New("no separator found in address"),
		},
		"Invalid allocation - invalid depositOfferID": {
			fields: fields{
				VerifyNodeSignature: true,
				LockModeBondDeposit: true,
				InitialAdmin:        xAddress,
				DepositOffers:       nil,
				Allocations: []UnparsedCaminoAllocation{{
					ETHAddr:  "0x" + hex.EncodeToString(toShortID.Bytes()),
					AVAXAddr: xAddress,
					PlatformAllocations: []UnparsedPlatformAllocation{{
						Amount:            1,
						NodeID:            ids.NodeIDPrefix + sampleShortID.String(),
						ValidatorDuration: 1,
						DepositOfferID:    sampleIDWithInvalidChecksum,
					}},
				}},
			},
			err: errors.New("invalid input checksum"),
		},
		"Invalid allocation - invalid nodeID": {
			fields: fields{
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
						DepositOfferID:    sampleID.String(),
					}},
				}},
			},
			err: fmt.Errorf("ID: %s is missing the prefix: %s", sampleShortID.String(), ids.NodeIDPrefix),
		},
		"Valid allocation": {
			fields: fields{
				VerifyNodeSignature: true,
				LockModeBondDeposit: true,
				InitialAdmin:        xAddress,
				DepositOffers:       nil,
				Allocations: []UnparsedCaminoAllocation{{
					ETHAddr:  "0x" + hex.EncodeToString(toShortID.Bytes()),
					AVAXAddr: xAddress,
					PlatformAllocations: []UnparsedPlatformAllocation{{
						Amount:            1,
						NodeID:            ids.NodeIDPrefix + sampleShortID.String(),
						ValidatorDuration: 1,
						DepositOfferID:    sampleID.String(),
					}},
				}},
				UnparsedMultisigAlias: []UnparsedMultisigAlias{{
					Alias:     wrappers.IgnoreError(address.Format(configChainIDAlias, "local", sampleShortID.Bytes())).(string),
					Threshold: 1,
					Addresses: []string{wrappers.IgnoreError(address.Format(configChainIDAlias, "local", shortID2.Bytes())).(string)},
				}},
			},
			want: Camino{
				VerifyNodeSignature: true,
				LockModeBondDeposit: true,
				InitialAdmin:        toAvaxAddr(xAddress),
				DepositOffers:       []genesis.DepositOffer{},
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
						DepositOfferID:    sampleID,
					}},
				}},
				InitialMultisigAddresses: []genesis.MultisigAlias{{
					Alias:     sampleShortID,
					Threshold: 1,
					Addresses: []ids.ShortID{shortID2},
				}},
			},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			uc := UnparsedCamino{
				VerifyNodeSignature:      tt.fields.VerifyNodeSignature,
				LockModeBondDeposit:      tt.fields.LockModeBondDeposit,
				InitialAdmin:             tt.fields.InitialAdmin,
				DepositOffers:            tt.fields.DepositOffers,
				Allocations:              tt.fields.Allocations,
				InitialMultisigAddresses: tt.fields.UnparsedMultisigAlias,
			}
			got, err := uc.Parse(0)

			if tt.err != nil {
				require.ErrorContains(t, err, tt.err.Error())
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func toAvaxAddr(id string) ids.ShortID {
	_, _, avaxAddrBytes, _ := address.Parse(id)
	avaxAddr, _ := ids.ToShortID(avaxAddrBytes)
	return avaxAddr
}
