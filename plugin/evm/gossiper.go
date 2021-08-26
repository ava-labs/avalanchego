package evm

import (
	"sync"

	commonEng "github.com/ava-labs/avalanchego/snow/engine/common"
)

type Gossiper interface {
	GossipAtomicTxID(atmTxIDBytes []byte) error
	GossipEthTxIDs(ethTxIDsBytes []byte) error
	listenAndGossip()
}

type gossiper struct {
	appSender commonEng.AppSender
	bytesChan chan []byte

	shutdownChan <-chan struct{}
	shutdownWg   *sync.WaitGroup
}

func NewGossiper(appSender commonEng.AppSender, wg *sync.WaitGroup, ch <-chan struct{}) Gossiper {
	return &gossiper{
		appSender:    appSender,
		bytesChan:    make(chan []byte, 1024), // TODO: pick proper size
		shutdownChan: ch,
		shutdownWg:   wg,
	}
}

func (g *gossiper) listenAndGossip() {
	defer g.shutdownWg.Done()
	g.shutdownWg.Add(1)

	for {
		select {
		case bytes := <-g.bytesChan:
			g.appSender.SendAppGossip(bytes) // TODO check for errors
		case <-g.shutdownChan:
			return
		}
	}
}

func (g *gossiper) GossipAtomicTxID(atmTxIDBytes []byte) error {
	g.bytesChan <- atmTxIDBytes
	return nil
}

func (g *gossiper) GossipEthTxIDs(ethTxIDsBytes []byte) error {
	g.bytesChan <- ethTxIDsBytes
	return nil
}
