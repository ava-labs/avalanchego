// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vms

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"time"

	"connectrpc.com/connect"
	"github.com/onsi/ginkgo/v2"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"golang.org/x/net/http2"
	"golang.org/x/sync/errgroup"

	"github.com/ava-labs/avalanchego/api/connectclient"
	"github.com/ava-labs/avalanchego/connectproto/pb/xsvm"
	"github.com/ava-labs/avalanchego/connectproto/pb/xsvm/xsvmconnect"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/tests/fixture/e2e"
	"github.com/ava-labs/avalanchego/tests/fixture/subnet"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/example/xsvm/api"
	"github.com/ava-labs/avalanchego/vms/example/xsvm/cmd/issue/export"
	"github.com/ava-labs/avalanchego/vms/example/xsvm/cmd/issue/importtx"
	"github.com/ava-labs/avalanchego/vms/example/xsvm/cmd/issue/transfer"
)

const pollingInterval = 50 * time.Millisecond

var (
	subnetAName = "xsvm-a"
	subnetBName = "xsvm-b"
)

func XSVMSubnetsOrPanic(nodes ...*tmpnet.Node) []*tmpnet.Subnet {
	key, err := secp256k1.NewPrivateKey()
	if err != nil {
		panic(err)
	}
	subnetANodes := nodes
	subnetBNodes := nodes
	if len(nodes) > 1 {
		// Validate tmpnet bootstrap of a disjoint validator set
		midpoint := len(nodes) / 2
		subnetANodes = nodes[:midpoint]
		subnetBNodes = nodes[midpoint:]
	}
	return []*tmpnet.Subnet{
		subnet.NewXSVMOrPanic(subnetAName, key, subnetANodes...),
		subnet.NewXSVMOrPanic(subnetBName, key, subnetBNodes...),
	}
}

