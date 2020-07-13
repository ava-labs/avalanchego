package platformvm

import (
	"math/rand"
	"testing"

	"github.com/ava-labs/gecko/vms/secp256k1fx"

	"github.com/ava-labs/gecko/database/memdb"
	"github.com/ava-labs/gecko/database/nodb"
	"github.com/ava-labs/gecko/vms/components/ava"

	"github.com/ava-labs/gecko/database"
	"github.com/ava-labs/gecko/database/versiondb"
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/utils/crypto"
)

// implements snow.SharedMemory
type MockSharedMemory struct {
	GetDatabaseF func(ids.ID) database.Database
}

func (msm MockSharedMemory) GetDatabase(ID ids.ID) database.Database {
	if msm.GetDatabaseF != nil {
		return msm.GetDatabaseF(ID)
	}
	return &nodb.Database{}
}

func (msm MockSharedMemory) ReleaseDatabase(ID ids.ID) {}

func TestNewImportTx(t *testing.T) {
	type test struct {
		description  string
		sharedMemory MockSharedMemory
		feeKeys      []*crypto.PrivateKeySECP256K1R
		recipientKey *crypto.PrivateKeySECP256K1R
		shouldErr    bool
	}

	// Returns an empty database
	//emptyDBFunc := func(ids.ID) database.Database { return memdb.New() }

	factory := crypto.FactorySECP256K1R{}
	recipientKeyIntf, err := factory.NewPrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	recipientKey := recipientKeyIntf.(*crypto.PrivateKeySECP256K1R)

	// Returns a shared memory where GetDatabase returns a database
	// where [recipientKey] has a balance of 100,000
	fundedSharedMemory := MockSharedMemory{
		GetDatabaseF: func(ids.ID) database.Database {
			db := memdb.New()
			state := ava.NewPrefixedState(db, Codec)
			if err := state.FundAVMUTXO(&ava.UTXO{
				UTXOID: ava.UTXOID{
					TxID:        generateRandomID(),
					OutputIndex: rand.Uint32(),
				},
				Asset: ava.Asset{ID: avaxAssetID},
				Out: &secp256k1fx.TransferOutput{
					Amt:      50000,
					Locktime: 0,
					OutputOwners: secp256k1fx.OutputOwners{
						Addrs:     []ids.ShortID{recipientKey.PublicKey().Address()},
						Threshold: 1,
					},
				},
			}); err != nil {
				panic(err)
			}
			return db
		},
	}

	tests := []test{
		{
			description:  "no fee keys provided",
			sharedMemory: fundedSharedMemory,
			feeKeys:      nil,
			recipientKey: recipientKey,
			shouldErr:    true,
		},
		{
			description:  "no recipient keys provided",
			sharedMemory: fundedSharedMemory,
			feeKeys:      []*crypto.PrivateKeySECP256K1R{keys[0]},
			recipientKey: nil,
			shouldErr:    true,
		},
		{
			description:  "recipient key has no funds",
			sharedMemory: fundedSharedMemory,
			feeKeys:      []*crypto.PrivateKeySECP256K1R{keys[0]},
			recipientKey: keys[0],
			shouldErr:    true,
		},
		{
			description:  "tx fee payer has no funds",
			sharedMemory: fundedSharedMemory,
			feeKeys:      []*crypto.PrivateKeySECP256K1R{recipientKey},
			recipientKey: recipientKey,
			shouldErr:    true,
		},
	}

	vm := defaultVM()
	avmID := generateRandomID()
	vm.avm = avmID
	vm.Ctx.Lock.Lock()
	defer func() {
		vm.Shutdown()
		vm.Ctx.Lock.Unlock()
	}()
	vdb := versiondb.New(vm.DB)
	for _, tt := range tests {
		vm.Ctx.SharedMemory = tt.sharedMemory
		if tx, err := vm.newImportTx(tt.feeKeys, tt.recipientKey); err != nil {
			if !tt.shouldErr {
				t.Fatalf("test '%s' errored but it shouldn't have", tt.description)
			}
		} else if tt.shouldErr {
			t.Fatalf("test '%s' didn't error but it should have", tt.description)
		} else if len(tx.BaseTx.Inputs) == 0 {
			t.Fatal("tx has no inputs to pay fee")
		} else if len(tx.ImportedInputs) == 0 {
			t.Fatal("tx has no imported inputs")
		} else if len(tx.Outs()) == 0 {
			t.Fatal("tx has no outputs")
		} else if len(tx.Creds()) != len(tx.Ins()) {
			t.Fatal("should have same number of credentials as inputs")
		}
		vdb.Abort()
	}
}
