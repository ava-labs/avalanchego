// Copyright (C) 2022-2024, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"fmt"
	"testing"

	"github.com/btcsuite/btcd/btcutil/bech32"
	"github.com/stretchr/testify/require"

	json_api "github.com/ava-labs/avalanchego/api"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/utils/formatting/address"
	"github.com/ava-labs/avalanchego/utils/json"
	"github.com/ava-labs/avalanchego/vms/platformvm/api"
	"github.com/ava-labs/avalanchego/vms/platformvm/deposit"
	"github.com/ava-labs/avalanchego/vms/platformvm/test"
	"github.com/ava-labs/avalanchego/vms/platformvm/test/generate"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

// Test method GetBalance in CaminoService
func TestGetCaminoBalance(t *testing.T) {
	tests := map[string]struct {
		camino          api.Camino
		address         string
		bonded          uint64 // additional (to existing genesis validator bond) bonded utxos
		deposited       uint64 // additional deposited utxos
		depositedBonded uint64 // additional depositedBonded utxos
		expectedError   error
	}{
		"Genesis Validator with added balance": {
			camino: api.Camino{
				LockModeBondDeposit: true,
			},
			address: test.FundedKeysBech32[0],
			bonded:  defaultWeight,
		},
		"Genesis Validator with deposited amount": {
			camino: api.Camino{
				LockModeBondDeposit: true,
			},
			address:   test.FundedKeysBech32[0],
			bonded:    defaultWeight,
			deposited: test.PreFundedBalance,
		},
		"Genesis Validator with depositedBonded amount": {
			camino: api.Camino{
				LockModeBondDeposit: true,
			},
			address:         test.FundedKeysBech32[0],
			bonded:          defaultWeight,
			depositedBonded: test.PreFundedBalance,
		},
		"Genesis Validator with added balance and disabled LockModeBondDeposit": {
			camino: api.Camino{
				LockModeBondDeposit: false,
			},
			address: test.FundedKeysBech32[0],
			bonded:  defaultWeight,
		},
		"Error - Empty address ": {
			camino: api.Camino{
				LockModeBondDeposit: true,
			},
			expectedError: bech32.ErrInvalidLength(0),
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			service := newCaminoService(t, tt.camino, test.PhaseLast, nil)
			service.vm.ctx.Lock.Lock()
			defer service.vm.ctx.Lock.Unlock()

			request := GetBalanceRequest{
				Addresses: []string{
					fmt.Sprintf("P-%s", tt.address),
				},
			}
			responseWrapper := GetBalanceResponseWrapper{}

			if tt.deposited != 0 {
				outputOwners := secp256k1fx.OutputOwners{
					Locktime:  0,
					Threshold: 1,
					Addrs:     []ids.ShortID{keys[0].Address()},
				}
				utxo := generate.UTXO(ids.GenerateTestID(), service.vm.ctx.AVAXAssetID, tt.deposited, outputOwners, ids.GenerateTestID(), ids.Empty, true)
				service.vm.state.AddUTXO(utxo)
				require.NoError(t, service.vm.state.Commit())
			}

			if tt.bonded != 0 {
				outputOwners := secp256k1fx.OutputOwners{
					Locktime:  0,
					Threshold: 1,
					Addrs:     []ids.ShortID{keys[0].Address()},
				}
				utxo := generate.UTXO(ids.GenerateTestID(), service.vm.ctx.AVAXAssetID, tt.bonded, outputOwners, ids.Empty, ids.GenerateTestID(), true)
				service.vm.state.AddUTXO(utxo)
				require.NoError(t, service.vm.state.Commit())
			}

			if tt.depositedBonded != 0 {
				outputOwners := secp256k1fx.OutputOwners{
					Locktime:  0,
					Threshold: 1,
					Addrs:     []ids.ShortID{keys[0].Address()},
				}
				utxo := generate.UTXO(ids.GenerateTestID(), service.vm.ctx.AVAXAssetID, tt.depositedBonded, outputOwners, ids.GenerateTestID(), ids.GenerateTestID(), true)
				service.vm.state.AddUTXO(utxo)
				require.NoError(t, service.vm.state.Commit())
			}

			err := service.GetBalance(nil, &request, &responseWrapper)
			require.ErrorIs(t, err, tt.expectedError)
			if tt.expectedError != nil {
				return
			}

			if !tt.camino.LockModeBondDeposit {
				response := responseWrapper.avax
				require.Equal(t, json.Uint64(test.PreFundedBalance), response.Balance, "Wrong balance. Expected %d ; Returned %d", json.Uint64(test.PreFundedBalance), response.Balance)
				require.Equal(t, json.Uint64(0), response.LockedStakeable, "Wrong locked stakeable balance. Expected %d ; Returned %d", 0, response.LockedStakeable)
				require.Equal(t, json.Uint64(0), response.LockedNotStakeable, "Wrong locked not stakeable balance. Expected %d ; Returned %d", 0, response.LockedNotStakeable)
				require.Equal(t, json.Uint64(test.PreFundedBalance), response.Unlocked, "Wrong unlocked balance. Expected %d ; Returned %d", test.PreFundedBalance, response.Unlocked)
			} else {
				expectedBalance := json.Uint64(test.ValidatorWeight + test.PreFundedBalance + tt.bonded + tt.deposited + tt.depositedBonded)
				response := responseWrapper.camino
				require.Equal(t, expectedBalance, response.Balances[service.vm.ctx.AVAXAssetID], "Wrong balance. Expected %d ; Returned %d", expectedBalance, response.Balances[service.vm.ctx.AVAXAssetID])
				require.Equal(t, json.Uint64(tt.deposited), response.DepositedOutputs[service.vm.ctx.AVAXAssetID], "Wrong deposited balance. Expected %d ; Returned %d", tt.deposited, response.DepositedOutputs[service.vm.ctx.AVAXAssetID])
				require.Equal(t, json.Uint64(test.ValidatorWeight+tt.bonded), response.BondedOutputs[service.vm.ctx.AVAXAssetID], "Wrong bonded balance. Expected %d ; Returned %d", tt.bonded, response.BondedOutputs[service.vm.ctx.AVAXAssetID])
				require.Equal(t, json.Uint64(tt.depositedBonded), response.DepositedBondedOutputs[service.vm.ctx.AVAXAssetID], "Wrong depositedBonded balance. Expected %d ; Returned %d", tt.depositedBonded, response.DepositedBondedOutputs[service.vm.ctx.AVAXAssetID])
				require.Equal(t, json.Uint64(test.PreFundedBalance), response.UnlockedOutputs[service.vm.ctx.AVAXAssetID], "Wrong unlocked balance. Expected %d ; Returned %d", test.PreFundedBalance, response.UnlockedOutputs[service.vm.ctx.AVAXAssetID])
			}
		})
	}
}

