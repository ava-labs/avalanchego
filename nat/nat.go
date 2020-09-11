// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package nat

import (
	"errors"
	"net"
	"sync"
	"time"

	"github.com/ava-labs/avalanche-go/utils/logging"
)

const (
	mapTimeout       = 30 * time.Minute
	mapUpdateTimeout = mapTimeout / 2
	maxRetries       = 20
)

// Router describes the functionality that a network device must support to be
// able to open ports to an external IP.
type Router interface {
	MapPort(protocol string, intPort, extPort uint16, desc string, duration time.Duration) error
	UnmapPort(protocol string, intPort, extPort uint16) error
	ExternalIP() (net.IP, error)
	GetPortMappingEntry(extPort uint16, protocol string) (
		InternalIP string,
		InternalPort uint16,
		Description string,
		err error,
	)
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

// Map sets up port mapping using given protocol, internal and external ports
// and returns the final port mapped. It returns 0 if mapping failed after the
// maximun number of retries
func (dev *Mapper) Map(protocol string, intPort uint16, desc string) (uint16, error) {
	mappedPort := make(chan uint16)

	go dev.keepPortMapping(mappedPort, protocol, intPort, desc)

	port := <-mappedPort
	if port == 0 {
		return 0, errors.New("failed to map port")
	}
	return port, nil
}

// keepPortMapping runs in the background to keep a port mapped. It renews the
// the port mapping in mapUpdateTimeout.
func (dev *Mapper) keepPortMapping(mappedPort chan<- uint16, protocol string,
	intPort uint16, desc string) {
	updateTimer := time.NewTimer(mapUpdateTimeout)

	for i := 0; i <= maxRetries; i++ {
		extPort := intPort + uint16(i)
		if addr, port, desc, err := dev.r.GetPortMappingEntry(extPort, protocol); err == nil {
			dev.log.Debug("Port %d is taken by %s:%d: %s, retry with the next port",
				extPort, addr, port, desc)
		} else if err := dev.r.MapPort(protocol, intPort, extPort, desc, mapTimeout); err != nil {
			dev.log.Debug("Map port failed. Protocol %s Internal %d External %d. %s",
				protocol, intPort, extPort, err)
		} else {
			dev.log.Info("Mapped Protocol %s Internal %d External %d.", protocol,
				intPort, extPort)

			dev.wg.Add(1)

			mappedPort <- extPort

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
					if err := dev.r.MapPort(protocol, intPort, extPort, desc, mapTimeout); err != nil {
						dev.log.Error("Renewing port mapping from external port %d to internal port %d failed with %s",
							intPort, extPort, err)
					} else {
						dev.log.Debug("Renewed port mapping from external port %d to internal port %d.",
							intPort, extPort)
					}

					updateTimer.Reset(mapUpdateTimeout)
				case <-dev.closer:
					return
				}
			}
		}
	}

	dev.log.Debug("Unable to map port %d~%d", intPort, intPort+maxRetries)
	mappedPort <- 0
}

// UnmapAllPorts stops mapping all ports from this mapper and attempts to unmap
// them.
func (dev *Mapper) UnmapAllPorts() {
	close(dev.closer)
	dev.wg.Wait()
	dev.log.Info("Unmapped all ports")
}
