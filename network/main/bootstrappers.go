package main

import (
	"bytes"
	"encoding/json"
	"net/http"

	"github.com/ava-labs/avalanchego/api/info"
	"github.com/ava-labs/avalanchego/genesis"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	"go.uber.org/zap"
)

const (
	publicApi = "https://api.avax-test.network/ext/info"
)

func getBootstrappers(log logging.Logger) ([]genesis.Bootstrapper, error) {
	// get all the nodes
	args := info.PeersArgs{
		NodeIDs: []ids.NodeID{},
	}

	jsonArgs, err := json.Marshal(args)
	if err != nil {
		return nil, err
	}
	
	resp, err := http.Post(publicApi, "application/json", bytes.NewBuffer(jsonArgs))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var reply info.PeersReply
	if err := json.NewDecoder(resp.Body).Decode(&reply); err != nil {
		return nil, err
	}

	log.Info("Bootstrappers", zap.Any("peers", reply.Peers))

	bootstrappers := make([]genesis.Bootstrapper, len(reply.Peers))
	for i, peer := range reply.Peers {
		bootstrappers[i] = genesis.Bootstrapper{
			IP:   peer.IP,
			ID:   peer.ID,
		}
	}

	return bootstrappers, nil
}
	