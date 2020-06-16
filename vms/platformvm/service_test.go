// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/ava-labs/gecko/utils/formatting"
)

func TestAddDefaultSubnetValidator(t *testing.T) {
	expectedJSONString := `{"startTime":"0","endTime":"0","id":null,"destination":"","delegationFeeRate":"0","payerNonce":"0"}`
	args := AddDefaultSubnetValidatorArgs{}
	bytes, err := json.Marshal(&args)
	if err != nil {
		t.Fatal(err)
	}
	jsonString := string(bytes)
	if jsonString != expectedJSONString {
		t.Fatalf("Expected: %s\nResult: %s", expectedJSONString, jsonString)
	}
}

func TestCreateBlockchainArgsParsing(t *testing.T) {
	jsonString := `{"vmID":"lol","fxIDs":["secp256k1"], "name":"awesome", "payerNonce":5, "genesisData":"SkB92YpWm4Q2iPnLGCuDPZPgUQMxajqQQuz91oi3xD984f8r"}`
	args := CreateBlockchainArgs{}
	err := json.Unmarshal([]byte(jsonString), &args)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = json.Marshal(args.GenesisData); err != nil {
		t.Fatal(err)
	}
}

func TestExportKey(t *testing.T) {
	jsonString := `{"username":"ScoobyUser","password":"ShaggyPassword1","address":"6Y3kysjF9jnHnYkdS9yGAuoHyae2eNmeV"}`
	args := ExportKeyArgs{}
	err := json.Unmarshal([]byte(jsonString), &args)
	if err != nil {
		t.Fatal(err)
	}
}

func TestImportKey(t *testing.T) {
	jsonString := `{"username":"ScoobyUser","password":"ShaggyPassword1","privateKey":"ewoqjP7PxY4yr3iLTpLisriqt94hdyDFNgchSxGGztUrTXtNN"}`
	args := ImportKeyArgs{}
	err := json.Unmarshal([]byte(jsonString), &args)
	if err != nil {
		t.Fatal(err)
	}
}

