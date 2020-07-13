package platformvm

import (
	"bytes"
	cryptorand "crypto/rand"
	"reflect"
	"testing"

	"github.com/ava-labs/gecko/vms/secp256k1fx"

	"math/rand"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/vms/components/ava"
)

const (
	maxOutsPerUTXO  = 1000
	maxOwnersPerOut = 100
	maxOutAmt       = 1000 * 1000 * 1000 * 1000 // One trillion
)

// Generates a new, random ID
func generateRandomID() ids.ID {
	var bytes [32]byte
	n, err := cryptorand.Read(bytes[:])
	if err != nil || n != 32 {
		panic("couldn't generate 32 random bytes")
	}
	return ids.NewID(bytes)
}

// Generates a new, random Short ID
func generateRandomShortID() ids.ShortID {
	var bytes [20]byte
	n, err := cryptorand.Read(bytes[:])
	if err != nil || n != 20 {
		panic("couldn't generate 20 random bytes")
	}
	return ids.NewShortID(bytes)
}

// Generate [n] new UTXOs with randomly generated fields.
// Each UTXO is syntactically valid
func generateRandomUTXOs(n int) []*ava.UTXO {
	utxos := make([]*ava.UTXO, n)
	for i := 0; i < n; i++ {
		numOwners := rand.Intn(maxOutsPerUTXO)
		owners := make([]ids.ShortID, numOwners)
		for k := 0; k < numOwners; k++ {
			owners[k] = generateRandomShortID()
		}
		utxos[i] = &ava.UTXO{
			UTXOID: ava.UTXOID{
				TxID:        generateRandomID(),
				OutputIndex: rand.Uint32(),
			},
			Asset: ava.Asset{ID: avaxAssetID},
			Out: &secp256k1fx.TransferOutput{
				Amt: uint64(rand.Int63n(maxOutAmt)),
				OutputOwners: secp256k1fx.OutputOwners{
					Locktime:  0,
					Addrs:     owners,
					Threshold: uint32(len(owners)),
				},
			},
		}
	}
	return utxos
}

// Test getting and putting UTXOs
func TestStateGetPutUTXOs(t *testing.T) {
	vm := defaultVM()
	vm.Ctx.Lock.Lock()
	defer func() {
		vm.Shutdown()
		vm.Ctx.Lock.Unlock()
	}()

	numToTest := 500
	// generate 500 UTXOs put them, get them, make sure they're the same
	utxos := generateRandomUTXOs(numToTest)
	for _, utxo := range utxos {
		if err := vm.putUTXO(vm.DB, utxo); err != nil {
			t.Fatal(err)
		}
	}
	for _, utxo := range utxos {
		if retrievedUTXO, err := vm.getUTXO(vm.DB, utxo.InputID()); err != nil {
			if !reflect.DeepEqual(utxo, retrievedUTXO) {
				t.Fatal("Should be the same")
			}
		}
	}
}

// Test getting, putting and removing UTXOs
func TestStateUTXOs(t *testing.T) {
	vm := defaultVM()
	vm.Ctx.Lock.Lock()
	defer func() {
		vm.Shutdown()
		vm.Ctx.Lock.Unlock()
	}()

	numToTest := 10
	// generate 10 UTXOs, put them, get them, make sure they're the same, delete them, make sure they're gone
	utxos := generateRandomUTXOs(numToTest)
	for _, utxo := range utxos {
		utxoID := utxo.InputID()
		if err := vm.putUTXO(vm.DB, utxo); err != nil { // Store the UTXO
			t.Fatal(err)
		} else if retrievedUtxo, err := vm.getUTXO(vm.DB, utxoID); err != nil { // Retrieve it
			t.Fatal(err)
		} else if utxoBytes, err := Codec.Marshal(utxo); err != nil {
			t.Fatal(err)
		} else if retrievedUtxoBytes, err := Codec.Marshal(retrievedUtxo); err != nil {
			t.Fatal(err)
		} else if !bytes.Equal(utxoBytes, retrievedUtxoBytes) { // Make sure it's right
			t.Fatal("should be the same")
		}
		// Make sure each address referenced by this UTXO knows it is referenced by this UTXO
		referencedAddresses := utxo.Out.(*secp256k1fx.TransferOutput).Addrs
		for _, addr := range referencedAddresses {
			if utxos, err := vm.getReferencingUTXOs(vm.DB, addr); err != nil {
				t.Fatal(err)
			} else if utxos.Len() != 1 {
				t.Fatal("expected referencing UTXO set to contain only 1 UTXO")
			} else if !utxos.Contains(utxo.InputID()) {
				t.Fatal("referencing UTXO set has unexpected element")
			}
		}
		if err := vm.removeUTXO(vm.DB, utxoID); err != nil { // Remove the UTXO
			t.Fatal(err)
		} else if _, err := vm.getUTXO(vm.DB, utxoID); err == nil {
			t.Fatal("should have returned error because UTXO was removed")
		}
		// Make sure each address referenced by this UTXO knows the UTXO is removed
		for _, addr := range referencedAddresses {
			if utxos, err := vm.getReferencingUTXOs(vm.DB, addr); err != nil {
				t.Fatal(err)
			} else if utxos.Len() != 0 {
				t.Fatal("expected referencing UTXO set to contain no elements")
			}
		}
	}
}
