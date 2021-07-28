// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package logging

type NoFactory struct{}

func (NoFactory) Make(string) (Logger, error) { return NoLog{}, nil }

func (NoFactory) MakeChain(string) (Logger, error) { return NoLog{}, nil }

func (NoFactory) MakeChainChild(string, string) (Logger, error) { return NoLog{}, nil }

func (NoFactory) Close() {}
