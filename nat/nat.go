// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package nat

import (
	"net"
	"sync"
	"time"

	"github.com/ava-labs/gecko/utils/logging"
	"github.com/ava-labs/gecko/utils/wrappers"
)

const (
	mapTimeout       = 30 * time.Second
	mapUpdateTimeout = mapTimeout / 2
	maxRetries       = 20
)

type NATRouter interface {
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

func GetNATRouter() NATRouter {
	if r := getUPnPRouter(); r != nil {
		return r
	}
	if r := getPMPRouter(); r != nil {
		return r
	}

	return NewNoRouter()
}

type Mapper struct {
	log     logging.Logger
	r       NATRouter
	closer  chan struct{}
	wg      sync.WaitGroup
	errLock sync.Mutex
	errs    wrappers.Errs
}

func NewPortMapper(log logging.Logger, r NATRouter) Mapper {
	return Mapper{
		log:    log,
		r:      r,
		closer: make(chan struct{}),
	}
}

// Map sets up port mapping using given protocol, internal and external ports
// and returns the final port mapped. It returns 0 if mapping failed after the
// maximun number of retries
func (dev *Mapper) Map(protocol string, intPort, extPort uint16, desc string) uint16 {
	mappedPort := make(chan uint16)

	go dev.keepPortMapping(mappedPort, protocol, intPort, extPort, desc)

	return <-mappedPort
}

// keepPortMapping runs in the background to keep a port mapped. It renews the
// the port mapping in mapUpdateTimeout.
func (dev *Mapper) keepPortMapping(mappedPort chan<- uint16, protocol string,
	intPort, extPort uint16, desc string) {
	updateTimer := time.NewTimer(mapUpdateTimeout)

	for i := 0; i <= maxRetries; i++ {
		port := extPort + uint16(i)
		if intaddr, intPort, desc, err := dev.r.GetPortMappingEntry(port, protocol); err == nil {
			dev.log.Info("Port %d is mapped to %s:%d: %s, retry with the next port",
				port, intaddr, intPort, desc)
			port = 0
			continue
		}
		if err := dev.r.MapPort(protocol, intPort, port, desc, mapTimeout); err != nil {
			dev.log.Error("Map port failed. Protocol %s Internal %d External %d. %s",
				protocol, intPort, port, err)
			dev.errLock.Lock()
			dev.errs.Add(err)
			dev.errLock.Unlock()
		} else {
			dev.log.Info("Mapped Protocol %s Internal %d External %d.", protocol,
				intPort, port)
			mappedPort <- port

			dev.wg.Add(1)

			defer func(port uint16) {
				updateTimer.Stop()

				dev.log.Info("Unmap protocol %s external port %d", protocol, port)
				dev.errLock.Lock()
				dev.errs.Add(dev.r.UnmapPort(protocol, intPort, port))
				dev.errLock.Unlock()

				dev.wg.Done()
			}(port)

			for {
				select {
				case <-updateTimer.C:
					if err := dev.r.MapPort(protocol, intPort, port, desc, mapTimeout); err != nil {
						dev.log.Error("Renew port mapping failed. Protocol %s Internal %d External %d. %s",
							protocol, intPort, port, err)
					} else {
						dev.log.Info("Renew port mapping Protocol %s Internal %d External %d.", protocol,
							intPort, port)
					}

					updateTimer.Reset(mapUpdateTimeout)
				case _, _ = <-dev.closer:
					return
				}
			}
		}
	}

	dev.log.Warn("Unable to map port %d~%d", extPort, extPort+maxRetries)
	mappedPort <- 0
}

func (dev *Mapper) UnmapAllPorts() error {
	close(dev.closer)
	dev.wg.Wait()
	dev.log.Info("Unmapped all ports")
	return dev.errs.Err
}
