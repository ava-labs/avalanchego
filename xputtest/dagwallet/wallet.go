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

// Wallet is a holder for keys and UTXOs for the Ava DAG.
type Wallet struct {
	networkID uint32
	chainID   ids.ID
	clock     timer.Clock
	keyChain  *spdagvm.KeyChain // Mapping from public address to the SigningKeys
	utxoSet   *UtxoSet          // Mapping from utxoIDs to Utxos
	balance   uint64
	txFee     uint64
}

// NewWallet returns a new Wallet
func NewWallet(networkID uint32, chainID ids.ID, txFee uint64) Wallet {
	return Wallet{
		networkID: networkID,
		chainID:   chainID,
		keyChain:  &spdagvm.KeyChain{},
		utxoSet:   &UtxoSet{},
		txFee:     txFee,
	}
}

// GetAddress returns one of the addresses this wallet manages. If no address
// exists, one will be created.
func (w *Wallet) GetAddress() ids.ShortID {
	if w.keyChain.Addrs.Len() == 0 {
		return w.CreateAddress()
	}
	return w.keyChain.Addrs.CappedList(1)[0]
}

// CreateAddress returns a new address.
// It also saves the address and the private key that controls it
// so the address can be used later
func (w *Wallet) CreateAddress() ids.ShortID {
	privKey, _ := w.keyChain.New()
	return privKey.PublicKey().Address()
}

// ImportKey imports a private key into this wallet
func (w *Wallet) ImportKey(sk *crypto.PrivateKeySECP256K1R) { w.keyChain.Add(sk) }

// AddUtxo adds a new UTXO to this wallet if this wallet may spend it
// The UTXO's output must be an OutputPayment
func (w *Wallet) AddUtxo(utxo *spdagvm.UTXO) {
	out, ok := utxo.Out().(*spdagvm.OutputPayment)
	if !ok {
		return
	}

	if _, _, err := w.keyChain.Spend(utxo, math.MaxUint64); err == nil {
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
	if w.keyChain.Addrs.Len() < 1000 {
		changeAddr = w.CreateAddress()
	} else {
		changeAddr = w.GetAddress()
	}

	utxoList := w.utxoSet.Utxos // List of UTXOs this wallet may spend

	destAddrs := []ids.ShortID{destAddr}

	// Build the transaction
	tx, err := builder.NewTxFromUTXOs(w.keyChain, utxoList, amount, w.txFee, locktime, 1, destAddrs, changeAddr, currentTime)
	if err != nil {
		panic(err)
	}

	// Remove from [w.utxoSet] any UTXOs used to fund [tx]
	for _, in := range tx.Ins() {
		if in, ok := in.(*spdagvm.InputPayment); ok {
			inUtxoID := in.InputID()
			w.utxoSet.Remove(inUtxoID)
			w.balance -= in.Amount() // Deduct from [w.balance] the amount sent
		}
	}

	return tx
}

func (w Wallet) String() string {
	return fmt.Sprintf(
		"KeyChain:\n"+
			"%s\n"+
			"UtxoSet:\n"+
			"%s",
		w.keyChain.PrefixedString("    "),
		w.utxoSet.string("    "))
}
