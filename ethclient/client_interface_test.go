// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ethclient

import (
	"reflect"
	"testing"
)

func TestInterfaceStructOneToOne(t *testing.T) {
	// checks struct provides at least the methods signatures in the interface
	var _ Client = (*client)(nil)
	// checks interface and struct have the same number of methods
	clientType := reflect.TypeOf(&client{})
	ClientType := reflect.TypeOf((*Client)(nil)).Elem()
	if clientType.NumMethod() != ClientType.NumMethod() {
		t.Fatalf("no 1 to 1 compliance between struct methods (%v) and interface methods (%v)", clientType.NumMethod(), ClientType.NumMethod())
	}
}
