// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package load

type Agent struct {
	Issuer   Issuer
	Listener Listener
}

func NewAgent(
	issuer Issuer,
	listener Listener,
) Agent {
	return Agent{
		Issuer:   issuer,
		Listener: listener,
	}
}
