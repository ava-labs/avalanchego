// (c) 2019-2020, Ava Labs, Inc.
//
// This file is a derived work, based on the go-ethereum library whose original
// notices appear below.
//
// It is distributed under a license compatible with the licensing terms of the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********
// Copyright 2015 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package node

import (
	"context"
	"errors"

	"github.com/ava-labs/coreth/internal/debug"
	"github.com/ava-labs/coreth/rpc"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/p2p"
)

var (
	errNotSupported = errors.New("not supported")
)

// apis returns the collection of built-in RPC APIs.
func (n *Node) apis() []rpc.API {
	return []rpc.API{
		{
			Namespace: "admin",
			Version:   "1.0",
			Service:   &privateAdminAPI{},
		}, {
			Namespace: "admin",
			Version:   "1.0",
			Service:   &publicAdminAPI{},
			Public:    true,
		}, {
			Namespace: "debug",
			Version:   "1.0",
			Service:   debug.Handler,
		}, {
			Namespace: "web3",
			Version:   "1.0",
			Service:   &publicWeb3API{n},
			Public:    true,
		},
	}
}

// privateAdminAPI is the collection of administrative API methods exposed only
// over a secure RPC channel.
type privateAdminAPI struct {
	// node *Node // Node interfaced by this API
}

// AddPeer requests connecting to a remote node, and also maintaining the new
// connection at all times, even reconnecting if it is lost.
func (api *privateAdminAPI) AddPeer(url string) (bool, error) {
	// Original code:
	// // Make sure the server is running, fail otherwise
	// server := api.node.Server()
	// if server == nil {
	// 	return false, ErrNodeStopped
	// }
	// // Try to add the url as a static peer and return
	// node, err := enode.Parse(enode.ValidSchemes, url)
	// if err != nil {
	// 	return false, fmt.Errorf("invalid enode: %v", err)
	// }
	// server.AddPeer(node)
	// return true, nil
	return false, errNotSupported
}

// RemovePeer disconnects from a remote node if the connection exists
func (api *privateAdminAPI) RemovePeer(url string) (bool, error) {
	// Original code:
	// // Make sure the server is running, fail otherwise
	// server := api.node.Server()
	// if server == nil {
	// 	return false, ErrNodeStopped
	// }
	// // Try to remove the url as a static peer and return
	// node, err := enode.Parse(enode.ValidSchemes, url)
	// if err != nil {
	// 	return false, fmt.Errorf("invalid enode: %v", err)
	// }
	// server.RemovePeer(node)
	// return true, nil
	return false, errNotSupported
}

// AddTrustedPeer allows a remote node to always connect, even if slots are full
func (api *privateAdminAPI) AddTrustedPeer(url string) (bool, error) {
	// Original code:
	// // Make sure the server is running, fail otherwise
	// server := api.node.Server()
	// if server == nil {
	// 	return false, ErrNodeStopped
	// }
	// node, err := enode.Parse(enode.ValidSchemes, url)
	// if err != nil {
	// 	return false, fmt.Errorf("invalid enode: %v", err)
	// }
	// server.AddTrustedPeer(node)
	// return true, nil
	return false, errNotSupported
}

// RemoveTrustedPeer removes a remote node from the trusted peer set, but it
// does not disconnect it automatically.
func (api *privateAdminAPI) RemoveTrustedPeer(url string) (bool, error) {
	// Original code:
	// // Make sure the server is running, fail otherwise
	// server := api.node.Server()
	// if server == nil {
	// 	return false, ErrNodeStopped
	// }
	// node, err := enode.Parse(enode.ValidSchemes, url)
	// if err != nil {
	// 	return false, fmt.Errorf("invalid enode: %v", err)
	// }
	// server.RemoveTrustedPeer(node)
	// return true, nil
	return false, errNotSupported
}

// PeerEvents creates an RPC subscription which receives peer events from the
// node's p2p.Server
func (api *privateAdminAPI) PeerEvents(ctx context.Context) (*rpc.Subscription, error) {
	// Original code:
	// // Make sure the server is running, fail otherwise
	// server := api.node.Server()
	// if server == nil {
	// 	return nil, ErrNodeStopped
	// }

	// // Create the subscription
	// notifier, supported := rpc.NotifierFromContext(ctx)
	// if !supported {
	// 	return nil, rpc.ErrNotificationsUnsupported
	// }
	// rpcSub := notifier.CreateSubscription()

	// go func() {
	// 	events := make(chan *p2p.PeerEvent)
	// 	sub := server.SubscribeEvents(events)
	// 	defer sub.Unsubscribe()

	// 	for {
	// 		select {
	// 		case event := <-events:
	// 			notifier.Notify(rpcSub.ID, event)
	// 		case <-sub.Err():
	// 			return
	// 		case <-rpcSub.Err():
	// 			return
	// 		case <-notifier.Closed():
	// 			return
	// 		}
	// 	}
	// }()
	// return rpcSub, nil
	return nil, errNotSupported
}

// publicAdminAPI is the collection of administrative API methods exposed over
// both secure and unsecure RPC channels.
type publicAdminAPI struct {
	// node *Node // Node interfaced by this API
}

// Peers retrieves all the information we know about each individual peer at the
// protocol granularity.
func (api *publicAdminAPI) Peers() ([]*p2p.PeerInfo, error) {
	// Original code:
	// server := api.node.Server()
	// if server == nil {
	// 	return nil, ErrNodeStopped
	// }
	// return server.PeersInfo(), nil
	return nil, errNotSupported
}

// NodeInfo retrieves all the information we know about the host node at the
// protocol granularity.
func (api *publicAdminAPI) NodeInfo() (*p2p.NodeInfo, error) {
	// Original code:
	// server := api.node.Server()
	// if server == nil {
	// 	return nil, ErrNodeStopped
	// }
	// return server.NodeInfo(), nil
	return nil, errNotSupported
}

// Datadir retrieves the current data directory the node is using.
func (api *publicAdminAPI) Datadir() string {
	// Original code:
	// return api.node.DataDir()
	return ""
}

// publicWeb3API offers helper utils
type publicWeb3API struct {
	stack *Node
}

// ClientVersion returns the node name
func (s *publicWeb3API) ClientVersion() string {
	// Original code:
	// return s.stack.Server().Name
	return s.stack.config.CorethVersion
}

// Sha3 applies the ethereum sha3 implementation on the input.
// It assumes the input is hex encoded.
func (s *publicWeb3API) Sha3(input hexutil.Bytes) hexutil.Bytes {
	return crypto.Keccak256(input)
}
