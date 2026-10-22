// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package fuzz runs avalanchego nodes in the test process and fuzzes them. A
// run is a sequence of [Op]s. After each op, a [Runner] checks invariants that
// must hold on every node.
package fuzz

import (
	"encoding/binary"
	"fmt"
	"math/rand/v2"
	"slices"
	"strconv"
)

// Kind names an operation.
type Kind uint8

const (
	_ Kind = iota // the zero Kind is not an op

	// Honest kinds build txs the network may commit. The model expects their
	// effects only if they commit.
	Transfer
	CreateSubnet
	TransferSubnetOwnership
	AddValidator
	AddDelegator
	ConvertSubnetToL1
	IncreaseL1Balance
	DisableL1Validator
	AddAutoRenewedValidator
	SetAutoRenewedValidatorConfig
	CreateChain
	AddSubnetValidator
	RemoveSubnetValidator
	Export
	ReAddValidator
	ExportToP
	ImportToP

	// Adversarial kinds build txs the network must never commit, or must
	// commit at most once.
	Replay
	DoubleSpend
	Inflate
)

// kindNames holds each Kind's name in traces. Index 0 is empty because the
// zero Kind is not an op.
var kindNames = [...]string{
	Transfer:                      "transfer",
	CreateSubnet:                  "createSubnet",
	TransferSubnetOwnership:       "transferSubnetOwnership",
	AddValidator:                  "addValidator",
	AddDelegator:                  "addDelegator",
	ConvertSubnetToL1:             "convertSubnetToL1",
	IncreaseL1Balance:             "increaseL1Balance",
	DisableL1Validator:            "disableL1Validator",
	AddAutoRenewedValidator:       "addAutoRenewedValidator",
	SetAutoRenewedValidatorConfig: "setAutoRenewedValidatorConfig",
	CreateChain:                   "createChain",
	AddSubnetValidator:            "addSubnetValidator",
	RemoveSubnetValidator:         "removeSubnetValidator",
	Export:                        "export",
	ReAddValidator:                "reAddValidator",
	ExportToP:                     "exportToP",
	ImportToP:                     "importToP",
	Replay:                        "replay",
	DoubleSpend:                   "doubleSpend",
	Inflate:                       "inflate",
}

// String returns the name that traces use for k.
func (k Kind) String() string {
	if !k.valid() {
		return "Kind(" + strconv.Itoa(int(k)) + ")"
	}
	return kindNames[k]
}

// MarshalText implements [encoding.TextMarshaler], so traces name kinds in
// JSON.
func (k Kind) MarshalText() ([]byte, error) {
	if !k.valid() {
		return nil, fmt.Errorf("invalid op kind %d", k)
	}
	return []byte(kindNames[k]), nil
}

// UnmarshalText implements [encoding.TextUnmarshaler].
func (k *Kind) UnmarshalText(text []byte) error {
	i := slices.Index(kindNames[:], string(text))
	if i <= 0 {
		return fmt.Errorf("unknown op kind %q", text)
	}

	*k = Kind(i)
	return nil
}

func (k Kind) valid() bool {
	return k > 0 && int(k) < len(kindNames)
}

// An Op is one step of a run.
//
// Args are raw random choices, not values. Each op maps them onto the state
// at the time it runs (for example, Args[0] modulo the number of subnets), so
// every Op is runnable, and a trace still runs after ops before it are
// removed.
type Op struct {
	Kind Kind      `json:"kind"`
	Args [4]uint64 `json:"args"`
}

// Generate returns n ops derived from seed.
func Generate(seed uint64, n int) []Op {
	rng := newRand(seed)
	ops := make([]Op, n)
	for i := range ops {
		ops[i].Kind = Kind(1 + rng.IntN(len(kindNames)-1))
		for j := range ops[i].Args {
			ops[i].Args[j] = rng.Uint64()
		}
	}

	return ops
}

// newRand returns a generator seeded by seed. Ops derive keys from their Args
// through it, so a replayed trace reuses the same keys. The keys guard
// nothing, so they do not need crypto/rand.
func newRand(seed uint64) *rand.Rand {
	var b [32]byte
	binary.LittleEndian.PutUint64(b[:], seed)
	return rand.New(rand.NewChaCha8(b))
}
