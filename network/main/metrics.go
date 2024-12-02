package main

import (
	"encoding/json"
	"os"
	"path"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/logging"
)

const (
	dir = "metriclogs"
)

type metric struct {
	Time time.Time
	Online bool 
}

// collects metrics
type metrics struct { 
	// online(true) or offline(false)
	status map[ids.NodeID]bool
	log logging.Logger
}

func newMetrics(log logging.Logger) *metrics {
	return &metrics{
		status: make(map[ids.NodeID]bool),
		log: log,
	}
}

func (m *metrics) collect(tp *TestPeers) {
	// interval := constants.DefaultPingPongTimeout
	ticker := time.NewTicker(1* time.Second)

	for range ticker.C {
		m.log.Info("logging")
		for _, peer := range tp.peers {
			id := peer.ID()

			// we sent a message to this peer but didn't receive it 
			if peer.LastSent().Sub(peer.LastReceived()) > constants.DefaultPingPongTimeout {
				if (m.status[id]) {
					m.status[id] = false;
					// this peer has been offline since `LastSent`
					// push to metrics file
					// dir = id.String()
					filepath := path.Join(dir, id.String()+".json")
					metric := metric{
						Time: peer.LastSent(),
						Online: false,
					}
					bytes, err := json.Marshal(metric)
					if err != nil {
						m.log.Info("error marshaling")
					}
					push(filepath, bytes)
				}
				m.log.Info("Still ofline, don't log")
				// still offline
			} else if (!m.status[id]) {
				// peer was previously offline but is no longer
				// peer is online since `LastRecieved`
				// push to metrics file
				m.status[id] = true;
				filepath := path.Join(dir, id.String()+".json")
				metric := metric{
					Time: peer.LastReceived(),
					Online: true,
				}
				bytes, err := json.Marshal(metric)
				if err != nil {
					m.log.Info("error marshaling")
				}
				push(filepath, bytes)
				
			}
		}
	}
}

// pushes to file
func push(filename string, bytes []byte) error {
    // Open file with append mode (O_APPEND), create if not exists (O_CREATE)
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