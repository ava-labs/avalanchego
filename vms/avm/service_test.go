// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanche-go/api"
	"github.com/ava-labs/avalanche-go/api/keystore"
	"github.com/ava-labs/avalanche-go/chains/atomic"
	"github.com/ava-labs/avalanche-go/ids"
	"github.com/ava-labs/avalanche-go/snow/choices"
	"github.com/ava-labs/avalanche-go/utils/constants"
	"github.com/ava-labs/avalanche-go/utils/crypto"
	"github.com/ava-labs/avalanche-go/utils/formatting"
	"github.com/ava-labs/avalanche-go/utils/json"
	"github.com/ava-labs/avalanche-go/vms/components/avax"
	"github.com/ava-labs/avalanche-go/vms/secp256k1fx"
)

func setup(t *testing.T) ([]byte, *VM, *Service, *atomic.Memory) {
	genesisBytes, _, vm, m := GenesisVM(t)
	keystore := keystore.CreateTestKeystore()
	keystore.AddUser(username, password)
	vm.ctx.Keystore = keystore.NewBlockchainKeyStore(chainID)
	s := &Service{vm: vm}
	return genesisBytes, vm, s, m
}

func setupWithKeys(t *testing.T) ([]byte, *VM, *Service, *atomic.Memory) {
	genesisBytes, vm, s, m := setup(t)

	// Import the initially funded private keys
	user := userState{vm: vm}
	db, err := s.vm.ctx.Keystore.GetDatabase(username, password)
	if err != nil {
		t.Fatalf("Failed to get user database: %s", err)
	}

	addrs := []ids.ShortID{}
	for _, sk := range keys {
		if err := user.SetKey(db, sk); err != nil {
			t.Fatalf("Failed to set key for user: %s", err)
		}
		addrs = append(addrs, sk.PublicKey().Address())
	}
	if err := user.SetAddresses(db, addrs); err != nil {
		t.Fatalf("Failed to set user addresses: %s", err)
	}
	return genesisBytes, vm, s, m
}

func TestServiceIssueTx(t *testing.T) {
	genesisBytes, vm, s, _ := setup(t)
	defer func() {
		vm.Shutdown()
		vm.ctx.Lock.Unlock()
	}()

	txArgs := &FormattedTx{}
	txReply := &api.JsonTxID{}
	err := s.IssueTx(nil, txArgs, txReply)
	if err == nil {
		t.Fatal("Expected empty transaction to return an error")
	}

	tx := NewTx(t, genesisBytes, vm)
	txArgs.Tx = formatting.CB58{Bytes: tx.Bytes()}
	txReply = &api.JsonTxID{}
	if err := s.IssueTx(nil, txArgs, txReply); err != nil {
		t.Fatal(err)
	}
	if !txReply.TxID.Equals(tx.ID()) {
		t.Fatalf("Expected %q, got %q", txReply.TxID, tx.ID())
	}
}

func TestServiceGetTxStatus(t *testing.T) {
	genesisBytes, vm, s, _ := setup(t)
	defer func() {
		vm.Shutdown()
		vm.ctx.Lock.Unlock()
	}()

	statusArgs := &api.JsonTxID{}
	statusReply := &GetTxStatusReply{}
	if err := s.GetTxStatus(nil, statusArgs, statusReply); err == nil {
		t.Fatal("Expected empty transaction to return an error")
	}

	tx := NewTx(t, genesisBytes, vm)
	statusArgs.TxID = tx.ID()
	statusReply = &GetTxStatusReply{}
	if err := s.GetTxStatus(nil, statusArgs, statusReply); err != nil {
		t.Fatal(err)
	}
	if expected := choices.Unknown; expected != statusReply.Status {
		t.Fatalf(
			"Expected an unsubmitted tx to have status %q, got %q",
			expected.String(), statusReply.Status.String(),
		)
	}

	txArgs := &FormattedTx{Tx: formatting.CB58{Bytes: tx.Bytes()}}
	txReply := &api.JsonTxID{}
	if err := s.IssueTx(nil, txArgs, txReply); err != nil {
		t.Fatal(err)
	}
	statusReply = &GetTxStatusReply{}
	if err := s.GetTxStatus(nil, statusArgs, statusReply); err != nil {
		t.Fatal(err)
	}
	if expected := choices.Processing; expected != statusReply.Status {
		t.Fatalf(
			"Expected a submitted tx to have status %q, got %q",
			expected.String(), statusReply.Status.String(),
		)
	}
}

