// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cchain

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/ava-labs/avalanchego/graft/evm/utils/rpc"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/vms/saevm/sae"
)

const (
	avaxServiceName       = "avax"
	avaxHTTPExtensionPath = "/" + avaxServiceName
)

var handlerPaths = append(sae.HandlerPaths, avaxHTTPExtensionPath)

// CreateHandlers returns the HTTP handlers exposed by the underlying SAE VM
// augmented with the avax service. None of the handlers are usable until after
// the [VM] is set as bootstrapping/normal operation.
func (vm *VM) CreateHandlers(context.Context) (map[string]http.Handler, error) {
	return vm.handlers.toInterface(), nil
}

// setHandlers initializes the lazy handlers with the real implementations.
func (vm *VM) setHandlers(ctx context.Context) error {
	m, err := vm.VM.CreateHandlers(ctx)
	if err != nil {
		return fmt.Errorf("creating SAE handlers: %w", err)
	}

	service, err := newService(vm.ctx, vm.gossipSet, vm.pushGossiper, vm.state)
	if err != nil {
		return fmt.Errorf("creating avax service: %w", err)
	}
	handler, err := rpc.NewHandler(avaxServiceName, service)
	if err != nil {
		return fmt.Errorf("creating avax RPC handler: %w", err)
	}

	m[avaxHTTPExtensionPath] = handler

	return vm.handlers.setHandlers(m)
}

// A handlerMap is a fixed set of HTTP routes whose implementations can be
// supplied after the routes have been exposed.
type handlerMap map[string]*lazyHandler

func newHandlerMap(paths ...string) handlerMap {
	m := make(handlerMap, len(paths))
	for _, path := range paths {
		m[path] = &lazyHandler{}
	}
	return m
}

func (m handlerMap) toInterface() map[string]http.Handler {
	iface := make(map[string]http.Handler, len(m))
	for path, lazy := range m {
		iface[path] = lazy
	}
	return iface
}

var (
	errUnregisteredHandlerPath = errors.New("handler path not registered at construction")
	errMissingHandlerPath      = errors.New("no handler provided for registered path")
)

// setHandlers routes each lazy handler to its implementation. Paths must match
// those registered at construction exactly, else a route stays unserved. Both
// directions are validated before any routing, so a mismatch changes nothing.
func (m handlerMap) setHandlers(actual map[string]http.Handler) error {
	for path := range actual {
		if _, ok := m[path]; !ok {
			return fmt.Errorf("%w: %q", errUnregisteredHandlerPath, path)
		}
	}
	for path := range m {
		if _, ok := actual[path]; !ok {
			return fmt.Errorf("%w: %q", errMissingHandlerPath, path)
		}
	}

	for path, h := range actual {
		m[path].set(h)
	}
	return nil
}

var _ http.Handler = (*lazyHandler)(nil)

// A lazyHandler is a placeholder for an actual HTTP handler. It returns 404
// until the actual handler is set.
type lazyHandler struct {
	h utils.Atomic[http.Handler]
}

func (l *lazyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h := l.h.Get()

	if h == nil {
		http.NotFound(w, r)
		return
	}

	h.ServeHTTP(w, r)
}

func (l *lazyHandler) set(h http.Handler) {
	l.h.Set(h)
}
