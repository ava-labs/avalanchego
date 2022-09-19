// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tests

import "testing"

func TestExpectFunc(t *testing.T) {
	ep, err := newExpect("echo", "hello world")
	if err != nil {
		t.Fatal(err)
	}
	wstr := "hello world\r\n"
	l, eerr := ep.ExpectFunc(func(a string) bool { return len(a) > 10 })
	if eerr != nil {
		t.Fatal(eerr)
	}
	if l != wstr {
		t.Fatalf(`got "%v", expected "%v"`, l, wstr)
	}
	if cerr := ep.Close(); cerr != nil {
		t.Fatal(cerr)
	}
}

func TestEcho(t *testing.T) {
	ep, err := newExpect("echo", "hello world")
	if err != nil {
		t.Fatal(err)
	}
	l, eerr := ep.Expect("world")
	if eerr != nil {
		t.Fatal(eerr)
	}
	wstr := "hello world"
	if l[:len(wstr)] != wstr {
		t.Fatalf(`got "%v", expected "%v"`, l, wstr)
	}
	if cerr := ep.Close(); cerr != nil {
		t.Fatal(cerr)
	}
	if _, eerr = ep.Expect("..."); eerr == nil {
		t.Fatalf("expected error on closed expect process")
	}
}