func TestServiceGetBalance(t *testing.T) {
	genesisBytes, vm, s, _ := setup(t)
	defer func() {
		vm.Shutdown()
		vm.ctx.Lock.Unlock()
	}()

	genesisTx := GetFirstTxFromGenesisTest(genesisBytes, t)
	assetID := genesisTx.ID()
	addr := keys[0].PublicKey().Address()
	addrStr, err := vm.FormatLocalAddress(addr)
	if err != nil {
		t.Fatal(err)
	}

	balanceArgs := &GetBalanceArgs{
		Address: addrStr,
		AssetID: assetID.String(),
	}
	balanceReply := &GetBalanceReply{}
	err = s.GetBalance(nil, balanceArgs, balanceReply)
	assert.NoError(t, err)
	assert.Equal(t, uint64(balanceReply.Balance), uint64(300000))

	assert.Len(t, balanceReply.UTXOIDs, 4, "should have only returned four utxoIDs")
}

func TestServiceGetAllBalances(t *testing.T) {
	genesisBytes, vm, s, _ := setup(t)
	defer func() {
		vm.Shutdown()
		vm.ctx.Lock.Unlock()
	}()

	genesisTx := GetFirstTxFromGenesisTest(genesisBytes, t)
	assetID := genesisTx.ID()
	addr := keys[0].PublicKey().Address()
	addrStr, err := vm.FormatLocalAddress(addr)
	if err != nil {
		t.Fatal(err)
	}

	balanceArgs := &api.JsonAddress{
		Address: addrStr,
	}
	balanceReply := &GetAllBalancesReply{}
	err = s.GetAllBalances(nil, balanceArgs, balanceReply)
	assert.NoError(t, err)

	assert.Len(t, balanceReply.Balances, 1)

	balance := balanceReply.Balances[0]
	alias, err := vm.PrimaryAlias(assetID)
	if err != nil {
		t.Fatalf("Failed to get primary alias of genesis asset: %s", err)
	}
	assert.Equal(t, balance.AssetID, alias)
	assert.Equal(t, uint64(balance.Balance), uint64(300000))
}

func TestServiceGetTx(t *testing.T) {
	genesisBytes, vm, s, _ := setup(t)
	defer func() {
		vm.Shutdown()
		vm.ctx.Lock.Unlock()
	}()

	genesisTx := GetFirstTxFromGenesisTest(genesisBytes, t)
	genesisTxBytes := genesisTx.Bytes()
	txID := genesisTx.ID()

	reply := FormattedTx{}
	err := s.GetTx(nil, &api.JsonTxID{
		TxID: txID,
	}, &reply)
	assert.NoError(t, err)
	assert.Equal(t, genesisTxBytes, reply.Tx.Bytes, "Wrong tx returned from service.GetTx")
}

func TestServiceGetNilTx(t *testing.T) {
	_, vm, s, _ := setup(t)
	defer func() {
		vm.Shutdown()
		vm.ctx.Lock.Unlock()
	}()

	reply := FormattedTx{}
	err := s.GetTx(nil, &api.JsonTxID{}, &reply)
	assert.Error(t, err, "Nil TxID should have returned an error")
}

func TestServiceGetUnknownTx(t *testing.T) {
	_, vm, s, _ := setup(t)
	defer func() {
		vm.Shutdown()
		vm.ctx.Lock.Unlock()
	}()

	reply := FormattedTx{}
	err := s.GetTx(nil, &api.JsonTxID{TxID: ids.Empty}, &reply)
	assert.Error(t, err, "Unknown TxID should have returned an error")
}

