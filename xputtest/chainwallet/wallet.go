// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chainwallet

import (
	"errors"
	"fmt"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow"
	"github.com/ava-labs/gecko/utils/crypto"
	"github.com/ava-labs/gecko/vms/spchainvm"
)

// Wallet is a holder for keys and UTXOs.
type Wallet struct {
	networkID  uint32
	chainID    ids.ID
	keychain   *spchainvm.Keychain            // Mapping from public address to the SigningKeys
	accountSet map[[20]byte]spchainvm.Account // Mapping from addresses to accounts
	balance    uint64

	txs []*spchainvm.Tx
}

// NewWallet ...
func NewWallet(networkID uint32, chainID ids.ID) *Wallet {
	return &Wallet{
		networkID:  networkID,
		chainID:    chainID,
		keychain:   spchainvm.NewKeychain(networkID, chainID),
		accountSet: make(map[[20]byte]spchainvm.Account),
	}
}

// CreateAddress returns a brand new address! Ready to receive funds!
func (w *Wallet) CreateAddress() (ids.ShortID, error) {
	sk, err := w.keychain.New()
	if err != nil {
		return ids.ShortID{}, err
	}
	return sk.PublicKey().Address(), nil
}

// ImportKey imports a private key into this wallet
func (w *Wallet) ImportKey(sk *crypto.PrivateKeySECP256K1R) { w.keychain.Add(sk) }

// AddAccount adds a new account to this wallet, if this wallet can spend it.
func (w *Wallet) AddAccount(account spchainvm.Account) {
	if account.Balance() > 0 {
		w.accountSet[account.ID().Key()] = account
		w.balance += account.Balance()
	}
}

// Balance returns the amount of the assets in this wallet
func (w *Wallet) Balance() uint64 { return w.balance }

// GenerateTxs generates the transactions that will be sent
// during the test
// Generate them all on test initialization so tx generation is not bottleneck
// in testing
func (w *Wallet) GenerateTxs(numTxs int) error {
	ctx := snow.DefaultContextTest()
	ctx.NetworkID = w.networkID
	ctx.ChainID = w.chainID

	w.txs = make([]*spchainvm.Tx, numTxs)
	for i := range w.txs {
		tx, err := w.MakeTx()
		if err != nil {
			return err
		}
		w.txs[i] = tx
	}
	return nil
}

// NextTx returns the next tx to be sent as part of xput test
func (w *Wallet) NextTx() *spchainvm.Tx {
	if len(w.txs) == 0 {
		return nil
	}
	tx := w.txs[0]
	w.txs = w.txs[1:]
	return tx
}

// MakeTx creates a new transaction and update the state to after the tx is accepted
func (w *Wallet) MakeTx() (*spchainvm.Tx, error) {
	ctx := snow.DefaultContextTest()
	ctx.NetworkID = w.networkID
	ctx.ChainID = w.chainID

	for _, account := range w.accountSet {
		accountID := account.ID()
		key, exists := w.keychain.Get(accountID)
		if !exists {
			return nil, errors.New("missing account")
		}

		amount := uint64(1)
		tx, sendAccount, err := account.CreateTx(amount, accountID, ctx, key)
		if err != nil {
			continue
		}

		newAccount, err := sendAccount.Receive(tx, ctx)
		if err != nil {
			return nil, err
		}
		w.accountSet[accountID.Key()] = newAccount
		return tx, nil
	}
	return nil, errors.New("empty")
}

func (w Wallet) String() string {
	return fmt.Sprintf(
		"Keychain:\n"+
			"%s",
		w.keychain.PrefixedString("    "))
}
