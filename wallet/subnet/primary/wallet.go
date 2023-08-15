// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package primary

import (
	"context"

	"github.com/ava-labs/coreth/plugin/evm"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/keychain"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/avm"
	"github.com/ava-labs/avalanchego/vms/platformvm"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/wallet/chain/c"
	"github.com/ava-labs/avalanchego/wallet/chain/p"
	"github.com/ava-labs/avalanchego/wallet/chain/x"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary/common"
)

var _ Wallet = (*wallet)(nil)

// Wallet provides chain wallets for the primary network.
type Wallet interface {
	P() p.Wallet
	X() x.Wallet
	C() c.Wallet
}

type wallet struct {
	p p.Wallet
	x x.Wallet
	c c.Wallet
}

func (w *wallet) P() p.Wallet {
	return w.p
}

func (w *wallet) X() x.Wallet {
	return w.x
}

func (w *wallet) C() c.Wallet {
	return w.c
}

// Creates a new default wallet
func NewWallet(p p.Wallet, x x.Wallet, c c.Wallet) Wallet {
	return &wallet{
		p: p,
		x: x,
		c: c,
	}
}

// Creates a Wallet with the given set of options
func NewWalletWithOptions(w Wallet, options ...common.Option) Wallet {
	return NewWallet(
		p.NewWalletWithOptions(w.P(), options...),
		x.NewWalletWithOptions(w.X(), options...),
		c.NewWalletWithOptions(w.C(), options...),
	)
}

type WalletConfig struct {
	// Base URI to use for all node requests.
	URI string // required
	// Keys to use for signing all transactions.
	AVAXKeychain keychain.Keychain // required
	EthKeychain  c.EthKeychain     // required
	// Set of P-chain transactions that the wallet should know about to be able
	// to generate transactions.
	PChainTxs map[ids.ID]*txs.Tx // optional
	// Set of P-chain transactions that the wallet should fetch to be able to
	// generate transactions.
	PChainTxsToFetch set.Set[ids.ID] // optional
}

// MakeWallet returns a wallet that supports issuing transactions to the chains
// living in the primary network.
//
// On creation, the wallet attaches to the provided uri and fetches all UTXOs
// that reference any of the provided keys. If the UTXOs are modified through an
// external issuance process, such as another instance of the wallet, the UTXOs
// may become out of sync. The wallet will also fetch all requested P-chain
// transactions.
//
// The wallet manages all state locally, and performs all tx signing locally.
func MakeWallet(ctx context.Context, config *WalletConfig) (Wallet, error) {
	avaxAddrs := config.AVAXKeychain.Addresses()
	pCTX, xCTX, cCTX, utxos, err := FetchState(ctx, config.URI, avaxAddrs)
	if err != nil {
		return nil, err
	}

	ethAddrs := config.EthKeychain.EthAddresses()
	accounts, err := FetchEthState(ctx, config.URI, ethAddrs)
	if err != nil {
		return nil, err
	}

	pChainTxs := config.PChainTxs
	if pChainTxs == nil {
		pChainTxs = make(map[ids.ID]*txs.Tx)
	}

	pClient := platformvm.NewClient(config.URI)
	for txID := range config.PChainTxsToFetch {
		txBytes, err := pClient.GetTx(ctx, txID)
		if err != nil {
			return nil, err
		}
		tx, err := txs.Parse(txs.Codec, txBytes)
		if err != nil {
			return nil, err
		}
		pChainTxs[txID] = tx
	}

	pUTXOs := NewChainUTXOs(constants.PlatformChainID, utxos)
	pBackend := p.NewBackend(pCTX, pUTXOs, pChainTxs)
	pBuilder := p.NewBuilder(avaxAddrs, pBackend)
	pSigner := p.NewSigner(config.AVAXKeychain, pBackend)

	xChainID := xCTX.BlockchainID()
	xUTXOs := NewChainUTXOs(xChainID, utxos)
	xBackend := x.NewBackend(xCTX, xUTXOs)
	xBuilder := x.NewBuilder(avaxAddrs, xBackend)
	xSigner := x.NewSigner(config.AVAXKeychain, xBackend)
	xClient := avm.NewClient(config.URI, "X")

	cChainID := cCTX.BlockchainID()
	cUTXOs := NewChainUTXOs(cChainID, utxos)
	cBackend := c.NewBackend(cCTX, cUTXOs, accounts)
	cBuilder := c.NewBuilder(avaxAddrs, ethAddrs, cBackend)
	cSigner := c.NewSigner(config.AVAXKeychain, config.EthKeychain, cBackend)
	cClient := evm.NewCChainClient(config.URI)

	return NewWallet(
		p.NewWallet(pBuilder, pSigner, pClient, pBackend),
		x.NewWallet(xBuilder, xSigner, xClient, xBackend),
		c.NewWallet(cBuilder, cSigner, cClient, cBackend),
	), nil
}