func TestServiceGetUTXOs(t *testing.T) {
	_, vm, s, m := setup(t)
	defer func() {
		vm.Shutdown()
		vm.ctx.Lock.Unlock()
	}()

	rawAddr := ids.GenerateTestShortID()
	rawEmptyAddr := ids.GenerateTestShortID()

	numUTXOs := 10
	// Put a bunch of UTXOs
	for i := 0; i < numUTXOs; i++ {
		if err := vm.state.FundUTXO(&avax.UTXO{
			UTXOID: avax.UTXOID{
				TxID: ids.GenerateTestID(),
			},
			Asset: avax.Asset{ID: vm.ctx.AVAXAssetID},
			Out: &secp256k1fx.TransferOutput{
				Amt: 1,
				OutputOwners: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{rawAddr},
				},
			},
		}); err != nil {
			t.Fatal(err)
		}
	}

	sm := m.NewSharedMemory(platformChainID)

	elems := make([]*atomic.Element, numUTXOs)
	for i := range elems {
		utxo := &avax.UTXO{
			UTXOID: avax.UTXOID{
				TxID: ids.GenerateTestID(),
			},
			Asset: avax.Asset{ID: vm.ctx.AVAXAssetID},
			Out: &secp256k1fx.TransferOutput{
				Amt: 1,
				OutputOwners: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{rawAddr},
				},
			},
		}

		utxoBytes, err := vm.codec.Marshal(utxo)
		if err != nil {
			t.Fatal(err)
		}
		elems[i] = &atomic.Element{
			Key:   utxo.InputID().Bytes(),
			Value: utxoBytes,
			Traits: [][]byte{
				rawAddr.Bytes(),
			},
		}
	}

	if err := sm.Put(vm.ctx.ChainID, elems); err != nil {
		t.Fatal(err)
	}

	hrp := constants.GetHRP(vm.ctx.NetworkID)
	xAddr, err := vm.FormatLocalAddress(rawAddr)
	if err != nil {
		t.Fatal(err)
	}
	pAddr, err := vm.FormatAddress(platformChainID, rawAddr)
	if err != nil {
		t.Fatal(err)
	}
	unknownChainAddr, err := formatting.FormatAddress("R", hrp, rawAddr.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	xEmptyAddr, err := vm.FormatLocalAddress(rawEmptyAddr)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		label     string
		count     int
		shouldErr bool
		args      *GetUTXOsArgs
	}{
		{
			label:     "invalid address: ''",
			shouldErr: true,
			args: &GetUTXOsArgs{
				Addresses: []string{""},
			},
		},
		{
			label:     "invalid address: '-'",
			shouldErr: true,
			args: &GetUTXOsArgs{
				Addresses: []string{"-"},
			},
		},
		{
			label:     "invalid address: 'foo'",
			shouldErr: true,
			args: &GetUTXOsArgs{
				Addresses: []string{"foo"},
			},
		},
		{
			label:     "invalid address: 'foo-bar'",
			shouldErr: true,
			args: &GetUTXOsArgs{
				Addresses: []string{"foo-bar"},
			},
		},
		{
			label:     "invalid address: '<ChainID>'",
			shouldErr: true,
			args: &GetUTXOsArgs{
				Addresses: []string{vm.ctx.ChainID.String()},
			},
		},
		{
			label:     "invalid address: '<ChainID>-'",
			shouldErr: true,
			args: &GetUTXOsArgs{
				Addresses: []string{fmt.Sprintf("%s-", vm.ctx.ChainID.String())},
			},
		},
		{
			label:     "invalid address: '<Unknown ID>-<addr>'",
			shouldErr: true,
			args: &GetUTXOsArgs{
				Addresses: []string{unknownChainAddr},
			},
		},
		{
			label:     "no addresses",
			shouldErr: true,
			args:      &GetUTXOsArgs{},
		},
		{
			label: "get all X-chain UTXOs",
			count: numUTXOs,
			args: &GetUTXOsArgs{
				Addresses: []string{
					xAddr,
				},
			},
		},
		{
			label: "get one X-chain UTXO",
			count: 1,
			args: &GetUTXOsArgs{
				Addresses: []string{
					xAddr,
				},
				Limit: 1,
			},
		},
		{
			label: "limit greater than number of UTXOs",
			count: numUTXOs,
			args: &GetUTXOsArgs{
				Addresses: []string{
					xAddr,
				},
				Limit: json.Uint32(numUTXOs + 1),
			},
		},
		{
			label: "no utxos to return",
			count: 0,
			args: &GetUTXOsArgs{
				Addresses: []string{
					xEmptyAddr,
				},
			},
		},
		{
			label: "multiple address with utxos",
			count: numUTXOs,
			args: &GetUTXOsArgs{
				Addresses: []string{
					xEmptyAddr,
					xAddr,
				},
			},
		},
		{
			label: "get all P-chain UTXOs",
			count: numUTXOs,
			args: &GetUTXOsArgs{
				Addresses: []string{
					xAddr,
				},
				SourceChain: "P",
			},
		},
		{
			label:     "invalid source chain ID",
			shouldErr: true,
			count:     numUTXOs,
			args: &GetUTXOsArgs{
				Addresses: []string{
					xAddr,
				},
				SourceChain: "HomeRunDerby",
			},
		},
		{
			label: "get all P-chain UTXOs",
			count: numUTXOs,
			args: &GetUTXOsArgs{
				Addresses: []string{
					xAddr,
				},
				SourceChain: "P",
			},
		},
		{
			label:     "get UTXOs from multiple chains",
			shouldErr: true,
			args: &GetUTXOsArgs{
				Addresses: []string{
					xAddr,
					pAddr,
				},
			},
		},
		{
			label:     "get UTXOs for an address on a different chain",
			shouldErr: true,
			args: &GetUTXOsArgs{
				Addresses: []string{
					pAddr,
				},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.label, func(t *testing.T) {
			reply := &GetUTXOsReply{}
			err := s.GetUTXOs(nil, test.args, reply)
			if err != nil {
				if !test.shouldErr {
					t.Fatal(err)
				}
				return
			}
			if test.shouldErr {
				t.Fatal("should have errored")
			}
			if test.count != len(reply.UTXOs) {
				t.Fatalf("Expected %d utxos, got %d", test.count, len(reply.UTXOs))
			}
		})
	}
}

