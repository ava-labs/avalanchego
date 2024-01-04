// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/ava-labs/avalanchego/api/info"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/network/peer"
	"github.com/ava-labs/avalanchego/proto/pb/p2p"
	"github.com/ava-labs/avalanchego/snow/networking/router"
	"github.com/ava-labs/avalanchego/utils/compression"
	"github.com/ava-labs/avalanchego/utils/ips"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ethereum/go-ethereum/common"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	"golang.org/x/sync/errgroup"
)

// TODO
// - add mapping from query to response type to support more query types
// - add ability to specify parameters to query type
// - improve Chits String() function (may need an intermediate type since it currently uses proto)

func main() {
	if err := run(); err != nil {
		fmt.Printf("failed due to %s\n", err)
		os.Exit(1)
	}
}

func run() error {
	v, err := BuildViper(os.Args[1:])
	if errors.Is(err, pflag.ErrHelp) {
		os.Exit(0)
	}
	if err != nil {
		fmt.Printf("failed to build config: %s\n", err)
		os.Exit(1)
	}

	return queryPeers(context.Background(), v)
}

func getPeers(ctx context.Context, v *viper.Viper) ([]ips.IPPort, error) {
	var ipsSlice []ips.IPPort

	switch {
	case v.IsSet(IPPortKey):
		ipStrSlice := v.GetStringSlice(IPPortKey)
		ipsSlice = make([]ips.IPPort, 0, len(ipStrSlice))
		for _, ipStr := range ipStrSlice {
			ip, err := ips.ToIPPort(ipStr)
			if err != nil {
				return nil, err
			}
			ipsSlice = append(ipsSlice, ip)
		}
	default:
		uri := v.GetString(UriKey)
		infoClient := info.NewClient(uri)
		peers, err := infoClient.Peers(ctx)
		if err != nil {
			return nil, err
		}
		ipsSlice = make([]ips.IPPort, 0, len(peers))
		for _, peerInfo := range peers {
			ip, err := ips.ToIPPort(peerInfo.PublicIP)
			if err != nil {
				return nil, err
			}
			ipsSlice = append(ipsSlice, ip)
		}
	}

	if limit := v.GetInt(PeerLimitKey); limit > 0 && len(ipsSlice) > limit {
		ipsSlice = ipsSlice[:limit]
	}
	return ipsSlice, nil
}

type networkQuerier struct {
	networkID uint32
	chainID   ids.ID
	deadline  time.Duration
	creator   message.Creator
}

func newNetworkQuerier(networkID uint32, chainID ids.ID, deadline time.Duration) (*networkQuerier, error) {
	creator, err := message.NewCreator(logging.NoLog{}, prometheus.NewRegistry(), "", compression.TypeNone, deadline)
	if err != nil {
		return nil, err
	}

	return &networkQuerier{
		networkID: networkID,
		chainID:   chainID,
		deadline:  deadline,
		creator:   creator,
	}, nil
}

func (n *networkQuerier) pullQuery(
	ctx context.Context,
	peerIP ips.IPPort,
) (*p2p.Chits, error) {
	var (
		chitResponses = make(chan *p2p.Chits, 1)
		sendOnce      sync.Once
	)
	p, err := peer.StartTestPeer(
		ctx,
		peerIP,
		n.networkID,
		router.InboundHandlerFunc(func(_ context.Context, msg message.InboundMessage) {
			fmt.Printf("received message from peer %s\n: %s\n", peerIP, msg)
			res, ok := msg.Message().(*p2p.Chits)
			if !ok {
				fmt.Printf("dropping msg from peer %s type %T\n", peerIP, msg.Message())
				return
			}

			// Write only the first chits response received
			sendOnce.Do(func() {
				defer close(chitResponses)

				chitResponses <- res
			})
		}),
	)
	if err != nil {
		return nil, err
	}
	defer func() {
		p.StartClose()
		_ = p.AwaitClosed(ctx)
	}()

	// TODO: replace hardcoded parameters to PullQuery with the capability to specify the arguments of the query.
	// This blockID is the hardcoded blockID accepted on the C-Chain at the given height.
	blockIDBytes := common.Hex2Bytes("ca6fe4a31c0745c84a953f5d942013c2eb16f8f03d4e5b81b6e627fafbffc13e")
	blockID, err := ids.ToID(blockIDBytes)
	if err != nil {
		return nil, err
	}
	blockHeight := uint64(39896056)
	pullQueryMsg, err := n.creator.PullQuery(
		n.chainID,
		99,
		n.deadline,
		blockID,
		blockHeight,
		p2p.EngineType_ENGINE_TYPE_SNOWMAN,
	)
	if err != nil {
		return nil, err
	}
	if !p.Send(ctx, pullQueryMsg) {
		return nil, errors.New("failed to send pull query to peer")
	}

	select {
	case chits := <-chitResponses:
		return chits, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func queryPeers(ctx context.Context, v *viper.Viper) error {
	peerIPs, err := getPeers(ctx, v)
	if err != nil {
		return err
	}

	fmt.Printf("Sending PullQuery to peers:\n")
	for _, peerIP := range peerIPs {
		fmt.Printf("IP: %s\n", peerIP)
	}
	chainIDStr := v.GetString(ChainIDKey)
	chainID, err := ids.FromString(chainIDStr)
	if err != nil {
		return fmt.Errorf("failed to parse chainID: %w", err)
	}
	querier, err := newNetworkQuerier(
		v.GetUint32(NetworkIDKey),
		chainID,
		v.GetDuration(DeadlineKey),
	)
	if err != nil {
		return err
	}

	chitResponses := make([]*p2p.Chits, len(peerIPs))
	eg := errgroup.Group{}
	eg.SetLimit(v.GetInt(ConcurrencyKey))

	for i, peerIP := range peerIPs {
		i := i
		peerIP := peerIP
		eg.Go(func() error {
			ctx, cancel := context.WithTimeout(ctx, querier.deadline)
			defer cancel()

			chits, err := querier.pullQuery(
				ctx,
				peerIP,
			)
			if err != nil {
				// Note: ignore errors instead of interrupting other queries
				fmt.Printf("failed to get chits from peer %s: %s\n", peerIP, err)
				return nil
			}
			chitResponses[i] = chits
			return nil
		})
	}
	if err := eg.Wait(); err != nil {
		return err
	}

	writer := io.Writer(os.Stdout)
	if v.IsSet(OutputFileKey) {
		f, err := os.Create(os.ExpandEnv(v.GetString(OutputFileKey)))
		if err != nil {
			return err
		}
		defer func() {
			_ = f.Close()
		}()
		writer = io.MultiWriter(writer, f)
	}
	csvWriter := csv.NewWriter(writer)
	defer csvWriter.Flush()

	if err := csvWriter.Write([]string{"NodeIP", "Chits"}); err != nil {
		return err
	}
	for i, chits := range chitResponses {
		csvWriter.Write([]string{
			peerIPs[i].String(),
			fmt.Sprintf("%v", chits),
		})
	}
	return nil
}