func TestIssueTxKeepsTimedEventsSorted(t *testing.T) {
	vm := defaultVM()
	vm.Ctx.Lock.Lock()
	defer func() {
		vm.Shutdown()
		vm.Ctx.Lock.Unlock()
	}()

	service := Service{vm: vm}

	pendingValidatorStartTime1 := defaultGenesisTime.Add(3 * time.Second)
	pendingValidatorEndTime1 := pendingValidatorStartTime1.Add(MinimumStakingDuration)
	nodeIDKey1, _ := vm.factory.NewPrivateKey()
	nodeID1 := nodeIDKey1.PublicKey().Address()
	addPendingValidatorTx1, err := vm.newAddDefaultSubnetValidatorTx(
		defaultNonce+1,
		defaultStakeAmount,
		uint64(pendingValidatorStartTime1.Unix()),
		uint64(pendingValidatorEndTime1.Unix()),
		nodeID1,
		nodeID1,
		NumberOfShares,
		testNetworkID,
		defaultKey,
	)
	if err != nil {
		t.Fatal(err)
	}

	txBytes1, err := Codec.Marshal(genericTx{Tx: addPendingValidatorTx1})
	if err != nil {
		t.Fatal(err)
	}

	args1 := &IssueTxArgs{}
	args1.Tx = formatting.CB58{Bytes: txBytes1}
	reply1 := IssueTxResponse{}

	err = service.IssueTx(nil, args1, &reply1)
	if err != nil {
		t.Fatal(err)
	}

	pendingValidatorStartTime2 := defaultGenesisTime.Add(2 * time.Second)
	pendingValidatorEndTime2 := pendingValidatorStartTime2.Add(MinimumStakingDuration)
	nodeIDKey2, _ := vm.factory.NewPrivateKey()
	nodeID2 := nodeIDKey2.PublicKey().Address()
	addPendingValidatorTx2, err := vm.newAddDefaultSubnetValidatorTx(
		defaultNonce+1,
		defaultStakeAmount,
		uint64(pendingValidatorStartTime2.Unix()),
		uint64(pendingValidatorEndTime2.Unix()),
		nodeID2,
		nodeID2,
		NumberOfShares,
		testNetworkID,
		defaultKey,
	)
	if err != nil {
		t.Fatal(err)
	}

	txBytes2, err := Codec.Marshal(genericTx{Tx: addPendingValidatorTx2})
	if err != nil {
		t.Fatal(err)
	}

	args2 := IssueTxArgs{Tx: formatting.CB58{Bytes: txBytes2}}
	reply2 := IssueTxResponse{}

	err = service.IssueTx(nil, &args2, &reply2)
	if err != nil {
		t.Fatal(err)
	}

	pendingValidatorStartTime3 := defaultGenesisTime.Add(10 * time.Second)
	pendingValidatorEndTime3 := pendingValidatorStartTime3.Add(MinimumStakingDuration)
	nodeIDKey3, _ := vm.factory.NewPrivateKey()
	nodeID3 := nodeIDKey3.PublicKey().Address()
	addPendingValidatorTx3, err := vm.newAddDefaultSubnetValidatorTx(
		defaultNonce+1,
		defaultStakeAmount,
		uint64(pendingValidatorStartTime3.Unix()),
		uint64(pendingValidatorEndTime3.Unix()),
		nodeID3,
		nodeID3,
		NumberOfShares,
		testNetworkID,
		defaultKey,
	)
	if err != nil {
		t.Fatal(err)
	}

	txBytes3, err := Codec.Marshal(genericTx{Tx: addPendingValidatorTx3})
	if err != nil {
		t.Fatal(err)
	}

	args3 := IssueTxArgs{Tx: formatting.CB58{Bytes: txBytes3}}
	reply3 := IssueTxResponse{}

	err = service.IssueTx(nil, &args3, &reply3)
	if err != nil {
		t.Fatal(err)
	}

	pendingValidatorStartTime4 := defaultGenesisTime.Add(1 * time.Second)
	pendingValidatorEndTime4 := pendingValidatorStartTime4.Add(MinimumStakingDuration)
	nodeIDKey4, _ := vm.factory.NewPrivateKey()
	nodeID4 := nodeIDKey4.PublicKey().Address()
	addPendingValidatorTx4, err := vm.newAddDefaultSubnetValidatorTx(
		defaultNonce+1,
		defaultStakeAmount,
		uint64(pendingValidatorStartTime4.Unix()),
		uint64(pendingValidatorEndTime4.Unix()),
		nodeID4,
		nodeID4,
		NumberOfShares,
		testNetworkID,
		defaultKey,
	)
	if err != nil {
		t.Fatal(err)
	}

	txBytes4, err := Codec.Marshal(genericTx{Tx: addPendingValidatorTx4})
	if err != nil {
		t.Fatal(err)
	}

	args4 := IssueTxArgs{Tx: formatting.CB58{Bytes: txBytes4}}
	reply4 := IssueTxResponse{}

	err = service.IssueTx(nil, &args4, &reply4)
	if err != nil {
		t.Fatal(err)
	}

	pendingValidatorStartTime5 := defaultGenesisTime.Add(50 * time.Second)
	pendingValidatorEndTime5 := pendingValidatorStartTime5.Add(MinimumStakingDuration)
	nodeIDKey5, _ := vm.factory.NewPrivateKey()
	nodeID5 := nodeIDKey5.PublicKey().Address()
	addPendingValidatorTx5, err := vm.newAddDefaultSubnetValidatorTx(
		defaultNonce+1,
		defaultStakeAmount,
		uint64(pendingValidatorStartTime5.Unix()),
		uint64(pendingValidatorEndTime5.Unix()),
		nodeID5,
		nodeID5,
		NumberOfShares,
		testNetworkID,
		defaultKey,
	)
	if err != nil {
		t.Fatal(err)
	}

	txBytes5, err := Codec.Marshal(genericTx{Tx: addPendingValidatorTx5})
	if err != nil {
		t.Fatal(err)
	}

	args5 := IssueTxArgs{Tx: formatting.CB58{Bytes: txBytes5}}
	reply5 := IssueTxResponse{}

	err = service.IssueTx(nil, &args5, &reply5)
	if err != nil {
		t.Fatal(err)
	}

	currentEvent := vm.unissuedEvents.Remove()
	for vm.unissuedEvents.Len() > 0 {
		nextEvent := vm.unissuedEvents.Remove()
		if !currentEvent.StartTime().Before(nextEvent.StartTime()) {
			t.Fatal("IssueTx does not keep event heap ordered")
		}
		currentEvent = nextEvent
	}
}
