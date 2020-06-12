package nat

import (
	"net"
	"sync"
	"time"

	"github.com/ava-labs/gecko/utils/logging"
	"github.com/ava-labs/gecko/utils/wrappers"
)

const (
	mapTimeout = 30 * time.Minute
	mapUpdate  = mapTimeout / 2
)

type NATRouter interface {
	MapPort(protocol string, intport, extport uint16, desc string, duration time.Duration) error
	UnmapPort(protocol string, extport uint16) error
	ExternalIP() (net.IP, error)
}

func GetNATRouter() NATRouter {
	//TODO other protocol
	if r := getUPnPRouter(); r != nil {
		return r
	}
	return nil
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

func (dev *Router) Map(protocol string, intport, extport uint16, desc string) {
	dev.wg.Add(1)
	go dev.mapPort(protocol, intport, extport, desc)
}

func (dev *Router) mapPort(protocol string, intport, extport uint16, desc string) {
	updater := time.NewTimer(mapUpdate)
	defer func() {
		updater.Stop()

		dev.log.Info("Unmap protocol %s external port %d", protocol, extport)
		dev.errLock.Lock()
		dev.errs.Add(dev.r.UnmapPort(protocol, extport))
		dev.errLock.Unlock()

		dev.wg.Done()
	}()

	if err := dev.r.MapPort(protocol, intport, extport, desc, mapTimeout); err != nil {
		dev.log.Error("Map port failed. Protocol %s Internal %d External %d. %s",
			protocol, intport, extport, err)
		dev.errLock.Lock()
		dev.errs.Add(err)
		dev.errLock.Unlock()
	} else {
		dev.log.Info("Mapped Protocol %s Internal %d External %d.", protocol,
			intport, extport)
	}

	for {
		select {
		case <-updater.C:
			if err := dev.r.MapPort(protocol, intport, extport, desc, mapTimeout); err != nil {
				dev.log.Error("Renew port mapping failed. Protocol %s Internal %d External %d. %s",
					protocol, intport, extport, err)
			} else {
				dev.log.Info("Renew port mapping Protocol %s Internal %d External %d.", protocol,
					intport, extport)
			}

			updater.Reset(mapUpdate)
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
