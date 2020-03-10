// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chainwallet

import (
	"fmt"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow"
	"github.com/ava-labs/gecko/utils/crypto"
	"github.com/ava-labs/gecko/vms/spchainvm"
)

// The max number of transactions this wallet can send as part of the throughput tests
// lower --> low startup time but test has shorter duration
// higher --> high startup time but test has longer duration
const (
	MaxNumTxs = 25000
)

// Wallet is a holder for keys and UTXOs.
type Wallet struct {
	networkID  uint32
	chainID    ids.ID
	keyChain   *spchainvm.KeyChain            // Mapping from public address to the SigningKeys
	accountSet map[[20]byte]spchainvm.Account // Mapping from addresses to accounts
	balance    uint64
	TxsSent    int32
	txs        [MaxNumTxs]*spchainvm.Tx
}

// NewWallet ...
func NewWallet(networkID uint32, chainID ids.ID) Wallet {
	return Wallet{
		networkID:  networkID,
		chainID:    chainID,
		keyChain:   spchainvm.NewKeyChain(networkID, chainID),
		accountSet: make(map[[20]byte]spchainvm.Account),
	}
}

// CreateAddress returns a brand new address! Ready to receive funds!
func (w *Wallet) CreateAddress() ids.ShortID { return w.keyChain.New().PublicKey().Address() }

// ImportKey imports a private key into this wallet
func (w *Wallet) ImportKey(sk *crypto.PrivateKeySECP256K1R) { w.keyChain.Add(sk) }

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
func (w *Wallet) GenerateTxs() {
	ctx := snow.DefaultContextTest()
	ctx.NetworkID = w.networkID
	ctx.ChainID = w.chainID

	for i := 0; i < MaxNumTxs; i++ {
		if i%1000 == 0 {
			fmt.Printf("generated %d transactions\n", i)
		}
		for _, account := range w.accountSet {
			accountID := account.ID()
			if key, exists := w.keyChain.Get(accountID); exists {
				amount := uint64(1)
				if tx, sendAccount, err := account.CreateTx(amount, accountID, ctx, key); err == nil {
					newAccount, err := sendAccount.Receive(tx, ctx)
					if err != nil {
						panic("shouldn't error")
					}
					w.accountSet[accountID.Key()] = newAccount
					w.txs[i] = tx
					continue
				} else {
					panic("shouldn't error here either: " + err.Error())
				}
			} else {
				panic("shouldn't not exist")
			}
		}
	}
}

/*
// Send a new transaction
func (w *Wallet) Send() *spchainvm.Tx {
	ctx := snow.DefaultContextTest()
	ctx.NetworkID = w.networkID
	ctx.ChainID = w.chainID

	for _, account := range w.accountSet {
		accountID := account.ID()
		if key, exists := w.keyChain.Get(accountID); exists {
			amount := uint64(1)
			if tx, sendAccount, err := account.CreateTx(amount, accountID, ctx, key); err == nil {
				newAccount, err := sendAccount.Receive(tx, ctx)
				if err == nil {
					w.accountSet[accountID.Key()] = newAccount
					return tx
				}
			}
		}
	}
	return nil
}
*/

// NextTx returns the next tx to be sent as part of xput test
func (w *Wallet) NextTx() *spchainvm.Tx {
	if w.TxsSent >= MaxNumTxs {
		return nil
	}
	w.TxsSent++
	return w.txs[w.TxsSent-1]
}

func (w Wallet) String() string {
	return fmt.Sprintf(
		"KeyChain:\n"+
			"%s",
		w.keyChain.PrefixedString("    "))
}
