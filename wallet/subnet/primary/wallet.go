// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package primary

import (
	"context"
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/keychain"
	"github.com/ava-labs/avalanchego/vms/platformvm"
	"github.com/ava-labs/avalanchego/wallet/chain/c"
	"github.com/ava-labs/avalanchego/wallet/chain/p"
	"github.com/ava-labs/avalanchego/wallet/chain/x"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary/common"

	pbuilder "github.com/ava-labs/avalanchego/wallet/chain/p/builder"
	psigner "github.com/ava-labs/avalanchego/wallet/chain/p/signer"
	pwallet "github.com/ava-labs/avalanchego/wallet/chain/p/wallet"
	xbuilder "github.com/ava-labs/avalanchego/wallet/chain/x/builder"
	xsigner "github.com/ava-labs/avalanchego/wallet/chain/x/signer"
)

// Wallet provides chain wallets for the primary network.
type Wallet struct {
	p pwallet.Wallet
	x x.Wallet
	c c.Wallet
}

func (w *Wallet) P() pwallet.Wallet {
	return w.p
}

func (w *Wallet) X() x.Wallet {
	return w.x
}

func (w *Wallet) C() c.Wallet {
	return w.c
}

// Creates a new default wallet
func NewWallet(p pwallet.Wallet, x x.Wallet, c c.Wallet) *Wallet {
	return &Wallet{
		p: p,
		x: x,
		c: c,
	}
}

// Creates a Wallet with the given set of options
func NewWalletWithOptions(w *Wallet, options ...common.Option) *Wallet {
	return NewWallet(
		pwallet.WithOptions(w.p, options...),
		x.NewWalletWithOptions(w.x, options...),
		c.NewWalletWithOptions(w.c, options...),
	)
}

type WalletConfig struct {
	// Subnet IDs that the wallet should know about to be able to generate
	// transactions.
	SubnetIDs []ids.ID // optional
	// Validation IDs that the wallet should know about to be able to generate
	// transactions.
	ValidationIDs []ids.ID // optional
}

// MakeWallet returns a wallet that supports issuing transactions to the chains
// living in the primary network.
//
// On creation, the wallet attaches to the provided uri and fetches all UTXOs
// that reference any of the provided keys. If the UTXOs are modified through an
// external issuance process, such as another instance of the wallet, the UTXOs
// may become out of sync. The wallet will also fetch all requested P-chain
// owners.
//
// The wallet manages all state locally, and performs all tx signing locally.
func MakeWallet(
	ctx context.Context,
	uri string,
	avaxKeychain keychain.Keychain,
	ethKeychain c.EthKeychain,
	config WalletConfig,
) (*Wallet, error) {
	avaxAddrs := avaxKeychain.Addresses()
	avaxState, err := FetchState(ctx, uri, avaxAddrs)
	if err != nil {
		return nil, fmt.Errorf("fetching avax state: %w", err)
	}

	ethAddrs := ethKeychain.EthAddresses()
	ethState, err := FetchEthState(ctx, uri, ethAddrs)
	if err != nil {
		return nil, fmt.Errorf("fetching eth state: %w", err)
	}

	owners, err := platformvm.GetOwners(avaxState.PClient, ctx, config.SubnetIDs, config.ValidationIDs)
	if err != nil {
		return nil, fmt.Errorf("fetching p-chain owners: %w", err)
	}

	pUTXOs := common.NewChainUTXOs(constants.PlatformChainID, avaxState.UTXOs)
	pBackend := pwallet.NewBackend(pUTXOs, owners)
	pClient := p.NewClient(avaxState.PClient, pBackend)
	pBuilder := pbuilder.New(avaxAddrs, avaxState.PCTX, pBackend)
	pSigner := psigner.New(avaxKeychain, pBackend)

	xChainID := avaxState.XCTX.BlockchainID
	xUTXOs := common.NewChainUTXOs(xChainID, avaxState.UTXOs)
	xBackend := x.NewBackend(avaxState.XCTX, xUTXOs)
	xBuilder := xbuilder.New(avaxAddrs, avaxState.XCTX, xBackend)
	xSigner := xsigner.New(avaxKeychain, xBackend)

	cChainID := avaxState.CCTX.BlockchainID
	cUTXOs := common.NewChainUTXOs(cChainID, avaxState.UTXOs)
	cBackend := c.NewBackend(cUTXOs, ethState.Accounts)
	cBuilder := c.NewBuilder(avaxAddrs, ethAddrs, avaxState.CCTX, cBackend)
	cSigner := c.NewSigner(avaxKeychain, ethKeychain, cBackend)

	return NewWallet(
		pwallet.New(pClient, pBuilder, pSigner),
		x.NewWallet(xBuilder, xSigner, avaxState.XClient, xBackend),
		c.NewWallet(cBuilder, cSigner, avaxState.CClient, ethState.Client, cBackend),
	), nil
}

// MakePWallet returns a P-chain wallet that supports issuing transactions.
//
// On creation, the wallet attaches to the provided uri and fetches all UTXOs
// that reference any of the provided keys. If the UTXOs are modified through an
// external issuance process, such as another instance of the wallet, the UTXOs
// may become out of sync. The wallet will also fetch all requested P-chain
// owners.
//
// The wallet manages all state locally, and performs all tx signing locally.
func MakePWallet(
	ctx context.Context,
	uri string,
	keychain keychain.Keychain,
	config WalletConfig,
) (pwallet.Wallet, error) {
	addrs := keychain.Addresses()
	client, context, utxos, err := FetchPState(ctx, uri, addrs)
	if err != nil {
		return nil, err
	}

	owners, err := platformvm.GetOwners(client, ctx, config.SubnetIDs, config.ValidationIDs)
	if err != nil {
		return nil, err
	}

	pUTXOs := common.NewChainUTXOs(constants.PlatformChainID, utxos)
	pBackend := pwallet.NewBackend(pUTXOs, owners)
	pClient := p.NewClient(client, pBackend)
	pBuilder := pbuilder.New(addrs, context, pBackend)
	pSigner := psigner.New(keychain, pBackend)
	return pwallet.New(pClient, pBuilder, pSigner), nil
}
