// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package formatting

import (
	"testing"
)

func TestIntFormat(t *testing.T) {
	if format := IntFormat(0); format != "%01d" {
		t.Fatalf("Wrong int format: %s", format)
	}
	if format := IntFormat(9); format != "%01d" {
		t.Fatalf("Wrong int format: %s", format)
	}
	if format := IntFormat(10); format != "%02d" {
		t.Fatalf("Wrong int format: %s", format)
	}
	if format := IntFormat(99); format != "%02d" {
		t.Fatalf("Wrong int format: %s", format)
	}
	if format := IntFormat(100); format != "%03d" {
		t.Fatalf("Wrong int format: %s", format)
	}
	if format := IntFormat(999); format != "%03d" {
		t.Fatalf("Wrong int format: %s", format)
	}
	if format := IntFormat(1000); format != "%04d" {
		t.Fatalf("Wrong int format: %s", format)
	}
	if format := IntFormat(9999); format != "%04d" {
		t.Fatalf("Wrong int format: %s", format)
	}
	if format := IntFormat(10000); format != "%05d" {
		t.Fatalf("Wrong int format: %s", format)
	}
	if format := IntFormat(99999); format != "%05d" {
		t.Fatalf("Wrong int format: %s", format)
	}
	if format := IntFormat(100000); format != "%06d" {
		t.Fatalf("Wrong int format: %s", format)
	}
}
