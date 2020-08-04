// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"fmt"
	"testing"

	"github.com/ava-labs/gecko/utils/hashing"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/gecko/api/keystore"
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow/choices"
	"github.com/ava-labs/gecko/utils/crypto"
	"github.com/ava-labs/gecko/utils/formatting"
	"github.com/ava-labs/gecko/utils/json"
	"github.com/ava-labs/gecko/vms/components/ava"
	"github.com/ava-labs/gecko/vms/secp256k1fx"
)

func setup(t *testing.T) ([]byte, *VM, *Service) {
	genesisBytes, _, vm := GenesisVM(t)
	s := &Service{vm: vm}
	return genesisBytes, vm, s
}

func TestServiceIssueTx(t *testing.T) {
	genesisBytes, vm, s := setup(t)
	ctx := vm.ctx
	defer func() {
		vm.Shutdown()
		ctx.Lock.Unlock()
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
	ctx := vm.ctx
	defer func() {
		vm.Shutdown()
		ctx.Lock.Unlock()
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
	ctx := vm.ctx
	defer ctx.Lock.Unlock()
	defer vm.Shutdown()

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

func TestServiceGetTx(t *testing.T) {
	genesisBytes, vm, s := setup(t)
	ctx := vm.ctx
	defer func() {
		vm.Shutdown()
		ctx.Lock.Unlock()
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
	ctx := vm.ctx
	defer func() {
		vm.Shutdown()
		ctx.Lock.Unlock()
	}()

	reply := GetTxReply{}
	err := s.GetTx(nil, &GetTxArgs{}, &reply)
	assert.Error(t, err, "Nil TxID should have returned an error")
}

func TestServiceGetUnknownTx(t *testing.T) {
	_, vm, s := setup(t)
	ctx := vm.ctx
	defer func() {
		vm.Shutdown()
		ctx.Lock.Unlock()
	}()

	reply := GetTxReply{}
	err := s.GetTx(nil, &GetTxArgs{TxID: ids.Empty}, &reply)
	assert.Error(t, err, "Unknown TxID should have returned an error")
}

func TestServiceGetUTXOsInvalidAddress(t *testing.T) {
	_, vm, s := setup(t)
	ctx := vm.ctx
	defer func() {
		vm.Shutdown()
		ctx.Lock.Unlock()
	}()

	addr0 := keys[0].PublicKey().Address()
	tests := []struct {
		label string
		args  *GetUTXOsArgs
	}{
		{"[", &GetUTXOsArgs{[]string{""}, 0, Index{}}},
		{"[-]", &GetUTXOsArgs{[]string{"-"}, 0, Index{}}},
		{"[foo]", &GetUTXOsArgs{[]string{"foo"}, 0, Index{}}},
		{"[foo-bar]", &GetUTXOsArgs{[]string{"foo-bar"}, 0, Index{}}},
		{"[<ChainID>]", &GetUTXOsArgs{[]string{ctx.ChainID.String()}, 0, Index{}}},
		{"[<ChainID>-]", &GetUTXOsArgs{[]string{fmt.Sprintf("%s-", ctx.ChainID.String())}, 0, Index{}}},
		{"[<Unknown ID>-<addr0>]", &GetUTXOsArgs{[]string{fmt.Sprintf("%s-%s", ids.NewID([32]byte{42}).String(), addr0.String())}, 0, Index{}}},
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
	ctx := vm.ctx
	defer func() {
		vm.Shutdown()
		ctx.Lock.Unlock()
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
			&GetUTXOsArgs{[]string{
				fmt.Sprintf("%s-%s", ctx.ChainID.String(), ids.NewID([32]byte{42}).String()),
			},
				0,
				Index{},
			},
			0,
			true,
		}, {
			"[<ChainID>-<addr>]",
			&GetUTXOsArgs{[]string{
				fmt.Sprintf("%s-%s", ctx.ChainID.String(), addr.String()),
			},
				0,
				Index{},
			},
			numUtxos,
			false,
		},
		{
			"[<ChainID>-<addr>] limit to 1 UTXO",
			&GetUTXOsArgs{[]string{
				fmt.Sprintf("%s-%s", ctx.ChainID.String(), addr.String()),
			},
				1,
				Index{},
			},
			1,
			false,
		},
		{
			"[<ChainID>-<addr>] limit greater than number of UTXOs",
			&GetUTXOsArgs{[]string{
				fmt.Sprintf("%s-%s", ctx.ChainID.String(), addr.String()),
			},
				100000,
				Index{},
			},
			numUtxos,
			false,
		},
		{
			"[<ChainID>-<addr>,<ChainID>-<addr>]",
			&GetUTXOsArgs{[]string{
				fmt.Sprintf("%s-%s", ctx.ChainID.String(), addr.String()),
				fmt.Sprintf("%s-%s", ctx.ChainID.String(), addr.String()),
			},
				0,
				Index{},
			},
			numUtxos,
			false,
		}, {
			"[<ChainID>-<addr>,<ChainID>-<addr>], limit to 1 UTXO",
			&GetUTXOsArgs{[]string{
				fmt.Sprintf("%s-%s", ctx.ChainID.String(), addr.String()),
				fmt.Sprintf("%s-%s", ctx.ChainID.String(), addr.String()),
			},
				1,
				Index{},
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
		[]string{fmt.Sprintf("%s-%s", ctx.ChainID.String(), addr.String())},
		5,
		Index{},
	}
	utxos := ids.Set{}
	if err := s.GetUTXOs(nil, args, reply); err != nil {
		t.Fatal(err)
	}
	for _, utxo := range reply.UTXOs { // Remember these UTXOs
		utxos.Add(ids.NewID(hashing.ComputeHash256Array(utxo.Bytes)))
	}
	args = &GetUTXOsArgs{
		[]string{fmt.Sprintf("%s-%s", ctx.ChainID.String(), addr.String())},
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

func TestServiceGetAtomicUTXOsInvalidAddress(t *testing.T) {
	_, vm, s := setup(t)
	ctx := vm.ctx
	defer func() {
		vm.Shutdown()
		ctx.Lock.Unlock()
	}()

	addr0 := keys[0].PublicKey().Address()
	tests := []struct {
		label string
		args  *GetAtomicUTXOsArgs
	}{
		{"[", &GetAtomicUTXOsArgs{[]string{""}, 0, Index{}}},
		{"[-]", &GetAtomicUTXOsArgs{[]string{"-"}, 0, Index{}}},
		{"[foo]", &GetAtomicUTXOsArgs{[]string{"foo"}, 0, Index{}}},
		{"[foo-bar]", &GetAtomicUTXOsArgs{[]string{"foo-bar"}, 0, Index{}}},
		{"[<ChainID>]", &GetAtomicUTXOsArgs{[]string{ctx.ChainID.String()}, 0, Index{}}},
		{"[<ChainID>-]", &GetAtomicUTXOsArgs{[]string{fmt.Sprintf("%s-", ctx.ChainID.String())}, 0, Index{}}},
		{"[<Unknown ID>-<addr0>]", &GetAtomicUTXOsArgs{[]string{fmt.Sprintf("%s-%s", ids.NewID([32]byte{42}).String(), addr0.String())}, 0, Index{}}},
	}
	for _, tt := range tests {
		t.Run(tt.label, func(t *testing.T) {
			utxosReply := &GetAtomicUTXOsReply{}
			if err := s.GetAtomicUTXOs(nil, tt.args, utxosReply); err == nil {
				t.Error(err)
			}
		})
	}
}

func TestServiceGetAtomicUTXOs(t *testing.T) {
	_, vm, s := setup(t)
	ctx := vm.ctx
	defer func() {
		vm.Shutdown()
		ctx.Lock.Unlock()
	}()

	addr1 := ids.GenerateTestShortID()
	addr2 := ids.GenerateTestShortID()

	smDB := vm.ctx.SharedMemory.GetDatabase(vm.platform)
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
	vm.ctx.SharedMemory.ReleaseDatabase(vm.platform)

	tests := []struct {
		label         string
		args          *GetAtomicUTXOsArgs
		expectedCount int
		shouldErr     bool
	}{
		{
			"Empty",
			&GetAtomicUTXOsArgs{},
			0,
			false,
		},
		{
			"[<ChainID>-<invalid address>]",
			&GetAtomicUTXOsArgs{[]string{
				fmt.Sprintf("%s-%s", ctx.ChainID.String(), ids.NewID([32]byte{42}).String()),
			},
				0,
				Index{},
			},
			0,
			true,
		},
		{
			"[<ChainID>-<addr1>]",
			&GetAtomicUTXOsArgs{[]string{
				fmt.Sprintf("%s-%s", ctx.ChainID.String(), addr1.String()),
			},
				0,
				Index{},
			},
			numUtxos,
			false,
		},
		{
			"[<ChainID>-<add1r>,<ChainID>-<addr2>]",
			&GetAtomicUTXOsArgs{[]string{
				fmt.Sprintf("%s-%s", ctx.ChainID.String(), addr1.String()),
				fmt.Sprintf("%s-%s", ctx.ChainID.String(), addr2.String()),
			},
				0,
				Index{},
			},
			numUtxos,
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.label, func(t *testing.T) {
			utxosReply := &GetAtomicUTXOsReply{}
			if err := s.GetAtomicUTXOs(nil, tt.args, utxosReply); err != nil && !tt.shouldErr {
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
	reply := &GetAtomicUTXOsReply{}
	args := &GetAtomicUTXOsArgs{
		[]string{fmt.Sprintf("%s-%s", ctx.ChainID.String(), addr1.String())},
		5,
		Index{},
	}
	utxos := ids.Set{}
	if err := s.GetAtomicUTXOs(nil, args, reply); err != nil {
		t.Fatal(err)
	}
	for _, utxo := range reply.UTXOs { // Remember these UTXOs
		utxos.Add(ids.NewID(hashing.ComputeHash256Array(utxo.Bytes)))
	}
	args = &GetAtomicUTXOsArgs{
		[]string{fmt.Sprintf("%s-%s", ctx.ChainID.String(), addr1.String())},
		json.Uint32(numUtxos - 5),
		reply.EndIndex,
	}
	if err := s.GetAtomicUTXOs(nil, args, reply); err != nil {
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
	ctx := vm.ctx
	defer func() {
		vm.Shutdown()
		ctx.Lock.Unlock()
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
	ctx := vm.ctx
	defer func() {
		vm.Shutdown()
		ctx.Lock.Unlock()
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
	ctx := vm.ctx
	defer func() {
		vm.Shutdown()
		ctx.Lock.Unlock()
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
	_, vm, s := setup(t)
	ctx := vm.ctx
	defer func() {
		vm.Shutdown()
		ctx.Lock.Unlock()
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

	if reply.AssetID.String() != "23FV5zQpuG9EZBh7BXKj9wqPAMe7tY9T4jEWpobbMQHLLUf88o" {
		t.Fatalf("Wrong assetID returned from CreateVariableCapAsset %s", reply.AssetID)
	}
}

func TestImportAVMKey(t *testing.T) {
	_, vm, s := setup(t)
	ctx := vm.ctx
	defer func() {
		vm.Shutdown()
		ctx.Lock.Unlock()
	}()

	userKeystore := keystore.CreateTestKeystore(t)

	username := "bobby"
	password := "StrnasfqewiurPasswdn56d"
	if err := userKeystore.AddUser(username, password); err != nil {
		t.Fatal(err)
	}

	vm.ctx.Keystore = userKeystore.NewBlockchainKeyStore(vm.ctx.ChainID)
	_, err := vm.ctx.Keystore.GetDatabase(username, password)
	if err != nil {
		t.Fatal(err)
	}

	factory := crypto.FactorySECP256K1R{}
	skIntf, err := factory.NewPrivateKey()
	if err != nil {
		t.Fatalf("problem generating private key: %s", err)
	}
	sk := skIntf.(*crypto.PrivateKeySECP256K1R)

	args := ImportKeyArgs{
		Username:   username,
		Password:   password,
		PrivateKey: formatting.CB58{Bytes: sk.Bytes()},
	}
	reply := ImportKeyReply{}
	if err = s.ImportKey(nil, &args, &reply); err != nil {
		t.Fatal(err)
	}
}

func TestImportAVMKeyNoDuplicates(t *testing.T) {
	_, vm, s := setup(t)
	ctx := vm.ctx
	defer func() {
		vm.Shutdown()
		ctx.Lock.Unlock()
	}()

	userKeystore := keystore.CreateTestKeystore(t)

	username := "bobby"
	password := "StrnasfqewiurPasswdn56d"
	if err := userKeystore.AddUser(username, password); err != nil {
		t.Fatal(err)
	}

	vm.ctx.Keystore = userKeystore.NewBlockchainKeyStore(vm.ctx.ChainID)
	_, err := vm.ctx.Keystore.GetDatabase(username, password)
	if err != nil {
		t.Fatal(err)
	}

	factory := crypto.FactorySECP256K1R{}
	skIntf, err := factory.NewPrivateKey()
	if err != nil {
		t.Fatalf("problem generating private key: %s", err)
	}
	sk := skIntf.(*crypto.PrivateKeySECP256K1R)

	args := ImportKeyArgs{
		Username:   username,
		Password:   password,
		PrivateKey: formatting.CB58{Bytes: sk.Bytes()},
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
