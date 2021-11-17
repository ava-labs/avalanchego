// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package logging

type NoFactory struct{}

func (NoFactory) Make(string) (Logger, error) { return NoLog{}, nil }

func (NoFactory) MakeChain(string) (Logger, error) { return NoLog{}, nil }

func (NoFactory) MakeChainChild(string, string) (Logger, error) { return NoLog{}, nil }

func (NoFactory) Close() {}

func (NoFactory) SetLogLevel(name string, level Level) error { return nil }

func (NoFactory) SetDisplayLevel(name string, level Level) error { return nil }

func (NoFactory) GetLogLevel(name string) (Level, error) { return Off, nil }

func (NoFactory) GetDisplayLevel(name string) (Level, error) { return Off, nil }

func (NoFactory) GetLoggerNames() []string { return nil }
