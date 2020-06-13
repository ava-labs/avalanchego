package nat

import (
	"net"
	"sync"
	"time"

	"github.com/ava-labs/gecko/utils/logging"
	"github.com/ava-labs/gecko/utils/wrappers"
)

const (
	mapTimeout       = 30 * time.Minute
	mapUpdateTimeout = mapTimeout / 2
	maxRetries       = 20
)

type NATRouter interface {
	MapPort(protocol string, intport, extport uint16, desc string, duration time.Duration) error
	UnmapPort(protocol string, extport uint16) error
	ExternalIP() (net.IP, error)
	IsMapped(extport uint16, protocol string) bool
}

func GetNATRouter() NATRouter {
	//TODO add PMP support
	if r := getUPnPRouter(); r != nil {
		return r
	}

	return NewNoRouter()
}

type Router struct {
	log     logging.Logger
	r       NATRouter
	closer  chan struct{}
	wg      sync.WaitGroup
	errLock sync.Mutex
	errs    wrappers.Errs
}

func NewRouter(log logging.Logger, r NATRouter) Router {
	return Router{
		log:    log,
		r:      r,
		closer: make(chan struct{}),
	}
}

// Map sets up port mapping using given protocol, internal and external ports
// and returns the final port mapped. It returns 0 if mapping failed after the
// maximun number of retries
func (dev *Router) Map(protocol string, intport, extport uint16, desc string) uint16 {
	mappedPort := make(chan uint16)

	dev.wg.Add(1)
	go dev.keepPortMapping(mappedPort, protocol, intport, extport, desc)

	return <-mappedPort
}

// keepPortMapping runs in the background to keep a port mapped. It renews the
// the port mapping in mapUpdateTimeout.
func (dev *Router) keepPortMapping(mappedPort chan<- uint16, protocol string,
	intport, extport uint16, desc string) {
	updateTimer := time.NewTimer(mapUpdateTimeout)
	var port uint16 = 0

	defer func() {
		updateTimer.Stop()

		dev.log.Info("Unmap protocol %s external port %d", protocol, port)
		if port > 0 {
			dev.errLock.Lock()
			dev.errs.Add(dev.r.UnmapPort(protocol, port))
			dev.errLock.Unlock()
		}

		dev.wg.Done()
	}()

	for i := 0; i < maxRetries; i++ {
		port = extport + uint16(i)
		if dev.r.IsMapped(port, protocol) {
			dev.log.Info("Port %d is occupied, retry with the next port", port)
			continue
		}
		if err := dev.r.MapPort(protocol, intport, port, desc, mapTimeout); err != nil {
			dev.log.Error("Map port failed. Protocol %s Internal %d External %d. %s",
				protocol, intport, port, err)
			dev.errLock.Lock()
			dev.errs.Add(err)
			dev.errLock.Unlock()
		} else {
			dev.log.Info("Mapped Protocol %s Internal %d External %d.", protocol,
				intport, port)
			mappedPort <- port
			break
		}
	}

	if port == 0 {
		dev.log.Error("Unable to map port %d", extport)
		mappedPort <- port
		return
	}

	for {
		select {
		case <-updateTimer.C:
			if err := dev.r.MapPort(protocol, intport, port, desc, mapTimeout); err != nil {
				dev.log.Error("Renew port mapping failed. Protocol %s Internal %d External %d. %s",
					protocol, intport, port, err)
			} else {
				dev.log.Info("Renew port mapping Protocol %s Internal %d External %d.", protocol,
					intport, port)
			}

			updateTimer.Reset(mapUpdateTimeout)
		case _, _ = <-dev.closer:
			return
		}
	}
}

func (dev *Router) UnmapAllPorts() error {
	close(dev.closer)
	dev.wg.Wait()
	dev.log.Info("Unmapped all ports")
	return dev.errs.Err
}
