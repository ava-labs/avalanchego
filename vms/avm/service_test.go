// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"testing"

	"github.com/ava-labs/gecko/database/memdb"
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow/engine/common"
	"github.com/ava-labs/gecko/vms/secp256k1fx"
)

func TestGetAssetDescription(t *testing.T) {
	genesisBytes := BuildGenesisTest(t)

	ctx.Lock.Lock()
	defer ctx.Lock.Unlock()

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
	defer vm.Shutdown()

	genesisTx := GetFirstTxFromGenesisTest(genesisBytes, t)

	avaAssetID := genesisTx.ID()

	s := Service{vm: vm}

	reply := GetAssetDescriptionReply{}
	err = s.GetAssetDescription(nil, &GetAssetDescriptionArgs{
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
	genesisBytes := BuildGenesisTest(t)

	ctx.Lock.Lock()
	defer ctx.Lock.Unlock()

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
	defer vm.Shutdown()

	genesisTx := GetFirstTxFromGenesisTest(genesisBytes, t)

	avaAssetID := genesisTx.ID()

	s := Service{vm: vm}

	reply := GetBalanceReply{}
	err = s.GetBalance(nil, &GetBalanceArgs{
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
	genesisBytes := BuildGenesisTest(t)

	ctx.Lock.Lock()
	defer ctx.Lock.Unlock()

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
	defer vm.Shutdown()

	s := Service{vm: vm}

	reply := CreateFixedCapAssetReply{}
	err = s.CreateFixedCapAsset(nil, &CreateFixedCapAssetArgs{
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

	if reply.AssetID.String() != "2PEdmaGjKsSd14xPHZkVjhDdxH1VBsCATW8gnmqfMYfp68EwU5" {
		t.Fatalf("Wrong assetID returned from CreateFixedCapAsset %s", reply.AssetID)
	}
}

func TestCreateVariableCapAsset(t *testing.T) {
	genesisBytes := BuildGenesisTest(t)

	ctx.Lock.Lock()
	defer ctx.Lock.Unlock()

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
	defer vm.Shutdown()

	s := Service{vm: vm}

	reply := CreateVariableCapAssetReply{}
	err = s.CreateVariableCapAsset(nil, &CreateVariableCapAssetArgs{
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

	if reply.AssetID.String() != "p58dzpQikQmKVp3QtXMrg4e9AcppDc7MgqjpQf18CiNrpr2ug" {
		t.Fatalf("Wrong assetID returned from CreateFixedCapAsset %s", reply.AssetID)
	}
}
