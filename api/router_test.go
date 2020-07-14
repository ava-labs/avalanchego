// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package api

import (
	"net/http"
	"testing"
)

type testHandler struct{ called bool }

func (t *testHandler) ServeHTTP(_ http.ResponseWriter, _ *http.Request) {
	t.called = true
}

func TestAliasing(t *testing.T) {
	r := newRouter()

	if err := r.AddAlias("1", "2", "3"); err != nil {
		t.Fatal(err)
	}
	if err := r.AddAlias("1", "4"); err != nil {
		t.Fatal(err)
	}
	if err := r.AddAlias("5", "1"); err != nil {
		t.Fatal(err)
	}
	if err := r.AddAlias("3", "6"); err != nil {
		t.Fatal(err)
	}
	if err := r.AddAlias("7", "4"); err == nil {
		t.Fatalf("Already reserved %s", "4")
	}

	handler1 := &testHandler{}
	if err := r.AddRouter("2", "", handler1); err == nil {
		t.Fatalf("Already reserved %s", "2")
	}
	if err := r.AddRouter("5", "", handler1); err != nil {
		t.Fatal(err)
	}
	if handler, exists := r.routes["5"][""]; !exists {
		t.Fatalf("Should have added %s", "5")
	} else if handler != handler1 {
		t.Fatalf("Registered unknown handler")
	}

	if err := r.AddAlias("5", "7"); err != nil {
		t.Fatal(err)
	}

	if handler, exists := r.routes["7"][""]; !exists {
		t.Fatalf("Should have added %s", "7")
	} else if handler != handler1 {
		t.Fatalf("Registered unknown handler")
	}
}

func TestBlock(t *testing.T) {
	r := newRouter()

	if err := r.AddAlias("1", "1"); err != nil {
		t.Fatal(err)
	}

	handler1 := &testHandler{}
	if err := r.AddRouter("1", "", handler1); err == nil {
		t.Fatalf("Permanently locked %s", "1")
	}
}

func TestReplaceRouter(t *testing.T) {
	originalHandler := &testHandler{}
	replaceHandler := &testHandler{}
	r := newRouter()

	if err := r.AddRouter("base", "endpoint", originalHandler); err != nil {
		t.Fatalf("Failed to add route")
	}

	if err := r.AddAlias("base", "alias-base1"); err != nil {
		t.Fatalf("Failed to add aliases for base")
	}

	if err := r.AddAlias("alias-base1", "secondary-alias"); err != nil {
		t.Fatalf("Failed to add secondary alias")
	}

	if err := r.ReplaceRouter("base", "endpoint", replaceHandler); err != nil {
		t.Fatalf("Failed to replace handler")
	}

	if handler, err := r.GetHandler("base", "endpoint"); err != nil {
		t.Fatalf("Couldn't get replaced handler")
	} else if handler != replaceHandler {
		t.Fatalf("Handler for base was not replaced by target")
	}

	if handler, err := r.GetHandler("alias-base1", "endpoint"); err != nil {
		t.Fatalf("Couldn't get replaced handler")
	} else if handler != replaceHandler {
		t.Fatalf("Handler for alias was not replaced by target")
	}

	if handler, err := r.GetHandler("secondary-alias", "endpoint"); err != nil {
		t.Fatalf("Couldn't get replaced handler")
	} else if handler != replaceHandler {
		t.Fatalf("Handler for secondary alias was not replaced by target")
	}
}
