// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/gecko/api"
	"github.com/ava-labs/gecko/api/keystore"
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow/choices"
	"github.com/ava-labs/gecko/utils/constants"
	"github.com/ava-labs/gecko/utils/crypto"
	"github.com/ava-labs/gecko/utils/formatting"
	"github.com/ava-labs/gecko/vms/components/ava"
	"github.com/ava-labs/gecko/vms/secp256k1fx"
)

func setup(t *testing.T) ([]byte, *VM, *Service) {
	genesisBytes, _, vm := GenesisVM(t)
	keystore := keystore.CreateTestKeystore()
	keystore.AddUser(username, password)
	vm.ctx.Keystore = keystore.NewBlockchainKeyStore(chainID)
	s := &Service{vm: vm}
	return genesisBytes, vm, s
}

func setupWithKeys(t *testing.T) ([]byte, *VM, *Service) {
	genesisBytes, vm, s := setup(t)

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
	return genesisBytes, vm, s
}

func TestServiceIssueTx(t *testing.T) {
	genesisBytes, vm, s := setup(t)
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
	genesisBytes, vm, s := setup(t)
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
	genesisBytes, vm, s := setup(t)
	defer func() {
		vm.Shutdown()
		vm.ctx.Lock.Unlock()
	}()

	genesisTx := GetFirstTxFromGenesisTest(genesisBytes, t)
	assetID := genesisTx.ID()
	addr := keys[0].PublicKey().Address().Bytes()
	addrstr, err := vm.FormatAddress(addr)
	if err != nil {
		t.Fatal(err)
	}

	balanceArgs := &GetBalanceArgs{
		Address: addrstr,
		AssetID: assetID.String(),
	}
	balanceReply := &GetBalanceReply{}
	err = s.GetBalance(nil, balanceArgs, balanceReply)
	assert.NoError(t, err)
	assert.Equal(t, uint64(balanceReply.Balance), uint64(300000))

	assert.Len(t, balanceReply.UTXOIDs, 4, "should have only returned four utxoIDs")
}