func TestGetAssetDescription(t *testing.T) {
	genesisBytes, vm, s, _ := setup(t)
	defer func() {
		vm.Shutdown()
		vm.ctx.Lock.Unlock()
	}()

	genesisTx := GetFirstTxFromGenesisTest(genesisBytes, t)

	avaxAssetID := genesisTx.ID()

	reply := GetAssetDescriptionReply{}
	err := s.GetAssetDescription(nil, &GetAssetDescriptionArgs{
		AssetID: avaxAssetID.String(),
	}, &reply)
	if err != nil {
		t.Fatal(err)
	}

	if reply.Name != "myFixedCapAsset" {
		t.Fatalf("Wrong name returned from GetAssetDescription %s", reply.Name)
	}
	if reply.Symbol != "MFCA" {
		t.Fatalf("Wrong name returned from GetAssetDescription %s", reply.Symbol)
	}
}

func TestGetBalance(t *testing.T) {
	genesisBytes, vm, s, _ := setup(t)
	defer func() {
		vm.Shutdown()
		vm.ctx.Lock.Unlock()
	}()

	genesisTx := GetFirstTxFromGenesisTest(genesisBytes, t)

	avaxAssetID := genesisTx.ID()

	reply := GetBalanceReply{}
	addrStr, err := vm.FormatLocalAddress(keys[0].PublicKey().Address())
	if err != nil {
		t.Fatal(err)
	}
	err = s.GetBalance(nil, &GetBalanceArgs{
		Address: addrStr,
		AssetID: avaxAssetID.String(),
	}, &reply)
	if err != nil {
		t.Fatal(err)
	}

	if reply.Balance != 300000 {
		t.Fatalf("Wrong balance returned from GetBalance %d", reply.Balance)
	}
}

func TestCreateFixedCapAsset(t *testing.T) {
	_, vm, s, _ := setup(t)
	defer func() {
		vm.Shutdown()
		vm.ctx.Lock.Unlock()
	}()

	reply := FormattedAssetID{}
	addrStr, err := vm.FormatLocalAddress(keys[0].PublicKey().Address())
	if err != nil {
		t.Fatal(err)
	}
	err = s.CreateFixedCapAsset(nil, &CreateFixedCapAssetArgs{
		UserPass: api.UserPass{
			Username: username,
			Password: password,
		},
		Name:         "testAsset",
		Symbol:       "TEST",
		Denomination: 1,
		InitialHolders: []*Holder{{
			Amount:  123456789,
			Address: addrStr,
		}},
	}, &reply)
	if err != nil {
		t.Fatal(err)
	}

	if reply.AssetID.String() != "2YD5ovZNEx7cxryCBxDZaYbrQc3v6AxTDiJwCxkZS1YMFU3Sni" {
		t.Fatalf("Wrong assetID returned from CreateFixedCapAsset %s", reply.AssetID)
	}
}

