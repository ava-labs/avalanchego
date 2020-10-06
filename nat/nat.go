// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package nat

import (
	"net"
	"sync"
	"time"

	"github.com/ava-labs/avalanchego/utils/logging"
)

const (
	mapTimeout        = 30 * time.Minute
	mapUpdateTimeout  = 5 * time.Minute
	maxRefreshRetries = 3
)

// Router describes the functionality that a network device must support to be
// able to open ports to an external IP.
type Router interface {
	IsNATTraversal() bool
	MapPort(protocol string, intPort, extPort uint16, desc string, duration time.Duration) error
	UnmapPort(protocol string, intPort, extPort uint16) error
	ExternalIP() (net.IP, error)
}

// GetRouter returns a router on the current network.
func GetRouter() Router {
	if r := getUPnPRouter(); r != nil {
		return r
	}
	if r := getPMPRouter(); r != nil {
		return r
	}

	return NewNoRouter()
}

// Mapper attempts to open a set of ports on a router
type Mapper struct {
	log    logging.Logger
	r      Router
	closer chan struct{}
	wg     sync.WaitGroup
}

// NewPortMapper returns an initialized mapper
func NewPortMapper(log logging.Logger, r Router) Mapper {
	return Mapper{
		log:    log,
		r:      r,
		closer: make(chan struct{}),
	}
}

// Map a connection from extPort (exposed to the internet) to our intPort (where
// our process is listening).
func (dev *Mapper) Map(protocol string, intPort, extPort uint16, desc string) {
	if !dev.r.IsNATTraversal() {
		return
	}

	// we attempt a port map, and log an Error if it fails.
	err := dev.retryMapPort(protocol, intPort, extPort, desc, mapTimeout)
	if err != nil {
		dev.log.Error("NAT Traversal failed from external port %d to internal port %d with %s", extPort, intPort, err)
	} else {
		dev.log.Info("NAT Traversal successful from external port %d to internal port %d", extPort, intPort)
	}

	go dev.keepPortMapping(protocol, intPort, extPort, desc)
}

// Retry port map up to maxRefreshRetries with a 1 second delay
func (dev *Mapper) retryMapPort(protocol string, intPort, extPort uint16, desc string, timeout time.Duration) error {
	var err error
	for retryCnt := 0; retryCnt < maxRefreshRetries; retryCnt++ {
		err = dev.r.MapPort(protocol, intPort, extPort, desc, timeout)
		if err == nil {
			return nil
		}

		// log a message, sleep a second and retry.
		dev.log.Error("Renewing port mapping try #%d from external port %d to internal port %d failed with %s",
			retryCnt+1, extPort, intPort, err)
		time.Sleep(1 * time.Second)
	}
	return err
}

// keepPortMapping runs in the background to keep a port mapped. It renews the
// the port mapping at intervals of mapUpdateTimeout.
func (dev *Mapper) keepPortMapping(protocol string, intPort, extPort uint16, desc string) {
	updateTimer := time.NewTimer(mapUpdateTimeout)

	dev.wg.Add(1)

	defer func(extPort uint16) {
		updateTimer.Stop()

		dev.log.Debug("Unmap protocol %s external port %d", protocol, extPort)
		if err := dev.r.UnmapPort(protocol, intPort, extPort); err != nil {
			dev.log.Debug("Error unmapping port %d to %d: %s", intPort, extPort, err)
		}

		dev.wg.Done()
	}(extPort)

	for {
		select {
		case <-updateTimer.C:
			err := dev.retryMapPort(protocol, intPort, extPort, desc, mapTimeout)
			if err != nil {
				dev.log.Warn("Renew NAT Traversal failed from external port %d to internal port %d with %s",
					extPort, intPort, err)
			}
			updateTimer.Reset(mapUpdateTimeout)
		case <-dev.closer:
			return
		}
	}
}

// UnmapAllPorts stops mapping all ports from this mapper and attempts to unmap
// them.
func (dev *Mapper) UnmapAllPorts() {
	close(dev.closer)
	dev.wg.Wait()
	dev.log.Info("Unmapped all ports")
}
