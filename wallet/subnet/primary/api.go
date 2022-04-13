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

	"github.com/chain4travel/caminogo/api"
	"github.com/chain4travel/caminogo/api/info"
	"github.com/chain4travel/caminogo/codec"
	"github.com/chain4travel/caminogo/ids"
	"github.com/chain4travel/caminogo/utils/constants"
	"github.com/chain4travel/caminogo/utils/formatting"
	"github.com/chain4travel/caminogo/vms/avm"
	"github.com/chain4travel/caminogo/vms/components/avax"
	"github.com/chain4travel/caminogo/vms/platformvm"
	"github.com/chain4travel/caminogo/wallet/chain/p"
	"github.com/chain4travel/caminogo/wallet/chain/x"
)

const (
	MainnetAPIURI = "https://api.avax.network"
	FujiAPIURI    = "https://api.avax-test.network"
	LocalAPIURI   = "http://localhost:9650"

	fetchLimit = 1024
)

// TODO: refactor UTXOClient definition to allow the client implementations to
//       perform their own assertions.
var (
	_ UTXOClient = platformvm.Client(nil)
	_ UTXOClient = avm.Client(nil)
)

type UTXOClient interface {
	GetAtomicUTXOs(
		ctx context.Context,
		addrs []string,
		sourceChain string,
		limit uint32,
		startAddress,
		startUTXOID string,
	) ([][]byte, api.Index, error)
}

func FetchState(ctx context.Context, uri string, addrs ids.ShortSet) (p.Context, x.Context, UTXOs, error) {
	infoClient := info.NewClient(uri)
	xClient := avm.NewClient(uri, "X")

	pCTX, err := p.NewContextFromClients(ctx, infoClient, xClient)
	if err != nil {
		return nil, nil, nil, err
	}
	pAddrs, err := FormatAddresses("P", pCTX.HRP(), addrs)
	if err != nil {
		return nil, nil, nil, err
	}

	xCTX, err := x.NewContextFromClients(ctx, infoClient, xClient)
	if err != nil {
		return nil, nil, nil, err
	}
	xAddrs, err := FormatAddresses("X", xCTX.HRP(), addrs)
	if err != nil {
		return nil, nil, nil, err
	}

	utxos := NewUTXOs()
	chains := []struct {
		id     ids.ID
		client UTXOClient
		codec  codec.Manager
		addrs  []string
	}{
		{
			id:     constants.PlatformChainID,
			client: platformvm.NewClient(uri),
			codec:  platformvm.Codec,
			addrs:  pAddrs,
		},
		{
			id:     xCTX.BlockchainID(),
			client: xClient,
			codec:  x.Codec,
			addrs:  xAddrs,
		},
	}
	for _, destinationChain := range chains {
		for _, sourceChain := range chains {
			err = AddAllUTXOs(
				ctx,
				utxos,
				destinationChain.client,
				destinationChain.codec,
				sourceChain.id,
				destinationChain.id,
				destinationChain.addrs,
			)
			if err != nil {
				return nil, nil, nil, err
			}
		}
	}
	return pCTX, xCTX, utxos, nil
}

// FormatAddresses returns the string format of the provided address set for the
// requested chain and hrp. This is useful to use with the API clients to
// support address queries.
func FormatAddresses(chain, hrp string, addrSet ids.ShortSet) ([]string, error) {
	addrs := make([]string, 0, addrSet.Len())
	for addr := range addrSet {
		addrStr, err := formatting.FormatAddress(chain, hrp, addr[:])
		if err != nil {
			return nil, err
		}
		addrs = append(addrs, addrStr)
	}
	return addrs, nil
}

// AddAllUTXOs fetches all the UTXOs referenced by [addresses] that were sent
// from [sourceChainID] to [destinationChainID] from the [client]. It then uses
// [codec] to parse the returned UTXOs and it adds them into [utxos]. If [ctx]
// expires, then the returned error will be immediately reported.
func AddAllUTXOs(
	ctx context.Context,
	utxos UTXOs,
	client UTXOClient,
	codec codec.Manager,
	sourceChainID ids.ID,
	destinationChainID ids.ID,
	addrs []string,
) error {
	var (
		sourceChainIDStr = sourceChainID.String()
		startAddr        string
		startUTXO        string
	)
	for {
		utxosBytes, index, err := client.GetAtomicUTXOs(
			ctx,
			addrs,
			sourceChainIDStr,
			fetchLimit,
			startAddr,
			startUTXO,
		)
		if err != nil {
			return err
		}

		for _, utxoBytes := range utxosBytes {
			var utxo avax.UTXO
			_, err := codec.Unmarshal(utxoBytes, &utxo)
			if err != nil {
				return err
			}

			if err := utxos.AddUTXO(ctx, sourceChainID, destinationChainID, &utxo); err != nil {
				return err
			}
		}

		if len(utxosBytes) < fetchLimit {
			break
		}

		startAddr = index.Address
		startUTXO = index.UTXO
	}
	return nil
}