func TestCreateVariableCapAsset(t *testing.T) {
	_, vm, s, _ := setupWithKeys(t)
	defer func() {
		vm.Shutdown()
		vm.ctx.Lock.Unlock()
	}()

	reply := FormattedAssetID{}
	addrStr, err := vm.FormatLocalAddress(keys[0].PublicKey().Address())
	if err != nil {
		t.Fatal(err)
	}
	err = s.CreateVariableCapAsset(nil, &CreateVariableCapAssetArgs{
		UserPass: api.UserPass{
			Username: username,
			Password: password,
		},
		Name:   "test asset",
		Symbol: "TEST",
		MinterSets: []Owners{
			{
				Threshold: 1,
				Minters: []string{
					addrStr,
				},
			},
		},
	}, &reply)
	if err != nil {
		t.Fatal(err)
	}

	createdAssetID := reply.AssetID.String()

	if createdAssetID != "25BzKomFRYuq52dgoutjDsehgw4v9Uvcgb2fnTBLt8qqTTTAD7" {
		t.Fatalf("Wrong assetID returned from CreateVariableCapAsset %s", reply.AssetID)
	}

	createAssetTx := UniqueTx{
		vm:   vm,
		txID: reply.AssetID,
	}
	if status := createAssetTx.Status(); status != choices.Processing {
		t.Fatalf("CreateVariableCapAssetTx status should have been Processing, but was %s", status)
	}
	if err := createAssetTx.Accept(); err != nil {
		t.Fatalf("Failed to accept CreateVariableCapAssetTx due to: %s", err)
	}

	// Test minting of the created variable cap asset
	mintArgs := &MintArgs{
		UserPass: api.UserPass{
			Username: username,
			Password: password,
		},
		Amount:  200,
		AssetID: createdAssetID,
		To:      addrStr,
	}
	mintReply := &api.JsonTxID{}
	if err := s.Mint(nil, mintArgs, mintReply); err != nil {
		t.Fatalf("Failed to mint variable cap asset due to: %s", err)
	}

	mintTx := UniqueTx{
		vm:   vm,
		txID: mintReply.TxID,
	}

	if status := mintTx.Status(); status != choices.Processing {
		t.Fatalf("MintTx status should have been Processing, but was %s", status)
	}
	if err := mintTx.Accept(); err != nil {
		t.Fatalf("Failed to accept MintTx due to: %s", err)
	}
	sendArgs := &SendArgs{
		UserPass: api.UserPass{
			Username: username,
			Password: password,
		},
		Amount:  200,
		AssetID: createdAssetID,
		To:      addrStr,
	}
	sendReply := &api.JsonTxID{}
	if err := s.Send(nil, sendArgs, sendReply); err != nil {
		t.Fatalf("Failed to send newly minted variable cap asset due to: %s", err)
	}
}

func TestNFTWorkflow(t *testing.T) {
	_, vm, s, _ := setupWithKeys(t)
	defer func() {
		vm.Shutdown()
		vm.ctx.Lock.Unlock()
	}()

	// Test minting of the created variable cap asset
	addrStr, err := vm.FormatLocalAddress(keys[0].PublicKey().Address())
	if err != nil {
		t.Fatal(err)
	}
	createArgs := &CreateNFTAssetArgs{
		UserPass: api.UserPass{
			Username: username,
			Password: password,
		},
		Name:   "BIG COIN",
		Symbol: "COIN",
		MinterSets: []Owners{
			{
				Threshold: 1,
				Minters: []string{
					addrStr,
				},
			},
		},
	}
	createReply := &FormattedAssetID{}
	if err := s.CreateNFTAsset(nil, createArgs, createReply); err != nil {
		t.Fatalf("Failed to mint variable cap asset due to: %s", err)
	}

	assetID := createReply.AssetID
	createNFTTx := UniqueTx{
		vm:   vm,
		txID: createReply.AssetID,
	}
	if createNFTTx.Status() != choices.Processing {
		t.Fatalf("CreateNFTTx should have been processing after creating the NFT")
	}

	// Accept the transaction so that we can Mint NFTs for the test
	if err := createNFTTx.Accept(); err != nil {
		t.Fatalf("Failed to accept CreateNFT transaction: %s", err)
	}

	mintArgs := &MintNFTArgs{
		UserPass: api.UserPass{
			Username: username,
			Password: password,
		},
		AssetID: assetID.String(),
		Payload: formatting.CB58{Bytes: []byte{1, 2, 3, 4, 5}},
		To:      addrStr,
	}
	mintReply := &api.JsonTxID{}

	if err := s.MintNFT(nil, mintArgs, mintReply); err != nil {
		t.Fatalf("MintNFT returned an error: %s", err)
	}

	mintNFTTx := UniqueTx{
		vm:   vm,
		txID: mintReply.TxID,
	}
	if mintNFTTx.Status() != choices.Processing {
		t.Fatal("MintNFTTx should have been processing after minting the NFT")
	}

	// Accept the transaction so that we can send the newly minted NFT
	if err := mintNFTTx.Accept(); err != nil {
		t.Fatalf("Failed to accept MintNFTTx: %s", err)
	}

	sendArgs := &SendNFTArgs{
		UserPass: api.UserPass{
			Username: username,
			Password: password,
		},
		AssetID: assetID.String(),
		GroupID: 0,
		To:      addrStr,
	}
	sendReply := &api.JsonTxID{}
	if err := s.SendNFT(nil, sendArgs, sendReply); err != nil {
		t.Fatalf("Failed to send NFT due to: %s", err)
	}
}

