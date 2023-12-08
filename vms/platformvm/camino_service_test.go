// Copyright (C) 2022-2023, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"context"
	"fmt"
	"testing"

	json_api "github.com/ava-labs/avalanchego/api"
	"github.com/ava-labs/avalanchego/api/keystore"
	"github.com/ava-labs/avalanchego/database/manager"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/utils/formatting/address"
	"github.com/ava-labs/avalanchego/utils/json"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/api"
	"github.com/ava-labs/avalanchego/vms/platformvm/deposit"
	"github.com/ava-labs/avalanchego/vms/platformvm/locked"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/stretchr/testify/require"
)

// Test method GetBalance in CaminoService
func TestGetCaminoBalance(t *testing.T) {
	hrp := constants.NetworkIDToHRP[testNetworkID]

	id := caminoPreFundedKeys[0].PublicKey().Address()
	addr, err := address.FormatBech32(hrp, id.Bytes())
	require.NoError(t, err)

	tests := map[string]struct {
		camino          api.Camino
		genesisUTXOs    []api.UTXO // unlocked utxos
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
			genesisUTXOs: []api.UTXO{
				{
					Amount:  json.Uint64(defaultBalance),
					Address: addr,
				},
			},
			address: addr,
			bonded:  defaultWeight,
		},
		"Genesis Validator with deposited amount": {
			camino: api.Camino{
				LockModeBondDeposit: true,
			},
			genesisUTXOs: []api.UTXO{
				{
					Amount:  json.Uint64(defaultBalance),
					Address: addr,
				},
			},
			address:   addr,
			bonded:    defaultWeight,
			deposited: defaultBalance,
		},
		"Genesis Validator with depositedBonded amount": {
			camino: api.Camino{
				LockModeBondDeposit: true,
			},
			genesisUTXOs: []api.UTXO{
				{
					Amount:  json.Uint64(defaultBalance),
					Address: addr,
				},
			},
			address:         addr,
			bonded:          defaultWeight,
			depositedBonded: defaultBalance,
		},
		"Genesis Validator with added balance and disabled LockModeBondDeposit": {
			camino: api.Camino{
				LockModeBondDeposit: false,
			},
			genesisUTXOs: []api.UTXO{
				{
					Amount:  json.Uint64(defaultBalance),
					Address: addr,
				},
			},
			address: addr,
			bonded:  defaultWeight,
		},
		"Error - Empty address ": {
			camino: api.Camino{
				LockModeBondDeposit: true,
			},
			expectedError: fmt.Errorf("couldn't parse address %q: %s", "P-", ""),
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			service := defaultCaminoService(t, tt.camino, tt.genesisUTXOs)
			service.vm.ctx.Lock.Lock()
			defer func() {
				if err := service.vm.Shutdown(context.TODO()); err != nil {
					t.Fatal(err)
				}
				service.vm.ctx.Lock.Unlock()
			}()

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
					Addrs:     []ids.ShortID{keys[0].PublicKey().Address()},
				}
				utxo := generateTestUTXO(ids.GenerateTestID(), avaxAssetID, tt.deposited, outputOwners, ids.GenerateTestID(), ids.Empty)
				service.vm.state.AddUTXO(utxo)
				err := service.vm.state.Commit()
				require.NoError(t, err)
			}

			if tt.bonded != 0 {
				outputOwners := secp256k1fx.OutputOwners{
					Locktime:  0,
					Threshold: 1,
					Addrs:     []ids.ShortID{keys[0].PublicKey().Address()},
				}
				utxo := generateTestUTXO(ids.GenerateTestID(), avaxAssetID, tt.bonded, outputOwners, ids.Empty, ids.GenerateTestID())
				service.vm.state.AddUTXO(utxo)
				err := service.vm.state.Commit()
				require.NoError(t, err)
			}

			if tt.depositedBonded != 0 {
				outputOwners := secp256k1fx.OutputOwners{
					Locktime:  0,
					Threshold: 1,
					Addrs:     []ids.ShortID{keys[0].PublicKey().Address()},
				}
				utxo := generateTestUTXO(ids.GenerateTestID(), avaxAssetID, tt.depositedBonded, outputOwners, ids.GenerateTestID(), ids.GenerateTestID())
				service.vm.state.AddUTXO(utxo)
				err := service.vm.state.Commit()
				require.NoError(t, err)
			}

			err := service.GetBalance(nil, &request, &responseWrapper)
			if tt.expectedError != nil {
				require.ErrorContains(t, err, tt.expectedError.Error())
				return
			}
			require.NoError(t, err)
			expectedBalance := json.Uint64(defaultCaminoValidatorWeight + defaultBalance + tt.bonded + tt.deposited + tt.depositedBonded)

			if !tt.camino.LockModeBondDeposit {
				response := responseWrapper.avax
				require.Equal(t, json.Uint64(defaultBalance), response.Balance, "Wrong balance. Expected %d ; Returned %d", json.Uint64(defaultBalance), response.Balance)
				require.Equal(t, json.Uint64(0), response.LockedStakeable, "Wrong locked stakeable balance. Expected %d ; Returned %d", 0, response.LockedStakeable)
				require.Equal(t, json.Uint64(0), response.LockedNotStakeable, "Wrong locked not stakeable balance. Expected %d ; Returned %d", 0, response.LockedNotStakeable)
				require.Equal(t, json.Uint64(defaultBalance), response.Unlocked, "Wrong unlocked balance. Expected %d ; Returned %d", defaultBalance, response.Unlocked)
			} else {
				response := responseWrapper.camino
				require.Equal(t, json.Uint64(defaultCaminoValidatorWeight+defaultBalance+tt.bonded+tt.deposited+tt.depositedBonded), response.Balances[avaxAssetID], "Wrong balance. Expected %d ; Returned %d", expectedBalance, response.Balances[avaxAssetID])
				require.Equal(t, json.Uint64(tt.deposited), response.DepositedOutputs[avaxAssetID], "Wrong deposited balance. Expected %d ; Returned %d", tt.deposited, response.DepositedOutputs[avaxAssetID])
				require.Equal(t, json.Uint64(defaultCaminoValidatorWeight+tt.bonded), response.BondedOutputs[avaxAssetID], "Wrong bonded balance. Expected %d ; Returned %d", tt.bonded, response.BondedOutputs[avaxAssetID])
				require.Equal(t, json.Uint64(tt.depositedBonded), response.DepositedBondedOutputs[avaxAssetID], "Wrong depositedBonded balance. Expected %d ; Returned %d", tt.depositedBonded, response.DepositedBondedOutputs[avaxAssetID])
				require.Equal(t, json.Uint64(defaultBalance), response.UnlockedOutputs[avaxAssetID], "Wrong unlocked balance. Expected %d ; Returned %d", defaultBalance, response.UnlockedOutputs[avaxAssetID])
			}
		})
	}
}

