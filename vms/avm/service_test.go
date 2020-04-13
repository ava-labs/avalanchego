// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"fmt"
	"testing"

	"github.com/ava-labs/gecko/snow/choices"

	"github.com/ava-labs/gecko/database/memdb"
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow/engine/common"
	"github.com/ava-labs/gecko/utils/formatting"
	"github.com/ava-labs/gecko/vms/secp256k1fx"
)

func setup(t *testing.T) ([]byte, *VM, *Service) {
	genesisBytes := BuildGenesisTest(t)

	ctx.Lock.Lock()

	// This is VM initilialzation is very silimar to that done by GenesisVM().
	// However that function also sets vm.batchTimeout = 0, which causes the
	// tests in this module to deadlock.
	vm := &VM{}
	err := vm.Initialize(
		ctx,
		memdb.New(),
		genesisBytes,
		make(chan common.Message, 1),
		[]*common.Fx{&common.Fx{
			ID: ids.Empty,
			Fx: &secp256k1fx.Fx{},
		}},
	)
	if err != nil {
		t.Fatal(err)
	}
	s := &Service{vm: vm}
	return genesisBytes, vm, s
}

func TestServiceIssueTx(t *testing.T) {
	genesisBytes, vm, s := setup(t)
	// TODO: Does this need to be here? Or can the lock be released once the
	//       VM is initilized?
	defer ctx.Lock.Unlock()
	defer vm.Shutdown()

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
	defer ctx.Lock.Unlock()
	defer vm.Shutdown()

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

func TestServiceGetUTXOsInvalidAddress(t *testing.T) {
	_, vm, s := setup(t)
	defer ctx.Lock.Unlock()
	defer vm.Shutdown()

	addr0 := keys[0].PublicKey().Address()
	tests := []struct {
		label string
		args  *GetUTXOsArgs
	}{
		{"[", &GetUTXOsArgs{[]string{""}}},
		{"[-]", &GetUTXOsArgs{[]string{"-"}}},
		{"[foo]", &GetUTXOsArgs{[]string{"foo"}}},
		{"[foo-bar]", &GetUTXOsArgs{[]string{"foo-bar"}}},
		{"[<ChainID>]", &GetUTXOsArgs{[]string{ctx.ChainID.String()}}},
		{"[<ChainID>-]", &GetUTXOsArgs{[]string{fmt.Sprintf("%s-", ctx.ChainID.String())}}},
		{"[<Unknown ID>-<addr0>]", &GetUTXOsArgs{[]string{fmt.Sprintf("%s-%s", ids.NewID([32]byte{42}).String(), addr0.String())}}},
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
	defer ctx.Lock.Unlock()
	defer vm.Shutdown()

	addr0 := keys[0].PublicKey().Address()
	tests := []struct {
		label string
		args  *GetUTXOsArgs
		count int
	}{
		{
			"Empty",
			&GetUTXOsArgs{},
			0,
		}, {
			"[<ChainID>-<unrelated address>]",
			&GetUTXOsArgs{[]string{
				// TODO: Should GetUTXOs() raise an error for this? The address portion is
				//		 longer than addr0.String()
				fmt.Sprintf("%s-%s", ctx.ChainID.String(), ids.NewID([32]byte{42}).String()),
			}},
			0,
		}, {
			"[<ChainID>-<addr0>]",
			&GetUTXOsArgs{[]string{
				fmt.Sprintf("%s-%s", ctx.ChainID.String(), addr0.String()),
			}},
			7,
		}, {
			"[<ChainID>-<addr0>,<ChainID>-<addr0>]",
			&GetUTXOsArgs{[]string{
				fmt.Sprintf("%s-%s", ctx.ChainID.String(), addr0.String()),
				fmt.Sprintf("%s-%s", ctx.ChainID.String(), addr0.String()),
			}},
			7,
		},
	}
	for _, tt := range tests {
		t.Run(tt.label, func(t *testing.T) {
			utxosReply := &GetUTXOsReply{}
			if err := s.GetUTXOs(nil, tt.args, utxosReply); err != nil {
				t.Error(err)
			} else if tt.count != len(utxosReply.UTXOs) {
				t.Errorf("Expected %d utxos, got %#v", tt.count, len(utxosReply.UTXOs))
			}
		})
	}
}

func TestGetAssetDescription(t *testing.T) {
	genesisBytes, vm, s := setup(t)
	defer ctx.Lock.Unlock()
	defer vm.Shutdown()

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
	defer ctx.Lock.Unlock()
	defer vm.Shutdown()

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
	defer ctx.Lock.Unlock()
	defer vm.Shutdown()

	reply := CreateFixedCapAssetReply{}
	err := s.CreateFixedCapAsset(nil, &CreateFixedCapAssetArgs{
		Name:         "test asset",
		Symbol:       "test",
		Denomination: 1,
		InitialHolders: []*Holder{&Holder{
			Amount:  123456789,
			Address: vm.Format(keys[0].PublicKey().Address().Bytes()),
		}},
	}, &reply)
	if err != nil {
		t.Fatal(err)
	}

	if reply.AssetID.String() != "wWBk78PGAU4VkXhESr3jiYyMCEzzPPcnVYeEnNr9g4JuvYs2x" {
		t.Fatalf("Wrong assetID returned from CreateFixedCapAsset %s", reply.AssetID)
	}
}

func TestCreateVariableCapAsset(t *testing.T) {
	_, vm, s := setup(t)
	defer ctx.Lock.Unlock()
	defer vm.Shutdown()

	reply := CreateVariableCapAssetReply{}
	err := s.CreateVariableCapAsset(nil, &CreateVariableCapAssetArgs{
		Name:   "test asset",
		Symbol: "test",
		MinterSets: []Owners{
			Owners{
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

	if reply.AssetID.String() != "SscTvpQFCZPNiRXyueDc7LdHT9EstHiva3AK6kuTgHTMd7DsU" {
		t.Fatalf("Wrong assetID returned from CreateFixedCapAsset %s", reply.AssetID)
	}
}
