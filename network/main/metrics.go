package main

import (
	"encoding/json"
	"os"
	"path"
	"path/filepath"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/logging"
)

const (
	dir = "metriclogs"
	fileType = ".json"
)

type onlineMetric struct {
	Time   time.Time
	Online bool
}

// collects metrics
type metrics struct {
	online map[ids.NodeID]bool
	log    logging.Logger
}

func newMetrics(log logging.Logger) *metrics {
	return &metrics{
		online: make(map[ids.NodeID]bool),
		log:    log,
	}
}

func (m *metrics) collect(tp *TestPeers) {
	interval := constants.DefaultPingPongTimeout
	ticker := time.NewTicker(interval)

	for range ticker.C {
		for _, peer := range tp.peers {
			id := peer.ID()

			// we sent a message to this peer but didn't receive it
			if peer.LastSent().Sub(peer.LastReceived()) > constants.DefaultPingPongTimeout {
				// this peer has been offline since `LastSent`
				if m.online[id] {
					m.setOnline(id, false, peer.LastSent())
				}
				m.log.Info("Still ofline, don't log")
			} else if !m.online[id] {
				// peer was previously offline but is no longer
				// peer is online since `LastRecieved`
				m.setOnline(id, true, peer.LastReceived())
			}
		}
	}
}

func (m metrics) setOnline(id ids.NodeID, online bool, time time.Time) error {
	m.online[id] = online
	filepath := path.Join(dir, id.String()+fileType)
	om := onlineMetric{
		Time:   time,
		Online: online,
	}
	
	return appendToFile(filepath, om)
}

// only online metric for now, could change to interface to make generic
func appendToFile(filename string, x interface{}) error {
	dir := filepath.Dir(filename)

	// Create all directories in path if they don't exist
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	// append to current file if exists
	var data []interface{}
	fileBytes, err := os.ReadFile(filename)
	if err == nil {
		err = json.Unmarshal(fileBytes, &data)
		if err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}

	data = append(data, x)
	dataBytes, err := json.Marshal(data)
	if err != nil {
		return err
	}

    return os.WriteFile(filename, dataBytes, 0666)
}