func TestImportExportKey(t *testing.T) {
	_, vm, s, _ := setup(t)
	defer func() {
		vm.Shutdown()
		vm.ctx.Lock.Unlock()
	}()

	factory := crypto.FactorySECP256K1R{}
	skIntf, err := factory.NewPrivateKey()
	if err != nil {
		t.Fatalf("problem generating private key: %s", err)
	}
	sk := skIntf.(*crypto.PrivateKeySECP256K1R)

	formattedKey := formatting.CB58{Bytes: sk.Bytes()}
	importArgs := &ImportKeyArgs{
		UserPass: api.UserPass{
			Username: username,
			Password: password,
		},
		PrivateKey: constants.SecretKeyPrefix + formatting.CB58{Bytes: sk.Bytes()}.String(),
	}
	importReply := &api.JsonAddress{}
	if err = s.ImportKey(nil, importArgs, importReply); err != nil {
		t.Fatal(err)
	}

	addrStr, err := vm.FormatLocalAddress(sk.PublicKey().Address())
	if err != nil {
		t.Fatal(err)
	}
	exportArgs := &ExportKeyArgs{
		UserPass: api.UserPass{
			Username: username,
			Password: password,
		},
		Address: addrStr,
	}
	exportReply := &ExportKeyReply{}
	if err = s.ExportKey(nil, exportArgs, exportReply); err != nil {
		t.Fatal(err)
	}

	if !strings.HasPrefix(exportReply.PrivateKey, constants.SecretKeyPrefix) {
		t.Fatalf("ExportKeyReply private key: %s mssing secret key prefix: %s", exportReply.PrivateKey, constants.SecretKeyPrefix)
	}

	exportedKey := formatting.CB58{}
	if err := exportedKey.FromString(strings.TrimPrefix(exportReply.PrivateKey, constants.SecretKeyPrefix)); err != nil {
		t.Fatal("Failed to parse exported private key")
	}
	if !bytes.Equal(exportedKey.Bytes, formattedKey.Bytes) {
		t.Fatal("Unexpected key was found in ExportKeyReply")
	}
}

func TestImportAVMKeyNoDuplicates(t *testing.T) {
	_, vm, s, _ := setup(t)
	ctx := vm.ctx
	defer func() {
		vm.Shutdown()
		ctx.Lock.Unlock()
	}()

	factory := crypto.FactorySECP256K1R{}
	skIntf, err := factory.NewPrivateKey()
	if err != nil {
		t.Fatalf("problem generating private key: %s", err)
	}
	sk := skIntf.(*crypto.PrivateKeySECP256K1R)

	args := ImportKeyArgs{
		UserPass: api.UserPass{
			Username: username,
			Password: password,
		},
		PrivateKey: constants.SecretKeyPrefix + formatting.CB58{Bytes: sk.Bytes()}.String(),
	}
	reply := api.JsonAddress{}
	if err = s.ImportKey(nil, &args, &reply); err != nil {
		t.Fatal(err)
	}

	expectedAddress, err := vm.FormatLocalAddress(sk.PublicKey().Address())
	if err != nil {
		t.Fatal(err)
	}

	if reply.Address != expectedAddress {
		t.Fatalf("Reply address: %s did not match expected address: %s", reply.Address, expectedAddress)
	}

	reply2 := api.JsonAddress{}
	if err = s.ImportKey(nil, &args, &reply2); err != nil {
		t.Fatal(err)
	}

	if reply2.Address != expectedAddress {
		t.Fatalf("Reply address: %s did not match expected address: %s", reply2.Address, expectedAddress)
	}

	addrsArgs := api.UserPass{
		Username: username,
		Password: password,
	}
	addrsReply := api.JsonAddresses{}
	if err := s.ListAddresses(nil, &addrsArgs, &addrsReply); err != nil {
		t.Fatal(err)
	}

	if len(addrsReply.Addresses) != 1 {
		t.Fatal("Importing the same key twice created duplicate addresses")
	}

	if addrsReply.Addresses[0] != expectedAddress {
		t.Fatal("List addresses returned an incorrect address")
	}
}

