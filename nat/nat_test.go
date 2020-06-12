package nat

// go test -run 'HTTP'

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"testing"
	"time"

	"github.com/ava-labs/gecko/utils/logging"
)

const (
	externalPort = 9876
	localPort    = 8080
)

func TestRouter(t *testing.T) {
	n := GetNATRouter()
	if n == nil {
		fmt.Println("NAT Router is nil")
		return
	}

	ip, err := n.ExternalIP()
	if err != nil {
		fmt.Printf("Unable to get external IP: %v\n", err)
		return
	}
	fmt.Printf("External Address %s:%d\n", ip.String(), externalPort)
}

func TestHTTP(t *testing.T) {
	config, err := logging.DefaultConfig()
	if err != nil {
		return
	}
	factory := logging.NewFactory(config)
	defer factory.Close()

	log, err := factory.Make()
	if err != nil {
		return
	}
	defer log.Stop()
	defer log.StopOnPanic()

	log.Info("Logger Initialized")

	n := GetNATRouter()
	if n == nil {
		log.Error("Unable to get UPnP Device")
		return
	}

	ip, err := n.ExternalIP()
	if err != nil {
		log.Error("Unable to get external IP: %v", err)
		return
	}
	log.Info("External Address %s:%d", ip.String(), externalPort)

	r := NewRouter(log, n)
	defer r.UnmapAllPorts()

	r.Map("TCP", localPort, externalPort, "AVA UPnP Test")

	log.Info("Starting HTTP Service")
	server := &http.Server{Addr: ":" + strconv.Itoa(localPort)}
	http.HandleFunc("/", hello)
	go func() {
		server.ListenAndServe()
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt)

	<-stop

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	server.Shutdown(ctx)
}

func hello(w http.ResponseWriter, req *http.Request) {
	fmt.Fprintf(w, "AVA UPnP Test\n")
}