func TestServiceGetAllBalances(t *testing.T) {
	genesisBytes, vm, s := setup(t)
	defer func() {
		vm.Shutdown()
		vm.ctx.Lock.Unlock()
	}()

	genesisTx := GetFirstTxFromGenesisTest(genesisBytes, t)
	assetID := genesisTx.ID()
	addr := keys[0].PublicKey().Address().Bytes()
	addrstr, err := vm.FormatAddress(addr)
	if err != nil {
		t.Fatal(err)
	}

	balanceArgs := &api.JsonAddress{
		Address: addrstr,
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
	genesisBytes, vm, s := setup(t)
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
	_, vm, s := setup(t)
	defer func() {
		vm.Shutdown()
		vm.ctx.Lock.Unlock()
	}()

	reply := FormattedTx{}
	err := s.GetTx(nil, &api.JsonTxID{}, &reply)
	assert.Error(t, err, "Nil TxID should have returned an error")
}

func TestServiceGetUnknownTx(t *testing.T) {
	_, vm, s := setup(t)
	defer func() {
		vm.Shutdown()
		vm.ctx.Lock.Unlock()
	}()

	reply := FormattedTx{}
	err := s.GetTx(nil, &api.JsonTxID{TxID: ids.Empty}, &reply)
	assert.Error(t, err, "Unknown TxID should have returned an error")
}

func TestServiceGetUTXOsInvalidAddress(t *testing.T) {
	_, vm, s := setup(t)
	defer func() {
		vm.Shutdown()
		vm.ctx.Lock.Unlock()
	}()

	addr0 := keys[0].PublicKey().Address()
	addr0fullstr, err := vm.FormatAddress(addr0.Bytes())
	addr0str := strings.SplitN(addr0fullstr, addressSep, 2)[1]
	if err != nil {
		t.Error(err)
	}
	tests := []struct {
		label string
		args  *api.JsonAddresses
	}{
		{"[", &api.JsonAddresses{Addresses: []string{""}}},
		{"[-]", &api.JsonAddresses{Addresses: []string{"-"}}},
		{"[foo]", &api.JsonAddresses{Addresses: []string{"foo"}}},
		{"[foo-bar]", &api.JsonAddresses{Addresses: []string{"foo-bar"}}},
		{"[<ChainID>]", &api.JsonAddresses{Addresses: []string{vm.ctx.ChainID.String()}}},
		{"[<ChainID>-]", &api.JsonAddresses{Addresses: []string{fmt.Sprintf("%s-", vm.ctx.ChainID.String())}}},
		{"[<Unknown ID>-<addr0>]", &api.JsonAddresses{Addresses: []string{fmt.Sprintf("%s-%s", ids.NewID([32]byte{42}).String(), addr0str)}}},
	}
	for _, tt := range tests {
		t.Run(tt.label, func(t *testing.T) {
			utxosReply := &FormattedUTXOs{}
			if err := s.GetUTXOs(nil, tt.args, utxosReply); err == nil {
				t.Error(err)
			}
		})
	}
}

func TestServiceGetUTXOs(t *testing.T) {
	_, vm, s := setup(t)
	defer func() {
		vm.Shutdown()
		vm.ctx.Lock.Unlock()
	}()

	addr0 := keys[0].PublicKey().Address()
	addr0fullstr, err := vm.FormatAddress(addr0.Bytes())
	if err != nil {
		t.Error(err)
	}
	addr0str := strings.SplitN(addr0fullstr, addressSep, 2)[1]

	newAddrFullStr, err := vm.FormatAddress(ids.NewID([32]byte{20}).Bytes())
	if err != nil {
		t.Error(err)
	}
	newAddrStr := strings.SplitN(newAddrFullStr, addressSep, 2)[1]

	tests := []struct {
		label string
		args  *api.JsonAddresses
		count int
	}{
		{
			"Empty",
			&api.JsonAddresses{},
			0,
		},
		{
			"[<ChainID>-<unrelated address>]",
			&api.JsonAddresses{
				Addresses: []string{
					fmt.Sprintf("%s-%s", vm.ctx.ChainID.String(), newAddrStr),
				},
			},
			0,
		},
		{
			"[<ChainID>-<addr0>]",
			&api.JsonAddresses{
				Addresses: []string{
					fmt.Sprintf("%s-%s", vm.ctx.ChainID.String(), addr0str),
				},
			},
			7,
		},
		{
			"[<ChainID>-<addr0>,<ChainID>-<addr0>]",
			&api.JsonAddresses{
				Addresses: []string{
					fmt.Sprintf("%s-%s", vm.ctx.ChainID.String(), addr0str),
					fmt.Sprintf("%s-%s", vm.ctx.ChainID.String(), addr0str),
				},
			},
			7,
		},
	}
	for _, tt := range tests {
		t.Run(tt.label, func(t *testing.T) {
			utxosReply := &FormattedUTXOs{}
			if err := s.GetUTXOs(nil, tt.args, utxosReply); err != nil {
				t.Error(err)
			} else if tt.count != len(utxosReply.UTXOs) {
				t.Errorf("Expected %d utxos, got %#v", tt.count, len(utxosReply.UTXOs))
			}
		})
	}
}

func TestServiceGetAtomicUTXOsInvalidAddress(t *testing.T) {
	_, vm, s := setup(t)
	defer func() {
		vm.Shutdown()
		vm.ctx.Lock.Unlock()
	}()

	addr0 := keys[0].PublicKey().Address()
	addr0fullstr, err := vm.FormatAddress(addr0.Bytes())
	addr0str := strings.SplitN(addr0fullstr, addressSep, 2)[1]
	if err != nil {
		t.Error(err)
	}
	tests := []struct {
		label string
		args  *api.JsonAddresses
	}{
		{"[", &api.JsonAddresses{Addresses: []string{""}}},
		{"[-]", &api.JsonAddresses{Addresses: []string{"-"}}},
		{"[foo]", &api.JsonAddresses{Addresses: []string{"foo"}}},
		{"[foo-bar]", &api.JsonAddresses{Addresses: []string{"foo-bar"}}},
		{"[<ChainID>]", &api.JsonAddresses{Addresses: []string{vm.ctx.ChainID.String()}}},
		{"[<ChainID>-]", &api.JsonAddresses{Addresses: []string{fmt.Sprintf("%s-", vm.ctx.ChainID.String())}}},
		{"[<Unknown ID>-<addr0>]", &api.JsonAddresses{Addresses: []string{fmt.Sprintf("%s-%s", ids.NewID([32]byte{42}).String(), addr0str)}}},
	}
	for _, tt := range tests {
		t.Run(tt.label, func(t *testing.T) {
			utxosReply := &FormattedUTXOs{}
			if err := s.GetAtomicUTXOs(nil, tt.args, utxosReply); err == nil {
				t.Error(err)
			}
		})
	}
}

func TestServiceGetAtomicUTXOs(t *testing.T) {
	_, vm, s := setup(t)
	defer func() {
		vm.Shutdown()
		vm.ctx.Lock.Unlock()
	}()

	addr0 := keys[0].PublicKey().Address()
	addr0fullstr, err := vm.FormatAddress(addr0.Bytes())
	if err != nil {
		t.Error(err)
	}
	addr0str := strings.SplitN(addr0fullstr, addressSep, 2)[1]

	newAddrFullStr, err := vm.FormatAddress(ids.NewID([32]byte{20}).Bytes())
	if err != nil {
		t.Error(err)
	}
	newAddrStr := strings.SplitN(newAddrFullStr, addressSep, 2)[1]

	platformID := ids.Empty.Prefix(0)
	smDB := vm.ctx.SharedMemory.GetDatabase(platformID)

	utxo := &ava.UTXO{
		UTXOID: ava.UTXOID{TxID: ids.Empty},
		Asset:  ava.Asset{ID: ids.Empty},
		Out: &secp256k1fx.TransferOutput{
			Amt: 7,
			OutputOwners: secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{addr0},
			},
		},
	}

	state := ava.NewPrefixedState(smDB, vm.codec)
	if err := state.FundPlatformUTXO(utxo); err != nil {
		t.Fatal(err)
	}
	vm.ctx.SharedMemory.ReleaseDatabase(platformID)

	tests := []struct {
		label string
		args  *api.JsonAddresses
		count int
	}{
		{
			"Empty",
			&api.JsonAddresses{},
			0,
		},
		{
			"[<ChainID>-<unrelated address>]",
			&api.JsonAddresses{
				Addresses: []string{
					// TODO: Should GetAtomicUTXOs() raise an error for this? The address portion is
					//		 longer than addr0.String()
					fmt.Sprintf("%s-%s", vm.ctx.ChainID.String(), newAddrStr),
				},
			},
			0,
		},
		{
			"[<ChainID>-<addr0>]",
			&api.JsonAddresses{
				Addresses: []string{
					fmt.Sprintf("%s-%s", vm.ctx.ChainID.String(), addr0str),
				},
			},
			1,
		},
		{
			"[<ChainID>-<addr0>,<ChainID>-<addr0>]",
			&api.JsonAddresses{
				Addresses: []string{
					fmt.Sprintf("%s-%s", vm.ctx.ChainID.String(), addr0str),
					fmt.Sprintf("%s-%s", vm.ctx.ChainID.String(), addr0str),
				},
			},
			1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.label, func(t *testing.T) {
			utxosReply := &FormattedUTXOs{}
			if err := s.GetAtomicUTXOs(nil, tt.args, utxosReply); err != nil {
				t.Error(err)
			} else if tt.count != len(utxosReply.UTXOs) {
				t.Errorf("Expected %d utxos, got %#v", tt.count, len(utxosReply.UTXOs))
			}
		})
	}
}

