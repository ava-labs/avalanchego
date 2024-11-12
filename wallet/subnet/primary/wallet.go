// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package primary

import (
	"context"

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

var _ Wallet = (*wallet)(nil)

// Wallet provides chain wallets for the primary network.
type Wallet interface {
	P() pwallet.Wallet
	X() x.Wallet
	C() c.Wallet
}

type wallet struct {
	p pwallet.Wallet
	x x.Wallet
	c c.Wallet
}

func (w *wallet) P() pwallet.Wallet {
	return w.p
}

func (w *wallet) X() x.Wallet {
	return w.x
}

func (w *wallet) C() c.Wallet {
	return w.c
}

// Creates a new default wallet
func NewWallet(p pwallet.Wallet, x x.Wallet, c c.Wallet) Wallet {
	return &wallet{
		p: p,
		x: x,
		c: c,
	}
}

// Creates a Wallet with the given set of options
func NewWalletWithOptions(w Wallet, options ...common.Option) Wallet {
	return NewWallet(
		pwallet.WithOptions(w.P(), options...),
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
	// Subnet IDs that the wallet should know about to be able to
	// generate transactions.
	SubnetIDs []ids.ID // optional
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
	avaxState, err := FetchState(ctx, config.URI, avaxAddrs)
	if err != nil {
		return nil, err
	}

	ethAddrs := config.EthKeychain.EthAddresses()
	ethState, err := FetchEthState(ctx, config.URI, ethAddrs)
	if err != nil {
		return nil, err
	}

	subnetOwners, err := platformvm.GetSubnetOwners(avaxState.PClient, ctx, config.SubnetIDs...)
	if err != nil {
		return nil, err
	}
	pUTXOs := common.NewChainUTXOs(constants.PlatformChainID, avaxState.UTXOs)
	pBackend := pwallet.NewBackend(avaxState.PCTX, pUTXOs, subnetOwners)
	pClient := p.NewClient(avaxState.PClient, pBackend)
	pBuilder := pbuilder.New(avaxAddrs, avaxState.PCTX, pBackend)
	pSigner := psigner.New(config.AVAXKeychain, pBackend)

	xChainID := avaxState.XCTX.BlockchainID
	xUTXOs := common.NewChainUTXOs(xChainID, avaxState.UTXOs)
	xBackend := x.NewBackend(avaxState.XCTX, xUTXOs)
	xBuilder := xbuilder.New(avaxAddrs, avaxState.XCTX, xBackend)
	xSigner := xsigner.New(config.AVAXKeychain, xBackend)

	cChainID := avaxState.CCTX.BlockchainID
	cUTXOs := common.NewChainUTXOs(cChainID, avaxState.UTXOs)
	cBackend := c.NewBackend(cUTXOs, ethState.Accounts)
	cBuilder := c.NewBuilder(avaxAddrs, ethAddrs, avaxState.CCTX, cBackend)
	cSigner := c.NewSigner(config.AVAXKeychain, config.EthKeychain, cBackend)

	return NewWallet(
		pwallet.New(pClient, pBuilder, pSigner),
		x.NewWallet(xBuilder, xSigner, avaxState.XClient, xBackend),
		c.NewWallet(cBuilder, cSigner, avaxState.CClient, ethState.Client, cBackend),
	), nil
}