func TestCaminoService_GetAllDepositOffers(t *testing.T) {
	type args struct {
		depositOffersArgs *GetAllDepositOffersArgs
		response          *GetAllDepositOffersReply
	}
	tests := map[string]struct {
		args    args
		want    []*APIDepositOffer
		wantErr error
		prepare func(service *CaminoService)
	}{
		"OK": {
			args: args{
				depositOffersArgs: &GetAllDepositOffersArgs{
					Timestamp: 50,
				},
				response: &GetAllDepositOffersReply{},
			},
			want: []*APIDepositOffer{
				{
					ID:    ids.ID{1},
					Start: 0,
					End:   50,
				},
				{
					ID:    ids.ID{2},
					Start: 0,
					End:   100,
				},
				{
					ID:    ids.ID{3},
					Start: 50,
					End:   100,
				},
			},
			prepare: func(service *CaminoService) {
				service.vm.state.SetDepositOffer(&deposit.Offer{
					ID:    ids.ID{0},
					Start: 0,
					End:   49, // end before timestamp
				})
				service.vm.state.SetDepositOffer(&deposit.Offer{
					ID:    ids.ID{1},
					Start: 0,
					End:   50, // end at timestamp
				})
				service.vm.state.SetDepositOffer(&deposit.Offer{
					ID:    ids.ID{2},
					Start: 0,
					End:   100,
				})
				service.vm.state.SetDepositOffer(&deposit.Offer{
					ID:    ids.ID{3},
					Start: 50, // start at timestamp
					End:   100,
				})
				service.vm.state.SetDepositOffer(&deposit.Offer{
					ID:    ids.ID{4},
					Start: 51, // start after timestamp
					End:   100,
				})
			},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			s := newCaminoService(t, api.Camino{}, test.PhaseLast, []api.UTXO{})
			s.vm.ctx.Lock.Lock()
			defer s.vm.ctx.Lock.Unlock()

			tt.prepare(s)

			err := s.GetAllDepositOffers(nil, tt.args.depositOffersArgs, tt.args.response)
			require.ErrorIs(t, err, tt.wantErr)
			require.ElementsMatch(t, tt.want, tt.args.response.DepositOffers)
		})
	}
}

