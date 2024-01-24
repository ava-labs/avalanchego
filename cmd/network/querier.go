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
	"github.com/spf13/viper"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
)

var errNoPeers = errors.New("no peers")

type networkQuerier struct {
	log                logging.Logger
	networkID          uint32
	chainID            ids.ID
	deadline           time.Duration
	creator            message.Creator
	concurrency        int
	queryType          string
	outboundMsg        message.OutboundMessage
	expectedResponseOp message.Op
	outputFilePath     string
}

func newQuerierFromViper(v *viper.Viper) (*networkQuerier, error) {
	chainIDStr := v.GetString(ChainIDKey)
	chainID, err := ids.FromString(chainIDStr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse chainID: %w", err)
	}
	creator, err := message.NewCreator(logging.NoLog{}, prometheus.NewRegistry(), "", compression.TypeNone, v.GetDuration(DeadlineKey))
	if err != nil {
		return nil, err
	}
	deadline := v.GetDuration(DeadlineKey)

	// This blockID is the hardcoded blockID accepted on the C-Chain at the given height.
	blockIDBytes := common.Hex2Bytes("ca6fe4a31c0745c84a953f5d942013c2eb16f8f03d4e5b81b6e627fafbffc13e")
	blockID, err := ids.ToID(blockIDBytes)
	if err != nil {
		return nil, err
	}
	blockHeight := uint64(39896056)
	outboundMsg, err := creator.PullQuery(
		chainID,
		99,
		deadline,
		blockID,
		blockHeight,
		p2p.EngineType_ENGINE_TYPE_SNOWMAN,
	)
	if err != nil {
		return nil, err
	}
	expectedResponseOp := message.ChitsOp

	log := logging.NewLogger(
		"network-querier",
		logging.NewWrappedCore(
			logging.Info,
			os.Stdout,
			logging.Colors.ConsoleEncoder(),
		),
	)

	return &networkQuerier{
		log:                log,
		networkID:          v.GetUint32(NetworkIDKey),
		chainID:            chainID,
		deadline:           deadline,
		creator:            creator,
		concurrency:        v.GetInt(ConcurrencyKey),
		queryType:          v.GetString(QueryTypeKey),
		outboundMsg:        outboundMsg,
		expectedResponseOp: expectedResponseOp,
		outputFilePath:     v.GetString(OutputFileKey),
	}, nil
}

func (n *networkQuerier) sendQuery(
	ctx context.Context,
	peerIP ips.IPPort,
	outboundMsg message.OutboundMessage,
	expectedResponseOp message.Op,
) (interface{}, error) {
	var (
		responseCh = make(chan interface{}, 1)
		sendOnce   sync.Once
	)
	p, err := peer.StartTestPeer(
		ctx,
		peerIP,
		n.networkID,
		router.InboundHandlerFunc(func(_ context.Context, msg message.InboundMessage) {
			if msg.Op() != expectedResponseOp {
				n.log.Info("dropping msg from peer",
					zap.Stringer("peerIP", peerIP),
					zap.Stringer("op", msg.Op()),
				)
				return
			}

			sendOnce.Do(func() {
				defer close(responseCh)

				responseCh <- msg.Message()
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

	if !p.Send(ctx, outboundMsg) {
		return nil, errors.New("failed to send pull query to peer")
	}

	select {
	case res := <-responseCh:
		return res, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (n *networkQuerier) queryPeers(ctx context.Context, nodes []node) error {
	if len(nodes) == 0 {
		return errNoPeers
	}

	n.log.Info(
		"Sending query to peers",
		zap.String("queryType", n.queryType),
		zap.Int("numPeers", len(nodes)),
	)

	responses := make([]interface{}, len(nodes))
	eg := errgroup.Group{}
	eg.SetLimit(n.concurrency)

	for i, node := range nodes {
		i := i
		node := node
		eg.Go(func() error {
			ctx, cancel := context.WithTimeout(ctx, n.deadline)
			defer cancel()

			chits, err := n.sendQuery(
				ctx,
				node.ip,
				n.outboundMsg,
				n.expectedResponseOp,
			)
			if err != nil {
				// Note: ignore errors instead of interrupting other queries
				n.log.Debug("failed to get response from peer",
					zap.Stringer("peer", &node),
					zap.Error(err),
				)
				return nil
			}
			responses[i] = chits
			return nil
		})
	}
	if err := eg.Wait(); err != nil {
		return err
	}

	writer := io.Writer(n.log)
	if len(n.outputFilePath) != 0 {
		f, err := os.Create(os.ExpandEnv(n.outputFilePath))
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

	if err := csvWriter.Write([]string{"NodeID", "NodeIP", "Weight", "Response"}); err != nil {
		return err
	}
	for i, response := range responses {
		csvWriter.Write([]string{
			nodes[i].nodeID.String(),
			nodes[i].ip.String(),
			fmt.Sprintf("%d", nodes[i].weight),
			fmt.Sprintf("%v", response),
		})
	}

	return nil
}
