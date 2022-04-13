// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
//
// This file is a derived work, based on ava-labs code whose
// original notices appear below.
//
// It is distributed under the same license conditions as the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********************************************************

// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package primary

import (
	"context"

	"github.com/chain4travel/caminogo/ids"
	"github.com/chain4travel/caminogo/utils/constants"
	"github.com/chain4travel/caminogo/vms/avm"
	"github.com/chain4travel/caminogo/vms/platformvm"
	"github.com/chain4travel/caminogo/vms/secp256k1fx"
	"github.com/chain4travel/caminogo/wallet/chain/p"
	"github.com/chain4travel/caminogo/wallet/chain/x"
	"github.com/chain4travel/caminogo/wallet/subnet/primary/common"
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
	pTXs := make(map[ids.ID]*platformvm.Tx)
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
