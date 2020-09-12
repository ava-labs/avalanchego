// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avmwallet

import (
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

func TestNewWallet(t *testing.T) {
	chainID := ids.NewID([32]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10})
	w, err := NewWallet(logging.NoLog{}, 12345, chainID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if w == nil {
		t.Fatalf("failed to create the new wallet")
	}
}

func TestWalletGetAddress(t *testing.T) {
	chainID := ids.NewID([32]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10})
	w, err := NewWallet(logging.NoLog{}, 12345, chainID, 0)
	if err != nil {
		t.Fatal(err)
	}

	addr0, err := w.GetAddress()
	if err != nil {
		t.Fatal(err)
	}
	if addr0.IsZero() || addr0.Equals(ids.ShortEmpty) {
		t.Fatalf("expected new address but got %s", addr0)
	}
}

func TestWalletGetMultipleAddresses(t *testing.T) {
	chainID := ids.NewID([32]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10})
	w, err := NewWallet(logging.NoLog{}, 12345, chainID, 0)
	if err != nil {
		t.Fatal(err)
	}

	addr0, err := w.GetAddress()
	if err != nil {
		t.Fatal(err)
	}
	addr1, err := w.GetAddress()
	if err != nil {
		t.Fatal(err)
	}
	if !addr0.Equals(addr1) {
		t.Fatalf("Should have returned the same address from multiple Get Address calls")
	}
}

func TestWalletEmptyBalance(t *testing.T) {
	chainID := ids.NewID([32]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10})
	w, err := NewWallet(logging.NoLog{}, 12345, chainID, 0)
	if err != nil {
		t.Fatal(err)
	}

	if balance := w.Balance(ids.Empty); balance != 0 {
		t.Fatalf("expected balance to be 0, was %d", balance)
	}
}

func TestWalletAddUTXO(t *testing.T) {
	chainID := ids.NewID([32]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10})
	w, err := NewWallet(logging.NoLog{}, 12345, chainID, 0)
	if err != nil {
		t.Fatal(err)
	}

	utxo := &avax.UTXO{
		UTXOID: avax.UTXOID{TxID: ids.Empty.Prefix(0)},
		Asset:  avax.Asset{ID: ids.Empty.Prefix(1)},
		Out: &secp256k1fx.TransferOutput{
			Amt: 1000,
		},
	}

	w.AddUTXO(utxo)

	if balance := w.Balance(utxo.AssetID()); balance != 1000 {
		t.Fatalf("expected balance to be 1000, was %d", balance)
	}
}

func TestWalletAddInvalidUTXO(t *testing.T) {
	chainID := ids.NewID([32]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10})
	w, err := NewWallet(logging.NoLog{}, 12345, chainID, 0)
	if err != nil {
		t.Fatal(err)
	}

	utxo := &avax.UTXO{
		UTXOID: avax.UTXOID{TxID: ids.Empty.Prefix(0)},
		Asset:  avax.Asset{ID: ids.Empty.Prefix(1)},
	}

	w.AddUTXO(utxo)

	if balance := w.Balance(utxo.AssetID()); balance != 0 {
		t.Fatalf("expected balance to be 0, was %d", balance)
	}
}

func TestWalletCreateTx(t *testing.T) {
	chainID := ids.NewID([32]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10})
	w, err := NewWallet(logging.NoLog{}, 12345, chainID, 0)
	if err != nil {
		t.Fatal(err)
	}

	assetID := ids.Empty.Prefix(0)

	addr, err := w.GetAddress()
	if err != nil {
		t.Fatal(err)
	}
	utxo := &avax.UTXO{
		UTXOID: avax.UTXOID{TxID: ids.Empty.Prefix(1)},
		Asset:  avax.Asset{ID: assetID},
		Out: &secp256k1fx.TransferOutput{
			Amt: 1000,
			OutputOwners: secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{addr},
			},
		},
	}

	w.AddUTXO(utxo)

	destAddr, err := w.CreateAddress()
	if err != nil {
		t.Fatal(err)
	}

	tx, err := w.CreateTx(assetID, 1000, destAddr)
	if err != nil {
		t.Fatal(err)
	}

	if balance := w.Balance(utxo.AssetID()); balance != 1000 {
		t.Fatalf("expected balance to be 1000, was %d", balance)
	}

	for _, utxo := range tx.InputUTXOs() {
		w.RemoveUTXO(utxo.InputID())
	}

	if balance := w.Balance(utxo.AssetID()); balance != 0 {
		t.Fatalf("expected balance to be 0, was %d", balance)
	}
}

func TestWalletImportKey(t *testing.T) {
	chainID := ids.NewID([32]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10})
	w, err := NewWallet(logging.NoLog{}, 12345, chainID, 0)
	if err != nil {
		t.Fatal(err)
	}

	factory := crypto.FactorySECP256K1R{}
	sk, err := factory.NewPrivateKey()
	if err != nil {
		t.Fatal(err)
	}

	w.ImportKey(sk.(*crypto.PrivateKeySECP256K1R))

	addr0 := sk.PublicKey().Address()
	addr1, err := w.GetAddress()
	if err != nil {
		t.Fatal(err)
	}
	if !addr0.Equals(addr1) {
		t.Fatalf("Should have returned the same address from the Get Address call")
	}
}

func TestWalletString(t *testing.T) {
	chainID := ids.NewID([32]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10})
	w, err := NewWallet(logging.NoLog{}, 12345, chainID, 0)
	if err != nil {
		t.Fatal(err)
	}

	skBytes := []byte{
		0x4a, 0x99, 0x82, 0x98, 0x5c, 0x39, 0xa8, 0x04,
		0x87, 0x4c, 0x62, 0x3c, 0xd4, 0x9e, 0xa7, 0x7d,
		0x63, 0x5f, 0x92, 0x7c, 0xb9, 0x6b, 0x3f, 0xb7,
		0x3b, 0x93, 0x59, 0xa2, 0x4f, 0xb4, 0x0c, 0x9e,
	}
	factory := crypto.FactorySECP256K1R{}
	sk, err := factory.ToPrivateKey(skBytes)
	if err != nil {
		t.Fatal(err)
	}

	w.ImportKey(sk.(*crypto.PrivateKeySECP256K1R))

	expected := "Keychain:" +
		"\n    Key[0]: Key: ZrYnAmArnk97JGzkq3kxTmFuKQnmajc86Xyd3JXC29meZ7znH Address: EHQiyKpq1VxkyNzt9bj1BLn5tzQ6Vt96q" +
		"\nUTXOs (length=0):"
	if str := w.String(); str != expected {
		t.Fatalf("got:\n%s\n\nexpected:\n%s", str, expected)
	}
}
