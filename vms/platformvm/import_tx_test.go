package platformvm

import (
	"math/rand"
	"testing"

	"github.com/ava-labs/gecko/database"
	"github.com/ava-labs/gecko/database/memdb"
	"github.com/ava-labs/gecko/database/nodb"
	"github.com/ava-labs/gecko/database/versiondb"
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/utils/crypto"
	"github.com/ava-labs/gecko/vms/components/avax"
	"github.com/ava-labs/gecko/vms/secp256k1fx"
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
	vm := defaultVM()
	avmID := ids.GenerateTestID()
	vm.avm = avmID
	vm.Ctx.Lock.Lock()
	defer func() {
		vm.Shutdown()
		vm.Ctx.Lock.Unlock()
	}()

	type test struct {
		description   string
		sharedMemory  MockSharedMemory
		feeKeys       []*crypto.PrivateKeySECP256K1R
		recipientKeys []*crypto.PrivateKeySECP256K1R
		shouldErr     bool
	}

	factory := crypto.FactorySECP256K1R{}
	recipientKeyIntf, err := factory.NewPrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	recipientKey := recipientKeyIntf.(*crypto.PrivateKeySECP256K1R)

	// Returns a shared memory where GetDatabase returns a database
	// where [recipientKey] has a balance of [amt]
	fundedSharedMemory := func(amt uint64) MockSharedMemory {
		return MockSharedMemory{
			GetDatabaseF: func(ids.ID) database.Database {
				db := memdb.New()
				state := avax.NewPrefixedState(db, Codec, avmID, vm.Ctx.ChainID)
				if err := state.FundUTXO(&avax.UTXO{
					UTXOID: avax.UTXOID{
						TxID:        ids.GenerateTestID(),
						OutputIndex: rand.Uint32(),
					},
					Asset: avax.Asset{ID: avaxAssetID},
					Out: &secp256k1fx.TransferOutput{
						Amt: amt,
						OutputOwners: secp256k1fx.OutputOwners{
							Locktime:  0,
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
	}

	tests := []test{
		{
			description:   "recipient key can't pay fee;",
			sharedMemory:  fundedSharedMemory(vm.txFee - 1),
			recipientKeys: []*crypto.PrivateKeySECP256K1R{recipientKey},
			shouldErr:     true,
		},
		{

			description:   "recipient key pays fee",
			sharedMemory:  fundedSharedMemory(vm.txFee),
			recipientKeys: []*crypto.PrivateKeySECP256K1R{recipientKey},
			shouldErr:     false,
		},
	}

	vdb := versiondb.New(vm.DB)
	to := ids.GenerateTestShortID()
	for _, tt := range tests {
		vm.Ctx.SharedMemory = tt.sharedMemory
		tx, err := vm.newImportTx(avmID, to, tt.recipientKeys)
		if err != nil {
			if !tt.shouldErr {
				t.Fatalf("test '%s' unexpectedly errored with: %s", tt.description, err)
			}
			continue
		} else if tt.shouldErr {
			t.Fatalf("test '%s' didn't error but it should have", tt.description)
		}
		unsignedTx := tx.UnsignedAtomicTx.(*UnsignedImportTx)
		if len(unsignedTx.ImportedInputs) == 0 {
			t.Fatalf("in test '%s', tx has no imported inputs", tt.description)
		} else if len(tx.Credentials) != len(unsignedTx.Ins)+len(unsignedTx.ImportedInputs) {
			t.Fatalf("in test '%s', should have same number of credentials as inputs", tt.description)
		}
		totalIn := uint64(0)
		for _, in := range unsignedTx.Ins {
			totalIn += in.Input().Amount()
		}
		for _, in := range unsignedTx.ImportedInputs {
			totalIn += in.Input().Amount()
		}
		totalOut := uint64(0)
		for _, out := range unsignedTx.Outs {
			totalOut += out.Out.Amount()
		}
		if totalIn-totalOut != vm.txFee {
			t.Fatalf("in test '%s'. inputs (%d) != outputs (%d) + txFee (%d)", tt.description, totalIn, totalOut, vm.txFee)
		}
		vdb.Abort()
	}
}