func TestGetKeystoreKeys(t *testing.T) {
	userPass := json_api.UserPass{Username: testUsername, Password: testPassword}

	testAddressID, err := address.ParseToID(testAddress)
	require.NoError(t, err)

	tests := map[string]struct {
		from          json_api.JSONFromAddrs
		expectedAddrs []ids.ShortID
		expectedError error
	}{
		"OK - No signers": {
			from: json_api.JSONFromAddrs{
				From: []string{testAddress},
			},
			expectedAddrs: []ids.ShortID{testAddressID},
		},
		"OK - From and signer are same": {
			from: json_api.JSONFromAddrs{
				From:   []string{testAddress},
				Signer: []string{testAddress},
			},
			expectedAddrs: []ids.ShortID{testAddressID, ids.ShortEmpty, testAddressID},
		},
		"Not OK - From and signer are same": {
			from: json_api.JSONFromAddrs{
				Signer: []string{testAddress},
			},
			expectedError: errNoKeys,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			s := newCaminoService(t, api.Camino{LockModeBondDeposit: true}, test.PhaseLast, nil)
			defaultAddress(t, &s.Service) // Insert testAddress into keystore
			s.vm.ctx.Lock.Lock()
			defer s.vm.ctx.Lock.Unlock()

			keys, err := s.getKeystoreKeys(&userPass, &tt.from)
			require.ErrorIs(t, err, tt.expectedError)

			for index, key := range keys {
				if key == nil {
					require.Equal(t, tt.expectedAddrs[index], ids.ShortEmpty)
				} else {
					require.Equal(t, tt.expectedAddrs[index], key.Address())
				}
			}
		})
	}
}

func TestGetFakeKeys(t *testing.T) {
	_, _, testAddressBytes, _ := address.Parse(testAddress)
	testAddressID, _ := ids.ToShortID(testAddressBytes)

	tests := map[string]struct {
		from          json_api.JSONFromAddrs
		expectedAddrs []ids.ShortID
		expectedError error
	}{
		"OK - No signers": {
			from: json_api.JSONFromAddrs{
				From: []string{testAddress},
			},
			expectedAddrs: []ids.ShortID{testAddressID},
		},
		"OK - From and signer are same": {
			from: json_api.JSONFromAddrs{
				From:   []string{testAddress},
				Signer: []string{testAddress},
			},
			expectedAddrs: []ids.ShortID{testAddressID, ids.ShortEmpty, testAddressID},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			s := newCaminoService(t, api.Camino{LockModeBondDeposit: true}, test.PhaseLast, nil)
			s.vm.ctx.Lock.Lock()
			defer s.vm.ctx.Lock.Unlock()

			keys, err := s.getFakeKeys(&tt.from)
			require.ErrorIs(t, err, tt.expectedError)

			for index, key := range keys {
				if key == nil {
					require.Equal(t, tt.expectedAddrs[index], ids.ShortEmpty)
				} else {
					require.Equal(t, tt.expectedAddrs[index], key.Address())
				}
			}
		})
	}
}

func TestSpend(t *testing.T) {
	service := newCaminoService(
		t,
		api.Camino{
			LockModeBondDeposit: true,
		},
		test.PhaseLast,
		[]api.UTXO{{
			Locktime: 0,
			Amount:   100,
			Address:  test.FundedKeysBech32[0],
			Message:  "",
		}},
	)

	spendArgs := SpendArgs{
		JSONFromAddrs: json_api.JSONFromAddrs{
			From: []string{"P-" + test.FundedKeysBech32[0]},
		},
		AmountToBurn: 50,
		Encoding:     formatting.Hex,
		To: api.Owner{
			Threshold: 1,
			Addresses: []string{"P-" + test.FundedKeysBech32[0]},
		},
	}

	spendReply := SpendReply{}

	require.NoError(t, service.Spend(nil, &spendArgs, &spendReply))
	require.Equal(t, "0x00000000000100000000000000000000000100000001fceda8f90fcb5d30614b99d79fc4baa2930776262dcf0a4e", spendReply.Owners)
}
