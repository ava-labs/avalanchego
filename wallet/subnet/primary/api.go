// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package primary

import (
	"context"

	"github.com/ava-labs/avalanchego/api/info"
	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/utils/rpc"
	"github.com/ava-labs/avalanchego/vms/avm"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm"
	"github.com/ava-labs/avalanchego/wallet/chain/p"
	"github.com/ava-labs/avalanchego/wallet/chain/x"
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
		addrs []ids.ShortID,
		sourceChain string,
		limit uint32,
		startAddress ids.ShortID,
		startUTXOID ids.ID,
		options ...rpc.Option,
	) ([][]byte, ids.ShortID, ids.ID, error)
}

func FetchState(ctx context.Context, uri string, addrs ids.ShortSet) (p.Context, x.Context, UTXOs, error) {
	infoClient := info.NewClient(uri)
	networkID, err := infoClient.GetNetworkID(ctx)
	if err != nil {
		return nil, nil, nil, err
	}
	xClient := avm.NewClient(uri, "X", networkID)

	pCTX, err := p.NewContextFromClients(ctx, infoClient, xClient)
	if err != nil {
		return nil, nil, nil, err
	}

	xCTX, err := x.NewContextFromClients(ctx, infoClient, xClient)
	if err != nil {
		return nil, nil, nil, err
	}

	utxos := NewUTXOs()
	chains := []struct {
		id     ids.ID
		client UTXOClient
		codec  codec.Manager
	}{
		{
			id:     constants.PlatformChainID,
			client: platformvm.NewClient(uri, networkID),
			codec:  platformvm.Codec,
		},
		{
			id:     xCTX.BlockchainID(),
			client: xClient,
			codec:  x.Codec,
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
				addrs.List(),
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
	addrs []ids.ShortID,
) error {
	var (
		sourceChainIDStr = sourceChainID.String()
		startAddr        ids.ShortID
		startUTXO        ids.ID
	)
	for {
		utxosBytes, endAddr, endUTXO, err := client.GetAtomicUTXOs(
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

		startAddr = endAddr
		startUTXO = endUTXO
	}
	return nil
}
