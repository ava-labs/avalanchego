// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cchain

import (
	"context"
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
// the [VM] set as bootstrapping/normal operation.
func (vm *VM) CreateHandlers(context.Context) (map[string]http.Handler, error) {
	for _, path := range handlerPaths {
		vm.handlers[path] = &lazyHandler{}
	}
	return vm.handlers.get(), nil
}

func (vm *VM) updateHandlers(ctx context.Context) error {
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

type handlerMap map[string]*lazyHandler

func (m handlerMap) get() map[string]http.Handler {
	iface := make(map[string]http.Handler, len(m))
	for path, lazy := range m {
		iface[path] = lazy
	}
	return iface
}

func (m handlerMap) setHandlers(actual map[string]http.Handler) error {
	for path, h := range actual {
		if lazy, ok := m[path]; ok {
			lazy.set(h)
		}
	}

	for path := range m {
		if _, ok := actual[path]; !ok {
			return fmt.Errorf("missing handler path %q", path)
		}
	}
	return nil
}

var _ http.Handler = (*lazyHandler)(nil)

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
