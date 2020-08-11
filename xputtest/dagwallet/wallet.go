// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package dagwallet

import (
	"fmt"
	"math"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/utils/crypto"
	"github.com/ava-labs/gecko/utils/timer"
	"github.com/ava-labs/gecko/vms/spdagvm"
)

// Wallet is a holder for keys and UTXOs for the Avalanche DAG.
type Wallet struct {
	networkID uint32
	chainID   ids.ID
	clock     timer.Clock
	keychain  *spdagvm.Keychain // Mapping from public address to the SigningKeys
	utxoSet   *UTXOSet          // Mapping from utxoIDs to UTXOs
	balance   uint64
	txFee     uint64
}

// NewWallet returns a new Wallet
func NewWallet(networkID uint32, chainID ids.ID, txFee uint64) *Wallet {
	return &Wallet{
		networkID: networkID,
		chainID:   chainID,
		keychain:  spdagvm.NewKeychain(networkID, chainID),
		utxoSet:   &UTXOSet{},
		txFee:     txFee,
	}
}

// GetAddress returns one of the addresses this wallet manages. If no address
// exists, one will be created.
func (w *Wallet) GetAddress() ids.ShortID {
	if w.keychain.Addrs.Len() == 0 {
		return w.CreateAddress()
	}
	return w.keychain.Addrs.CappedList(1)[0]
}

// CreateAddress returns a new address.
// It also saves the address and the private key that controls it
// so the address can be used later
func (w *Wallet) CreateAddress() ids.ShortID {
	privKey, _ := w.keychain.New()
	return privKey.PublicKey().Address()
}

// ImportKey imports a private key into this wallet
func (w *Wallet) ImportKey(sk *crypto.PrivateKeySECP256K1R) { w.keychain.Add(sk) }

// AddUTXO adds a new UTXO to this wallet if this wallet may spend it
// The UTXO's output must be an OutputPayment
func (w *Wallet) AddUTXO(utxo *spdagvm.UTXO) {
	out, ok := utxo.Out().(*spdagvm.OutputPayment)
	if !ok {
		return
	}

	if _, _, err := w.keychain.Spend(utxo, math.MaxUint64); err == nil {
		w.utxoSet.Put(utxo)
		w.balance += out.Amount()
	}
}

// Balance returns the amount of the assets in this wallet
func (w *Wallet) Balance() uint64 { return w.balance }

// Send sends [amount] to address [destAddr]
// The output of this transaction may be spent after [locktime]
func (w *Wallet) Send(amount uint64, locktime uint64, destAddr ids.ShortID) *spdagvm.Tx {
	builder := spdagvm.Builder{
		NetworkID: w.networkID,
		ChainID:   w.chainID,
	}
	currentTime := w.clock.Unix()

	// Send any change to an address this wallet controls
	changeAddr := ids.ShortID{}
	if w.keychain.Addrs.Len() < 1000 {
		changeAddr = w.CreateAddress()
	} else {
		changeAddr = w.GetAddress()
	}

	utxoList := w.utxoSet.UTXOs // List of UTXOs this wallet may spend

	destAddrs := []ids.ShortID{destAddr}

	// Build the transaction
	tx, err := builder.NewTxFromUTXOs(w.keychain, utxoList, amount, w.txFee, locktime, 1, destAddrs, changeAddr, currentTime)
	if err != nil {
		panic(err)
	}

	// Remove from [w.utxoSet] any UTXOs used to fund [tx]
	for _, in := range tx.Ins() {
		if in, ok := in.(*spdagvm.InputPayment); ok {
			inUTXOID := in.InputID()
			w.utxoSet.Remove(inUTXOID)
			w.balance -= in.Amount() // Deduct from [w.balance] the amount sent
		}
	}

	return tx
}

func (w Wallet) String() string {
	return fmt.Sprintf(
		"Keychain:\n"+
			"%s\n"+
			"UTXOSet:\n"+
			"%s",
		w.keychain.PrefixedString("    "),
		w.utxoSet.string("    "))
}