func TestGetAssetDescription(t *testing.T) {
	genesisBytes, vm, s := setup(t)
	defer func() {
		vm.Shutdown()
		vm.ctx.Lock.Unlock()
	}()

	genesisTx := GetFirstTxFromGenesisTest(genesisBytes, t)

	avaAssetID := genesisTx.ID()

	reply := GetAssetDescriptionReply{}
	err := s.GetAssetDescription(nil, &GetAssetDescriptionArgs{
		AssetID: avaAssetID.String(),
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
	genesisBytes, vm, s := setup(t)
	defer func() {
		vm.Shutdown()
		vm.ctx.Lock.Unlock()
	}()

	genesisTx := GetFirstTxFromGenesisTest(genesisBytes, t)

	avaAssetID := genesisTx.ID()

	reply := GetBalanceReply{}
	addrstr, err := vm.FormatAddress(keys[0].PublicKey().Address().Bytes())
	if err != nil {
		t.Fatal(err)
	}
	err = s.GetBalance(nil, &GetBalanceArgs{
		Address: addrstr,
		AssetID: avaAssetID.String(),
	}, &reply)
	if err != nil {
		t.Fatal(err)
	}

	if reply.Balance != 300000 {
		t.Fatalf("Wrong balance returned from GetBalance %d", reply.Balance)
	}
}

func TestCreateFixedCapAsset(t *testing.T) {
	_, vm, s := setup(t)
	defer func() {
		vm.Shutdown()
		vm.ctx.Lock.Unlock()
	}()

	reply := FormattedAssetID{}
	addrstr, err := vm.FormatAddress(keys[0].PublicKey().Address().Bytes())
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
			Address: addrstr,
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
	_, vm, s := setupWithKeys(t)
	defer func() {
		vm.Shutdown()
		vm.ctx.Lock.Unlock()
	}()

	reply := FormattedAssetID{}
	addrstr, err := vm.FormatAddress(keys[0].PublicKey().Address().Bytes())
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
					addrstr,
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
	addrstr, err = vm.FormatAddress(keys[0].PublicKey().Address().Bytes())
	if err != nil {
		t.Fatal(err)
	}
	mintArgs := &MintArgs{
		UserPass: api.UserPass{
			Username: username,
			Password: password,
		},
		Amount:  200,
		AssetID: createdAssetID,
		To:      addrstr,
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
	addrstr, err = vm.FormatAddress(keys[0].PublicKey().Address().Bytes())
	if err != nil {
		t.Fatal(err)
	}
	sendArgs := &SendArgs{
		UserPass: api.UserPass{
			Username: username,
			Password: password,
		},
		Amount:  200,
		AssetID: createdAssetID,
		To:      addrstr,
	}
	sendReply := &api.JsonTxID{}
	if err := s.Send(nil, sendArgs, sendReply); err != nil {
		t.Fatalf("Failed to send newly minted variable cap asset due to: %s", err)
	}
}

func TestNFTWorkflow(t *testing.T) {
	_, vm, s := setupWithKeys(t)
	defer func() {
		vm.Shutdown()
		vm.ctx.Lock.Unlock()
	}()

	// Test minting of the created variable cap asset
	addrstr, err := vm.FormatAddress(keys[0].PublicKey().Address().Bytes())
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
			Owners{
				Threshold: 1,
				Minters: []string{
					addrstr,
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

	addrstr, err = vm.FormatAddress(keys[0].PublicKey().Address().Bytes())
	if err != nil {
		t.Fatal(err)
	}
	mintArgs := &MintNFTArgs{
		UserPass: api.UserPass{
			Username: username,
			Password: password,
		},
		AssetID: assetID.String(),
		Payload: formatting.CB58{Bytes: []byte{1, 2, 3, 4, 5}},
		To:      addrstr,
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

	addrstr, err = vm.FormatAddress(keys[2].PublicKey().Address().Bytes())
	if err != nil {
		t.Fatal(err)
	}
	sendArgs := &SendNFTArgs{
		UserPass: api.UserPass{
			Username: username,
			Password: password,
		},
		AssetID: assetID.String(),
		GroupID: 0,
		To:      addrstr,
	}
	sendReply := &api.JsonTxID{}
	if err := s.SendNFT(nil, sendArgs, sendReply); err != nil {
		t.Fatalf("Failed to send NFT due to: %s", err)
	}
}

func TestImportExportKey(t *testing.T) {
	_, vm, s := setup(t)
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

	addrstr, err := vm.FormatAddress(sk.PublicKey().Address().Bytes())
	if err != nil {
		t.Fatal(err)
	}
	exportArgs := &ExportKeyArgs{
		UserPass: api.UserPass{
			Username: username,
			Password: password,
		},
		Address: addrstr,
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
	_, vm, s := setup(t)
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

	expectedAddress, err := vm.FormatAddress(sk.PublicKey().Address().Bytes())
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
	genesisBytes, vm, s := setupWithKeys(t)
	defer func() {
		vm.Shutdown()
		vm.ctx.Lock.Unlock()
	}()

	genesisTx := GetFirstTxFromGenesisTest(genesisBytes, t)
	assetID := genesisTx.ID()
	addr := keys[0].PublicKey().Address()

	addrstr, err := vm.FormatAddress(addr.Bytes())
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
		To:      addrstr,
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
	_, vm, s := setup(t)
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

func TestImportAVA(t *testing.T) {
	genesisBytes, vm, s := setupWithKeys(t)
	defer func() {
		vm.Shutdown()
		vm.ctx.Lock.Unlock()
	}()
	genesisTx := GetFirstTxFromGenesisTest(genesisBytes, t)
	assetID := genesisTx.ID()

	addr0 := keys[0].PublicKey().Address()
	smDB := vm.ctx.SharedMemory.GetDatabase(vm.platform)

	// Must set ava assetID to be the correct asset since only AVA can be imported
	vm.ava = assetID
	utxo := &ava.UTXO{
		UTXOID: ava.UTXOID{TxID: ids.Empty},
		Asset:  ava.Asset{ID: assetID},
		Out: &secp256k1fx.TransferOutput{
			Amt: 7,
			OutputOwners: secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{addr0},
			},
		},
	}

	state := ava.NewPrefixedState(smDB, vm.codec)
	if err := state.FundPlatformUTXO(utxo); err != nil {
		t.Fatal(err)
	}
	vm.ctx.SharedMemory.ReleaseDatabase(vm.platform)
	addrstr, err := vm.FormatAddress(keys[0].PublicKey().Address().Bytes())
	if err != nil {
		t.Fatal(err)
	}
	importArgs := &ImportAVAArgs{
		UserPass: api.UserPass{
			Username: username,
			Password: password,
		},
		To: addrstr,
	}
	importReply := &api.JsonTxID{}
	if err := s.ImportAVA(nil, importArgs, importReply); err != nil {
		t.Fatalf("Failed to import AVA due to %s", err)
	}
}
