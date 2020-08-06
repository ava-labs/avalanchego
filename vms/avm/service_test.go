// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/gecko/api/keystore"
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow/choices"
	"github.com/ava-labs/gecko/utils/constants"
	"github.com/ava-labs/gecko/utils/crypto"
	"github.com/ava-labs/gecko/utils/formatting"
	"github.com/ava-labs/gecko/utils/hashing"
	"github.com/ava-labs/gecko/utils/json"
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

	txArgs := &IssueTxArgs{}
	txReply := &IssueTxReply{}
	err := s.IssueTx(nil, txArgs, txReply)
	if err == nil {
		t.Fatal("Expected empty transaction to return an error")
	}

	tx := NewTx(t, genesisBytes, vm)
	txArgs.Tx = formatting.CB58{Bytes: tx.Bytes()}
	txReply = &IssueTxReply{}
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

	statusArgs := &GetTxStatusArgs{}
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

	txArgs := &IssueTxArgs{Tx: formatting.CB58{Bytes: tx.Bytes()}}
	txReply := &IssueTxReply{}
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
	addr := keys[0].PublicKey().Address()

	balanceArgs := &GetBalanceArgs{
		Address: fmt.Sprintf("%s-%s", vm.ctx.ChainID, addr),
		AssetID: assetID.String(),
	}
	balanceReply := &GetBalanceReply{}
	err := s.GetBalance(nil, balanceArgs, balanceReply)
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
	addr := keys[0].PublicKey().Address()

	balanceArgs := &GetAllBalancesArgs{
		Address: fmt.Sprintf("%s-%s", vm.ctx.ChainID, addr),
	}
	balanceReply := &GetAllBalancesReply{}
	err := s.GetAllBalances(nil, balanceArgs, balanceReply)
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

	reply := GetTxReply{}
	err := s.GetTx(nil, &GetTxArgs{
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

	reply := GetTxReply{}
	err := s.GetTx(nil, &GetTxArgs{}, &reply)
	assert.Error(t, err, "Nil TxID should have returned an error")
}

func TestServiceGetUnknownTx(t *testing.T) {
	_, vm, s := setup(t)
	defer func() {
		vm.Shutdown()
		vm.ctx.Lock.Unlock()
	}()

	reply := GetTxReply{}
	err := s.GetTx(nil, &GetTxArgs{TxID: ids.Empty}, &reply)
	assert.Error(t, err, "Unknown TxID should have returned an error")
}

func TestServiceGetUTXOsInvalidAddress(t *testing.T) {
	_, vm, s := setup(t)
	defer func() {
		vm.Shutdown()
		vm.ctx.Lock.Unlock()
	}()

	addr0 := keys[0].PublicKey().Address()
	tests := []struct {
		label string
		args  *GetUTXOsArgs
	}{
		{"[]", &GetUTXOsArgs{"", []string{""}, 0, Index{}}},
		{"[-]", &GetUTXOsArgs{"", []string{"-"}, 0, Index{}}},
		{"[foo]", &GetUTXOsArgs{"", []string{"foo"}, 0, Index{}}},
		{"[foo-bar]", &GetUTXOsArgs{"", []string{"foo-bar"}, 0, Index{}}},
		{"[<ChainID>]", &GetUTXOsArgs{"", []string{vm.ctx.ChainID.String()}, 0, Index{}}},
		{"[<ChainID>-]", &GetUTXOsArgs{"", []string{fmt.Sprintf("%s-", vm.ctx.ChainID.String())}, 0, Index{}}},
		{"[<Unknown ID>-<addr0>]", &GetUTXOsArgs{"", []string{fmt.Sprintf("%s-%s", ids.NewID([32]byte{42}).String(), addr0.String())}, 0, Index{}}},
	}
	for _, tt := range tests {
		t.Run(tt.label, func(t *testing.T) {
			utxosReply := &GetUTXOsReply{}
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

	addr := ids.GenerateTestShortID()

	numUtxos := 10
	// Put a bunch of UTXOs
	for i := 0; i < numUtxos; i++ {
		if err := vm.state.FundUTXO(&ava.UTXO{
			UTXOID: ava.UTXOID{
				TxID: ids.GenerateTestID(),
			},
			Asset: ava.Asset{ID: vm.ava},
			Out: &secp256k1fx.TransferOutput{
				Amt: 1,
				OutputOwners: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{addr},
				},
			},
		}); err != nil {
			t.Fatal(err)
		}
	}

	tests := []struct {
		label         string
		args          *GetUTXOsArgs
		expectedCount int
		shouldErr     bool
	}{
		{
			"Empty",
			&GetUTXOsArgs{},
			0,
			false,
		}, {
			"[<ChainID>-<invalid address>]",
			&GetUTXOsArgs{
				ChainID: "",
				Addresses: []string{
					fmt.Sprintf("%s-%s", vm.ctx.ChainID.String(), ids.NewID([32]byte{42}).String()),
				},
				Limit:      0,
				StartIndex: Index{},
			},
			0,
			true,
		}, {
			"[<ChainID>-<addr>]",
			&GetUTXOsArgs{
				ChainID: "",
				Addresses: []string{
					fmt.Sprintf("%s-%s", vm.ctx.ChainID.String(), addr.String()),
				},
				Limit:      0,
				StartIndex: Index{},
			},
			numUtxos,
			false,
		},
		{
			"[<ChainID>-<addr>] limit to 1 UTXO",
			&GetUTXOsArgs{
				ChainID: "",
				Addresses: []string{
					fmt.Sprintf("%s-%s", vm.ctx.ChainID.String(), addr.String()),
				},
				Limit:      1,
				StartIndex: Index{},
			},
			1,
			false,
		},
		{
			"[<ChainID>-<addr>] limit greater than number of UTXOs",
			&GetUTXOsArgs{
				ChainID: "",
				Addresses: []string{
					fmt.Sprintf("%s-%s", vm.ctx.ChainID.String(), addr.String()),
				},
				Limit:      100000,
				StartIndex: Index{},
			},
			numUtxos,
			false,
		},
		{
			"[<ChainID>-<addr>,<ChainID>-<addr>]",
			&GetUTXOsArgs{
				ChainID: "",
				Addresses: []string{
					fmt.Sprintf("%s-%s", vm.ctx.ChainID.String(), addr.String()),
					fmt.Sprintf("%s-%s", vm.ctx.ChainID.String(), addr.String()),
				},
				Limit:      0,
				StartIndex: Index{},
			},
			numUtxos,
			false,
		}, {
			"[<ChainID>-<addr>,<ChainID>-<addr>], limit to 1 UTXO",
			&GetUTXOsArgs{
				ChainID: "",
				Addresses: []string{
					fmt.Sprintf("%s-%s", vm.ctx.ChainID.String(), addr.String()),
					fmt.Sprintf("%s-%s", vm.ctx.ChainID.String(), addr.String()),
				},
				Limit:      1,
				StartIndex: Index{},
			},
			1,
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.label, func(t *testing.T) {
			utxosReply := &GetUTXOsReply{}
			if err := s.GetUTXOs(nil, tt.args, utxosReply); err != nil && !tt.shouldErr {
				t.Error(err)
			} else if err == nil && tt.shouldErr {
				t.Error("should have errored")
			} else if tt.expectedCount != len(utxosReply.UTXOs) {
				t.Errorf("Expected %d utxos, got %#v", tt.expectedCount, len(utxosReply.UTXOs))
			} else if tt.expectedCount != int(utxosReply.NumFetched) {
				t.Errorf("numFetced is %d but got %d utxos", utxosReply.NumFetched, tt.expectedCount)
			}
		})
	}

	// Test that start index and stop index work
	// (Assumes numUtxos > 5)
	reply := &GetUTXOsReply{}
	args := &GetUTXOsArgs{
		ChainID:    "",
		Addresses:  []string{fmt.Sprintf("%s-%s", vm.ctx.ChainID.String(), addr.String())},
		Limit:      5,
		StartIndex: Index{},
	}
	utxos := ids.Set{}
	if err := s.GetUTXOs(nil, args, reply); err != nil {
		t.Fatal(err)
	}
	for _, utxo := range reply.UTXOs { // Remember these UTXOs
		utxos.Add(ids.NewID(hashing.ComputeHash256Array(utxo.Bytes)))
	}
	args = &GetUTXOsArgs{
		"",
		[]string{fmt.Sprintf("%s-%s", vm.ctx.ChainID.String(), addr.String())},
		json.Uint32(numUtxos - 5),
		reply.EndIndex,
	}
	if err := s.GetUTXOs(nil, args, reply); err != nil {
		t.Fatal(err)
	} else if len(reply.UTXOs) != numUtxos-5 {
		t.Fatalf("got %d utxos but should have %d", len(reply.UTXOs), numUtxos-5)
	}
	for _, utxo := range reply.UTXOs { // Remember these UTXOs
		utxos.Add(ids.NewID(hashing.ComputeHash256Array(utxo.Bytes)))
	}
	// Should have gotten all the UTXOs now. None should have been repeats
	// so length of this set should be numUtxos
	if utxos.Len() != numUtxos {
		t.Fatalf("got %d utxos but should have %d", utxos.Len(), numUtxos)
	}
}

func TestServiceGetUTXOsFromPlatform(t *testing.T) {
	_, vm, s := setup(t)
	defer func() {
		vm.Shutdown()
		vm.ctx.Lock.Unlock()
	}()

	addr1 := ids.GenerateTestShortID()
	addr2 := ids.GenerateTestShortID()

	smDB := vm.ctx.SharedMemory.GetDatabase(platformChainID)
	state := ava.NewPrefixedState(smDB, vm.codec)
	numUtxos := 10
	// Put a bunch of UTXOs that reference both addr1 and addr2
	for i := 0; i < numUtxos; i++ {
		if err := state.FundPlatformUTXO(&ava.UTXO{
			UTXOID: ava.UTXOID{
				TxID: ids.GenerateTestID(),
			},
			Asset: ava.Asset{ID: vm.ava},
			Out: &secp256k1fx.TransferOutput{
				Amt: 1,
				OutputOwners: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{addr1, addr2},
				},
			},
		}); err != nil {
			t.Fatal(err)
		}
	}
	vm.ctx.SharedMemory.ReleaseDatabase(platformChainID)

	tests := []struct {
		label         string
		args          *GetUTXOsArgs
		expectedCount int
		shouldErr     bool
	}{
		{
			"Empty",
			&GetUTXOsArgs{},
			0,
			false,
		},
		{
			"[<ChainID>-<invalid address>]",
			&GetUTXOsArgs{
				ChainID: platformChainID.String(),
				Addresses: []string{
					fmt.Sprintf("%s-%s", vm.ctx.ChainID.String(), ids.NewID([32]byte{42}).String()),
				},
				Limit:      0,
				StartIndex: Index{},
			},
			0,
			true,
		},
		{
			"[<ChainID>-<addr1>]",
			&GetUTXOsArgs{
				ChainID: platformChainID.String(),
				Addresses: []string{
					fmt.Sprintf("%s-%s", vm.ctx.ChainID.String(), addr1.String()),
				},
				Limit:      0,
				StartIndex: Index{},
			},
			numUtxos,
			false,
		},
		{
			"[<ChainID>-<add1r>,<ChainID>-<addr2>]",
			&GetUTXOsArgs{
				ChainID: platformChainID.String(),
				Addresses: []string{
					fmt.Sprintf("%s-%s", vm.ctx.ChainID.String(), addr1.String()),
					fmt.Sprintf("%s-%s", vm.ctx.ChainID.String(), addr2.String()),
				},
				Limit:      0,
				StartIndex: Index{},
			},
			numUtxos,
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.label, func(t *testing.T) {
			utxosReply := &GetUTXOsReply{}
			if err := s.GetUTXOs(nil, tt.args, utxosReply); err != nil && !tt.shouldErr {
				t.Error(err)
			} else if err == nil && tt.shouldErr {
				t.Error("should have errored")
			} else if tt.expectedCount != len(utxosReply.UTXOs) {
				t.Errorf("Expected %d utxos, got %#v", tt.expectedCount, len(utxosReply.UTXOs))
			} else if tt.expectedCount != int(utxosReply.NumFetched) {
				t.Errorf("numFetced is %d but got %d utxos", utxosReply.NumFetched, tt.expectedCount)
			}
		})
	}

	// Test that start index and stop index work
	// (Assumes numUtxos > 5)
	reply := &GetUTXOsReply{}
	args := &GetUTXOsArgs{
		ChainID:    platformChainID.String(),
		Addresses:  []string{fmt.Sprintf("%s-%s", vm.ctx.ChainID.String(), addr1.String())},
		Limit:      5,
		StartIndex: Index{},
	}
	utxos := ids.Set{}
	if err := s.GetUTXOs(nil, args, reply); err != nil {
		t.Fatal(err)
	}
	for _, utxo := range reply.UTXOs { // Remember these UTXOs
		utxos.Add(ids.NewID(hashing.ComputeHash256Array(utxo.Bytes)))
	}
	args = &GetUTXOsArgs{
		platformChainID.String(),
		[]string{fmt.Sprintf("%s-%s", vm.ctx.ChainID.String(), addr1.String())},
		json.Uint32(numUtxos - 5),
		reply.EndIndex,
	}
	if err := s.GetUTXOs(nil, args, reply); err != nil {
		t.Fatal(err)
	} else if len(reply.UTXOs) != numUtxos-5 {
		t.Fatalf("got %d utxos but should have %d", len(reply.UTXOs), numUtxos-5)
	}
	for _, utxo := range reply.UTXOs { // Remember these UTXOs
		utxos.Add(ids.NewID(hashing.ComputeHash256Array(utxo.Bytes)))
	}
	// Should have gotten all the UTXOs now. None should have been repeats
	// so length of this set should be numUtxos
	if utxos.Len() != numUtxos {
		t.Fatalf("got %d utxos but should have %d", utxos.Len(), numUtxos)
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
	err := s.GetBalance(nil, &GetBalanceArgs{
		Address: vm.Format(keys[0].PublicKey().Address().Bytes()),
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

	reply := CreateFixedCapAssetReply{}
	err := s.CreateFixedCapAsset(nil, &CreateFixedCapAssetArgs{
		Username:     username,
		Password:     password,
		Name:         "testAsset",
		Symbol:       "TEST",
		Denomination: 1,
		InitialHolders: []*Holder{{
			Amount:  123456789,
			Address: vm.Format(keys[0].PublicKey().Address().Bytes()),
		}},
	}, &reply)
	if err != nil {
		t.Fatal(err)
	}

	if reply.AssetID.String() != "2CJbAPBPwt9nFd28MbKKJZkincdmvDmP7UYbPT4VP1LJ46Yyip" {
		t.Fatalf("Wrong assetID returned from CreateFixedCapAsset %s", reply.AssetID)
	}
}

func TestCreateVariableCapAsset(t *testing.T) {
	_, vm, s := setupWithKeys(t)
	defer func() {
		vm.Shutdown()
		vm.ctx.Lock.Unlock()
	}()

	reply := CreateVariableCapAssetReply{}
	err := s.CreateVariableCapAsset(nil, &CreateVariableCapAssetArgs{
		Username: username,
		Password: password,
		Name:     "test asset",
		Symbol:   "TEST",
		MinterSets: []Owners{
			{
				Threshold: 1,
				Minters: []string{
					vm.Format(keys[0].PublicKey().Address().Bytes()),
				},
			},
		},
	}, &reply)
	if err != nil {
		t.Fatal(err)
	}

	createdAssetID := reply.AssetID.String()

	if createdAssetID != "23FV5zQpuG9EZBh7BXKj9wqPAMe7tY9T4jEWpobbMQHLLUf88o" {
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
		Username: username,
		Password: password,
		Amount:   200,
		AssetID:  createdAssetID,
		To:       vm.Format(keys[0].PublicKey().Address().Bytes()),
	}
	mintReply := &MintReply{}
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
		Username: username,
		Password: password,
		Amount:   200,
		AssetID:  createdAssetID,
		To:       vm.Format(keys[0].PublicKey().Address().Bytes()),
	}
	sendReply := &SendReply{}
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
	createArgs := &CreateNFTAssetArgs{
		Username: username,
		Password: password,
		Name:     "BIG COIN",
		Symbol:   "COIN",
		MinterSets: []Owners{
			Owners{
				Threshold: 1,
				Minters: []string{
					vm.Format(keys[0].PublicKey().Address().Bytes()),
				},
			},
		},
	}
	createReply := &CreateNFTAssetReply{}
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
		Username: username,
		Password: password,
		AssetID:  assetID.String(),
		Payload:  formatting.CB58{Bytes: []byte{1, 2, 3, 4, 5}},
		To:       vm.Format(keys[0].PublicKey().Address().Bytes()),
	}
	mintReply := &MintNFTReply{}

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
		Username: username,
		Password: password,
		AssetID:  assetID.String(),
		GroupID:  0,
		To:       vm.Format(keys[2].PublicKey().Address().Bytes()),
	}
	sendReply := &SendNFTReply{}
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
		Username:   username,
		Password:   password,
		PrivateKey: constants.SecretKeyPrefix + formatting.CB58{Bytes: sk.Bytes()}.String(),
	}
	importReply := &ImportKeyReply{}
	if err = s.ImportKey(nil, importArgs, importReply); err != nil {
		t.Fatal(err)
	}

	exportArgs := &ExportKeyArgs{
		Username: username,
		Password: password,
		Address:  vm.Format(sk.PublicKey().Address().Bytes()),
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
		Username:   username,
		Password:   password,
		PrivateKey: constants.SecretKeyPrefix + formatting.CB58{Bytes: sk.Bytes()}.String(),
	}
	reply := ImportKeyReply{}
	if err = s.ImportKey(nil, &args, &reply); err != nil {
		t.Fatal(err)
	}

	expectedAddress := vm.Format(sk.PublicKey().Address().Bytes())

	if reply.Address != expectedAddress {
		t.Fatalf("Reply address: %s did not match expected address: %s", reply.Address, expectedAddress)
	}

	reply2 := ImportKeyReply{}
	if err = s.ImportKey(nil, &args, &reply2); err != nil {
		t.Fatal(err)
	}

	if reply2.Address != expectedAddress {
		t.Fatalf("Reply address: %s did not match expected address: %s", reply2.Address, expectedAddress)
	}

	addrsArgs := ListAddressesArgs{
		Username: username,
		Password: password,
	}
	addrsReply := ListAddressesResponse{}
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

	args := &SendArgs{
		Username: username,
		Password: password,
		Amount:   500,
		AssetID:  assetID.String(),
		To:       vm.Format(addr.Bytes()),
	}
	reply := &SendReply{}
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

	createArgs := &CreateAddressArgs{
		Username: username,
		Password: password,
	}
	createReply := &CreateAddressReply{}

	if err := s.CreateAddress(nil, createArgs, createReply); err != nil {
		t.Fatalf("Failed to create address: %s", err)
	}

	newAddr := createReply.Address

	listArgs := &ListAddressesArgs{
		Username: username,
		Password: password,
	}
	listReply := &ListAddressesResponse{}

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
	smDB := vm.ctx.SharedMemory.GetDatabase(platformChainID)

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
	vm.ctx.SharedMemory.ReleaseDatabase(platformChainID)

	importArgs := &ImportAVAArgs{
		Username:    username,
		Password:    password,
		SourceChain: platformChainID.String(),
		To:          vm.Format(keys[0].PublicKey().Address().Bytes()),
	}
	importReply := &ImportAVAReply{}
	if err := s.ImportAVA(nil, importArgs, importReply); err != nil {
		t.Fatalf("Failed to import AVA due to %s", err)
	}
}