var _ = ginkgo.Describe("[XSVM]", ginkgo.Label("xsvm"), func() {
	tc := e2e.NewTestContext()
	require := require.New(tc)

	ginkgo.It("should support transfers between subnets", func() {
		network := e2e.GetEnv(tc).GetNetwork()

		sourceSubnet := network.GetSubnet(subnetAName)
		require.NotNil(sourceSubnet)
		destinationSubnet := network.GetSubnet(subnetBName)
		require.NotNil(destinationSubnet)

		sourceChain := sourceSubnet.Chains[0]
		destinationChain := destinationSubnet.Chains[0]

		sourceValidators := getNodesForIDs(network.Nodes, sourceSubnet.ValidatorIDs)
		require.NotEmpty(sourceValidators)
		sourceAPINode := sourceValidators[0]
		sourceAPINodeURI := sourceAPINode.GetAccessibleURI()
		tc.Log().Info("issuing transactions for source subnet",
			zap.String("subnetName", subnetAName),
			zap.Stringer("nodeID", sourceAPINode.NodeID),
			zap.String("nodeURI", sourceAPINodeURI),
		)

		destinationValidators := getNodesForIDs(network.Nodes, destinationSubnet.ValidatorIDs)
		require.NotEmpty(destinationValidators)
		destinationAPINode := destinationValidators[0]
		destinationAPINodeURI := destinationAPINode.GetAccessibleURI()
		tc.Log().Info("issuing transactions for destination subnet",
			zap.String("subnetName", subnetBName),
			zap.Stringer("nodeID", destinationAPINode.NodeID),
			zap.String("nodeURI", destinationAPINodeURI),
		)

		destinationKey := e2e.NewPrivateKey(tc)

		tc.By("checking that the funded key has sufficient funds for the export")
		sourceClient := api.NewClient(sourceAPINodeURI, sourceChain.ChainID.String())
		initialSourcedBalance, err := sourceClient.Balance(
			tc.DefaultContext(),
			sourceChain.PreFundedKey.Address(),
			sourceChain.ChainID,
		)
		require.NoError(err)
		require.GreaterOrEqual(initialSourcedBalance, units.Schmeckle)

		tc.By(fmt.Sprintf("exporting from chain %s on subnet %s", sourceChain.ChainID, sourceSubnet.SubnetID))
		exportTxStatus, err := export.Export(
			tc.DefaultContext(),
			&export.Config{
				URI:                sourceAPINodeURI,
				SourceChainID:      sourceChain.ChainID,
				DestinationChainID: destinationChain.ChainID,
				Amount:             units.Schmeckle,
				To:                 destinationKey.Address(),
				PrivateKey:         sourceChain.PreFundedKey,
			},
		)
		require.NoError(err)
		tc.Log().Info("issued export transaction",
			zap.Stringer("txID", exportTxStatus.TxID),
		)

		tc.By("checking that the export transaction has been accepted on all nodes")
		for _, node := range sourceValidators[1:] {
			uri := node.GetAccessibleURI()
			require.NoError(api.AwaitTxAccepted(
				tc.DefaultContext(),
				api.NewClient(uri, sourceChain.ChainID.String()),
				sourceChain.PreFundedKey.Address(),
				exportTxStatus.Nonce,
				pollingInterval,
			))
		}

		tc.By(fmt.Sprintf("issuing transaction on chain %s on subnet %s to activate snowman++ consensus",
			destinationChain.ChainID, destinationSubnet.SubnetID))
		recipientKey := e2e.NewPrivateKey(tc)
		transferTxStatus, err := transfer.Transfer(
			tc.DefaultContext(),
			&transfer.Config{
				URI:        destinationAPINodeURI,
				ChainID:    destinationChain.ChainID,
				AssetID:    destinationChain.ChainID,
				Amount:     units.Schmeckle,
				To:         recipientKey.Address(),
				PrivateKey: destinationChain.PreFundedKey,
			},
		)
		require.NoError(err)
		tc.Log().Info("issued transfer transaction",
			zap.Stringer("txID", transferTxStatus.TxID),
		)

		tc.By(fmt.Sprintf("importing to blockchain %s on subnet %s", destinationChain.ChainID, destinationSubnet.SubnetID))
		sourceURIs := make([]string, len(sourceValidators))
		for i, node := range sourceValidators {
			sourceURIs[i] = node.GetAccessibleURI()
		}
		importTxStatus, err := importtx.Import(
			tc.DefaultContext(),
			&importtx.Config{
				URI:                destinationAPINodeURI,
				SourceURIs:         sourceURIs,
				SourceChainID:      sourceChain.ChainID.String(),
				DestinationChainID: destinationChain.ChainID.String(),
				TxID:               exportTxStatus.TxID,
				PrivateKey:         destinationKey,
			},
		)
		require.NoError(err)
		tc.Log().Info("issued import transaction",
			zap.Stringer("txID", importTxStatus.TxID),
		)

		tc.By("checking that the balance of the source key has decreased")
		sourceBalance, err := sourceClient.Balance(tc.DefaultContext(), sourceChain.PreFundedKey.Address(), sourceChain.ChainID)
		require.NoError(err)
		require.GreaterOrEqual(initialSourcedBalance-units.Schmeckle, sourceBalance)

		tc.By("checking that the balance of the destination key is non-zero")
		destinationClient := api.NewClient(destinationAPINodeURI, destinationChain.ChainID.String())
		destinationBalance, err := destinationClient.Balance(tc.DefaultContext(), destinationKey.Address(), sourceChain.ChainID)
		require.NoError(err)
		require.Equal(units.Schmeckle, destinationBalance)

		_ = e2e.CheckBootstrapIsPossible(tc, network)
	})

	ginkgo.It("should serve grpc api requests", func() {
		network := e2e.GetEnv(tc).GetNetwork()
		log := tc.Log()
		if network.DefaultRuntimeConfig.Kube != nil {
			ginkgo.Skip("h2c is not currently supported in kube")
		}

		tc.By("establishing connection")
		nodeID := network.GetSubnet(subnetAName).ValidatorIDs[0]
		node, err := network.GetNode(nodeID)
		require.NoError(err)

		httpClient := &http.Client{
			Transport: &http2.Transport{
				AllowHTTP: true,
				DialTLSContext: func(_ context.Context, network, addr string, _ *tls.Config) (net.Conn, error) {
					// Skip TLS to use h2c
					return net.Dial(network, addr)
				},
			},
		}

		chainID := network.GetSubnet(subnetAName).Chains[0].ChainID.String()
		client := xsvmconnect.NewPingClient(
			httpClient,
			node.URI,
			connect.WithInterceptors(
				connectclient.SetRouteHeaderInterceptor{Route: chainID},
			),
		)

		tc.By("serving unary rpc")
		msg := "foobar"
		request := &connect.Request[xsvm.PingRequest]{
			Msg: &xsvm.PingRequest{
				Message: msg,
			},
		}

		reply, err := client.Ping(tc.DefaultContext(), request)
		require.NoError(err)
		require.Equal(msg, reply.Msg.Message)

		tc.By("serving bidirectional streaming rpc")

		stream := client.StreamPing(tc.DefaultContext())
		ginkgo.DeferCleanup(func() {
			require.NoError(stream.CloseRequest())
		})

		// Stream pings to the server and block until all events are received
		// back.
		eg := &errgroup.Group{}

		n := 10
		eg.Go(func() error {
			for i := 0; i < n; i++ {
				msg := fmt.Sprintf("ping-%d", i)
				if err := stream.Send(&xsvm.StreamPingRequest{
					Message: msg,
				}); err != nil {
					return err
				}

				log.Info("sent message", zap.String("msg", msg))
			}

			return nil
		})

		eg.Go(func() error {
			for i := 0; i < n; i++ {
				reply, err := stream.Receive()
				if err != nil {
					return err
				}

				if fmt.Sprintf("ping-%d", i) != reply.Message {
					return fmt.Errorf("unexpected ping reply: %s", reply.Message)
				}

				log.Info("received message", zap.String("msg", reply.Message))
			}

			return nil
		})

		require.NoError(eg.Wait())
	})
})

// Retrieve the nodes corresponding to the provided IDs
func getNodesForIDs(nodes []*tmpnet.Node, nodeIDs []ids.NodeID) []*tmpnet.Node {
	desiredNodes := make([]*tmpnet.Node, 0, len(nodeIDs))
	for _, node := range nodes {
		for _, nodeID := range nodeIDs {
			if node.NodeID == nodeID {
				desiredNodes = append(desiredNodes, node)
			}
		}
	}
	return desiredNodes
}