func TestSend(t *testing.T) {
	genesisBytes, vm, s, _ := setupWithKeys(t)
	defer func() {
		vm.Shutdown()
		vm.ctx.Lock.Unlock()
	}()

	genesisTx := GetFirstTxFromGenesisTest(genesisBytes, t)
	assetID := genesisTx.ID()
	addr := keys[0].PublicKey().Address()

	addrStr, err := vm.FormatLocalAddress(addr)
	if err != nil {
		t.Fatal(err)
	}
	args := &SendArgs{
		UserPass: api.UserPass{
			Username: username,
			Password: password,
		},
		Amount:  500,
		AssetID: assetID.String(),
		To:      addrStr,
	}
	reply := &api.JsonTxID{}
	vm.timer.Cancel()
	if err := s.Send(nil, args, reply); err != nil {
		t.Fatalf("Failed to send transaction: %s", err)
	}

	pendingTxs := vm.txs
	if len(pendingTxs) != 1 {
		t.Fatalf("Expected to find 1 pending tx after send, but found %d", len(pendingTxs))
	}

	if !reply.TxID.Equals(pendingTxs[0].ID()) {
		t.Fatal("Transaction ID returned by Send does not match the transaction found in vm's pending transactions")
	}
}

func TestCreateAndListAddresses(t *testing.T) {
	_, vm, s, _ := setup(t)
	defer func() {
		vm.Shutdown()
		vm.ctx.Lock.Unlock()
	}()

	createArgs := &api.UserPass{
		Username: username,
		Password: password,
	}
	createReply := &api.JsonAddress{}

	if err := s.CreateAddress(nil, createArgs, createReply); err != nil {
		t.Fatalf("Failed to create address: %s", err)
	}

	newAddr := createReply.Address

	listArgs := &api.UserPass{
		Username: username,
		Password: password,
	}
	listReply := &api.JsonAddresses{}

	if err := s.ListAddresses(nil, listArgs, listReply); err != nil {
		t.Fatalf("Failed to list addresses: %s", err)
	}

	for _, addr := range listReply.Addresses {
		if addr == newAddr {
			return
		}
	}
	t.Fatalf("Failed to find newly created address among %d addresses", len(listReply.Addresses))
}

func TestImportAVAX(t *testing.T) {
	genesisBytes, vm, s, m := setupWithKeys(t)
	defer func() {
		vm.Shutdown()
		vm.ctx.Lock.Unlock()
	}()
	genesisTx := GetFirstTxFromGenesisTest(genesisBytes, t)
	assetID := genesisTx.ID()
	addr0 := keys[0].PublicKey().Address()

	// Must set AVAX assetID to be the correct asset since only AVAX can be imported
	utxo := &avax.UTXO{
		UTXOID: avax.UTXOID{TxID: ids.Empty},
		Asset:  avax.Asset{ID: assetID},
		Out: &secp256k1fx.TransferOutput{
			Amt: 7,
			OutputOwners: secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{addr0},
			},
		},
	}
	utxoBytes, err := vm.codec.Marshal(utxo)
	if err != nil {
		t.Fatal(err)
	}

	peerSharedMemory := m.NewSharedMemory(platformChainID)
	if err := peerSharedMemory.Put(vm.ctx.ChainID, []*atomic.Element{{
		Key:   utxo.InputID().Bytes(),
		Value: utxoBytes,
		Traits: [][]byte{
			addr0.Bytes(),
		},
	}}); err != nil {
		t.Fatal(err)
	}

	addrStr, err := vm.FormatLocalAddress(keys[0].PublicKey().Address())
	if err != nil {
		t.Fatal(err)
	}
	args := &ImportAVAXArgs{
		UserPass: api.UserPass{
			Username: username,
			Password: password,
		},
		SourceChain: "P",
		To:          addrStr,
	}
	reply := &api.JsonTxID{}
	if err := s.ImportAVAX(nil, args, reply); err != nil {
		t.Fatalf("Failed to import AVAX due to %s", err)
	}
}
