// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/ava-labs/gecko/chains/atomic"
	"github.com/ava-labs/gecko/database/memdb"
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow/choices"
	"github.com/ava-labs/gecko/utils/logging"
	"github.com/ava-labs/gecko/vms/components/ava"
	"github.com/ava-labs/gecko/vms/secp256k1fx"
)

func setup() (*VM, *Service) {
	vm := defaultVM()
	s := &Service{vm: vm}
	return vm, s
}

func TestAddDefaultSubnetValidator(t *testing.T) {
	expectedJSONString := `{"startTime":"0","endTime":"0","id":null,"destination":null,"delegationFeeRate":"0","payerNonce":"0"}`
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

func TestServiceGetTxStatus(t *testing.T) {
	vm, s := setup()

	avmID := ids.Empty.Prefix(0)
	amount := uint64(50000)
	assetID := ids.Empty.Prefix(2)
	key := keys[0]
	sm := &atomic.SharedMemory{}
	sm.Initialize(logging.NoLog{}, memdb.New())

	vm.Ctx.SharedMemory = sm.NewBlockchainSharedMemory(vm.Ctx.ChainID)

	statusArgs := &GetTxStatusArgs{}
	statusReply := &GetTxStatusReply{}
	if err := s.GetTxStatus(nil, statusArgs, statusReply); err == nil {
		t.Fatal("Expected empty transaction to return an error")
	}

	tx, err := vm.newExportTx(
		defaultNonce+1,
		testNetworkID,
		[]*ava.TransferableOutput{&ava.TransferableOutput{
			Asset: ava.Asset{ID: assetID},
			Out: &secp256k1fx.TransferOutput{
				Amt: amount,
			},
		}},
		key,
	)
	if err != nil {
		t.Fatal(err)
	}

	vm.Ctx.Lock.Lock()
	defer vm.Ctx.Lock.Unlock()

	txType := reflect.TypeOf(tx)
	statusArgs.TxID = tx.ID()
	statusArgs.TxType = txType.String()
	if err := s.GetTxStatus(nil, statusArgs, statusReply); err != nil {
		t.Fatal(err)
	}
	if expected := choices.Unknown; expected != statusReply.Status {
		t.Fatalf(
			"Expected an unsubmitted tx to have status %q, got %q",
			expected.String(), statusReply.Status.String(),
		)
	}

	vm.ava = assetID
	vm.avm = avmID

	vm.unissuedAtomicTxs = append(vm.unissuedAtomicTxs, tx)

	blk, _ := vm.BuildBlock()

	if err := s.GetTxStatus(nil, statusArgs, statusReply); err != nil {
		t.Fatal(err)
	}
	if expected := choices.Processing; expected != statusReply.Status {
		t.Fatalf(
			"Expected an unsubmitted tx to have status %q, got %q",
			expected.String(), statusReply.Status.String(),
		)
	}
	if err := blk.Verify(); err != nil {
		t.Fatal(err)
	}
	blk.Accept()
	if err := s.GetTxStatus(nil, statusArgs, statusReply); err != nil {
		t.Fatal(err)
	}
	if expected := choices.Accepted; expected != statusReply.Status {
		t.Fatalf(
			"Expected an unsubmitted tx to have status %q, got %q",
			expected.String(), statusReply.Status.String(),
		)
	}
}