func defaultCaminoService(t *testing.T, camino api.Camino, utxos []api.UTXO) *CaminoService {
	vm := newCaminoVM(camino, utxos, nil)

	vm.ctx.Lock.Lock()
	defer vm.ctx.Lock.Unlock()
	ks := keystore.New(logging.NoLog{}, manager.NewMemDB(version.Semantic1_0_0))
	if err := ks.CreateUser(testUsername, testPassword); err != nil {
		t.Fatal(err)
	}
	vm.ctx.Keystore = ks.NewBlockchainKeyStore(vm.ctx.ChainID)
	return &CaminoService{
		Service: Service{
			vm:          vm,
			addrManager: avax.NewAddressManager(vm.ctx),
		},
	}
}

func generateTestUTXO(txID ids.ID, assetID ids.ID, amount uint64, outputOwners secp256k1fx.OutputOwners, depositTxID, bondTxID ids.ID) *avax.UTXO {
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
	return &avax.UTXO{
		UTXOID: avax.UTXOID{TxID: txID},
		Asset:  avax.Asset{ID: assetID},
		Out:    out,
	}
}

func TestCaminoService_GetAllDepositOffers(t *testing.T) {
	type fields struct {
		Service CaminoService
	}
	type args struct {
		depositOffersArgs *GetAllDepositOffersArgs
		response          *GetAllDepositOffersReply
	}
	tests := map[string]struct {
		fields  fields
		args    args
		want    []*APIDepositOffer
		wantErr error
		prepare func(service CaminoService)
	}{
		"OK": {
			fields: fields{
				Service: *defaultCaminoService(t, api.Camino{}, []api.UTXO{}),
			},
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
			prepare: func(service CaminoService) {
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
			tt.prepare(tt.fields.Service)
			err := tt.fields.Service.GetAllDepositOffers(nil, tt.args.depositOffersArgs, tt.args.response)
			require.ErrorIs(t, err, tt.wantErr)
			require.ElementsMatch(t, tt.want, tt.args.response.DepositOffers)
		})
	}
}

func TestGetKeystoreKeys(t *testing.T) {
	s, _ := defaultService(t)
	userPass := json_api.UserPass{Username: testUsername, Password: testPassword}
	// Insert testAddress into keystore
	defaultAddress(t, s)
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
		"Not OK - From and signer are same": {
			from: json_api.JSONFromAddrs{
				Signer: []string{testAddress},
			},
			expectedError: errNoKeys,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
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
	s, _ := defaultService(t)

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
	hrp := constants.NetworkIDToHRP[testNetworkID]
	id := keys[0].PublicKey().Address()
	addr, err := address.FormatBech32(hrp, id.Bytes())
	require.NoError(t, err)

	service := defaultCaminoService(
		t,
		api.Camino{
			LockModeBondDeposit: true,
		},
		[]api.UTXO{{
			Locktime: 0,
			Amount:   100,
			Address:  addr,
			Message:  "",
		}},
	)

	spendArgs := SpendArgs{
		JSONFromAddrs: json_api.JSONFromAddrs{
			From: []string{"P-" + addr},
		},
		AmountToBurn: 50,
		Encoding:     formatting.Hex,
		To: api.Owner{
			Threshold: 1,
			Addresses: []string{"P-" + addr},
		},
	}

	spendReply := SpendReply{}

	err = service.Spend(nil, &spendArgs, &spendReply)
	require.NoError(t, err)
	require.Equal(t, "0x00000000000100000000000000000000000100000001fceda8f90fcb5d30614b99d79fc4baa2930776262dcf0a4e", spendReply.Owners)
}
