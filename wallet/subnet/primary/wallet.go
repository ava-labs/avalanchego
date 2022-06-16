// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package primary

import (
	"context"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/avm"
	"github.com/ava-labs/avalanchego/vms/platformvm"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/wallet/chain/p"
	"github.com/ava-labs/avalanchego/wallet/chain/x"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary/common"
)

var _ Wallet = &wallet{}

// Wallet provides chain wallets for the primary network.
type Wallet interface {
	P() p.Wallet
	X() x.Wallet
}

type wallet struct {
	p p.Wallet
	x x.Wallet
}

func (w *wallet) P() p.Wallet { return w.p }
func (w *wallet) X() x.Wallet { return w.x }

// NewWalletFromURI returns a wallet that supports issuing transactions to the
// chains living in the primary network to a provided [uri].
//
// On creation, the wallet attaches to the provided [uri] and fetches all UTXOs
// that reference any of the keys contained in [kc]. If the UTXOs are modified
// through an external issuance process, such as another instance of the wallet,
// the UTXOs may become out of sync.
//
// The wallet manages all UTXOs locally, and performs all tx signing locally.
func NewWalletFromURI(ctx context.Context, uri string, kc *secp256k1fx.Keychain) (Wallet, error) {
	pCTX, xCTX, utxos, err := FetchState(ctx, uri, kc.Addrs)
	if err != nil {
		return nil, err
	}
	return NewWalletWithState(uri, pCTX, xCTX, utxos, kc), nil
}

func NewWalletWithState(
	uri string,
	pCTX p.Context,
	xCTX x.Context,
	utxos UTXOs,
	kc *secp256k1fx.Keychain,
) Wallet {
	pUTXOs := NewChainUTXOs(constants.PlatformChainID, utxos)
	pTXs := make(map[ids.ID]*txs.Tx)
	pBackend := p.NewBackend(pCTX, pUTXOs, pTXs)
	pBuilder := p.NewBuilder(kc.Addrs, pBackend)
	pSigner := p.NewSigner(kc, pBackend)
	pClient := platformvm.NewClient(uri)

	xChainID := xCTX.BlockchainID()
	xUTXOs := NewChainUTXOs(xChainID, utxos)
	xBackend := x.NewBackend(xCTX, xChainID, xUTXOs)
	xBuilder := x.NewBuilder(kc.Addrs, xBackend)
	xSigner := x.NewSigner(kc, xBackend)
	xClient := avm.NewClient(uri, "X")

	return NewWallet(
		p.NewWallet(pBuilder, pSigner, pClient, pBackend),
		x.NewWallet(xBuilder, xSigner, xClient, xBackend),
	)
}

func NewWalletWithOptions(w Wallet, options ...common.Option) Wallet {
	return NewWallet(
		p.NewWalletWithOptions(w.P(), options...),
		x.NewWalletWithOptions(w.X(), options...),
	)
}

func NewWallet(p p.Wallet, x x.Wallet) Wallet {
	return &wallet{
		p: p,
		x: x,
	}
}
