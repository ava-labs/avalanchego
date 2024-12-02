package main

import (
	"encoding/json"
	"os"
	"path"
	"path/filepath"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/peer"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/logging"
)

const (
	dir = "metriclogs"
)

type metric struct {
	Time   time.Time
	Online bool
}

// collects metrics
type metrics struct {
	// online(true) or offline(false)
	status map[ids.NodeID]bool
	log    logging.Logger
}

func newMetrics(log logging.Logger) *metrics {
	return &metrics{
		status: make(map[ids.NodeID]bool),
		log:    log,
	}
}

func (m *metrics) collect(tp *TestPeers) {
	// interval := constants.DefaultPingPongTimeout
	ticker := time.NewTicker(10 * time.Second)

	for range ticker.C {
		for _, peer := range tp.peers {
			id := peer.ID()

			// we sent a message to this peer but didn't receive it
			if peer.LastSent().Sub(peer.LastReceived()) > constants.DefaultPingPongTimeout {
				// this peer has been offline since `LastSent`
				// push to metrics file
				if m.status[id] {
					m.setStatus(peer, false)
				}
				m.log.Info("Still ofline, don't log")
			} else if !m.status[id] {
				// peer was previously offline but is no longer
				// peer is online since `LastRecieved`
				// push to metrics file
				m.setStatus(peer, true)
			}
		}
	}
}

func (m metrics) setStatus(peer peer.Peer, online bool) error {
	m.status[peer.ID()] = online
	filepath := path.Join(dir, peer.ID().String()+".json")
	metric := metric{
		Time:   peer.LastSent(),
		Online: false,
	}
	bytes, err := json.Marshal(metric)
	if err != nil {
		m.log.Info("error marshaling")
		return err
	}

	return appendToFile(filepath, bytes)
}

// pushes to file
func appendToFile(filename string, bytes []byte) error {
	// Get the directory path
	dir := filepath.Dir(filename)

	// Create all directories in path if they don't exist
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	file, err := os.OpenFile(filename, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0666)
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = file.Write(bytes)
	return err
}

// defaultpongtimeout
// if lastSent > lastRecieved + constants.DefaultPongTimeout
// 		push(time, offline)
// else if (offline) {
// 		push(time, online)
// }
