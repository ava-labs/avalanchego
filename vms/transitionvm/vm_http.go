// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package transitionvm

import (
	"context"
	"net/http"
)

func (vm *VM) CreateHandlers(ctx context.Context) (map[string]http.Handler, error) {
	vm.transitionLock.RLock()
	defer vm.transitionLock.RUnlock()

	newHandlers, err := vm.current.chain.CreateHandlers(ctx)
	if err != nil {
		return nil, err
	}

	vm.httpHandlers.Set(newHandlers)

	// The engine only calls [VM.CreateHandlers] once. The transitionVM assumes
	// that the routes exposed by the pre-transition VM are a super-set of the
	// routes exposed by the post-transition VM.
	//
	// Coreth registers:
	// - /rpc (always)
	// - /ws (always)
	// - /avax (always)
	// - /admin (sometimes)
	//
	// CChain VM registers:
	// - /rpc (always)
	// - /ws (always)
	// - /avax (always)
	//
	// So Coreth's routes are a super-set of the CChain VM's routes.
	return vm.httpHandlers.AsInterface(), nil
}

// None of Subnet-EVM, Coreth, or SAEVM implement NewHTTPHandler, so it is left
// unimplemented.
func (*VM) NewHTTPHandler(context.Context) (http.Handler, error) {
	return nil, nil
}
