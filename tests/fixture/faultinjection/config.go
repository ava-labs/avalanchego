// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package faultinjection

import (
	"flag"
	"time"
)

const (
	defaultChaosInterval    = 30 * time.Second
	defaultChaosMinDuration = 10 * time.Second
	defaultChaosMaxDuration = 30 * time.Second

	// Default weights for experiment types (relative probability)
	defaultPodKillWeight     = 1
	defaultNetworkDelayWeight = 1
	defaultNetworkLossWeight  = 1

	// Default network chaos parameters
	defaultNetworkLatency   = "100ms"
	defaultNetworkJitter    = "50ms"
	defaultNetworkLossPercent = "25"
)

// Config contains configuration for fault injection.
type Config struct {
	// Interval between fault injections
	Interval time.Duration
	// Minimum duration for a chaos experiment
	MinDuration time.Duration
	// Maximum duration for a chaos experiment
	MaxDuration time.Duration

	// Experiment weights (relative probability)
	PodKillWeight      int
	NetworkDelayWeight int
	NetworkLossWeight  int

	// Network chaos parameters
	NetworkLatency     string
	NetworkJitter      string
	NetworkLossPercent string
}

// DefaultConfig returns a Config with default values.
func DefaultConfig() *Config {
	return &Config{
		Interval:           defaultChaosInterval,
		MinDuration:        defaultChaosMinDuration,
		MaxDuration:        defaultChaosMaxDuration,
		PodKillWeight:      defaultPodKillWeight,
		NetworkDelayWeight: defaultNetworkDelayWeight,
		NetworkLossWeight:  defaultNetworkLossWeight,
		NetworkLatency:     defaultNetworkLatency,
		NetworkJitter:      defaultNetworkJitter,
		NetworkLossPercent: defaultNetworkLossPercent,
	}
}

// FlagVars holds flag variables for chaos configuration.
type FlagVars struct {
	Interval           time.Duration
	MinDuration        time.Duration
	MaxDuration        time.Duration
	PodKillWeight      int
	NetworkDelayWeight int
	NetworkLossWeight  int
	NetworkLatency     string
	NetworkJitter      string
	NetworkLossPercent string
}

// RegisterFlags registers command-line flags for chaos configuration.
func RegisterFlags() *FlagVars {
	vars := &FlagVars{}

	flag.DurationVar(&vars.Interval, "chaos-interval", defaultChaosInterval,
		"Interval between fault injections")
	flag.DurationVar(&vars.MinDuration, "chaos-min-duration", defaultChaosMinDuration,
		"Minimum duration for a chaos experiment")
	flag.DurationVar(&vars.MaxDuration, "chaos-max-duration", defaultChaosMaxDuration,
		"Maximum duration for a chaos experiment")

	flag.IntVar(&vars.PodKillWeight, "chaos-pod-kill-weight", defaultPodKillWeight,
		"Weight for pod-kill experiments (relative probability)")
	flag.IntVar(&vars.NetworkDelayWeight, "chaos-network-delay-weight", defaultNetworkDelayWeight,
		"Weight for network-delay experiments (relative probability)")
	flag.IntVar(&vars.NetworkLossWeight, "chaos-network-loss-weight", defaultNetworkLossWeight,
		"Weight for network-loss experiments (relative probability)")

	flag.StringVar(&vars.NetworkLatency, "chaos-network-latency", defaultNetworkLatency,
		"Latency to inject for network-delay experiments")
	flag.StringVar(&vars.NetworkJitter, "chaos-network-jitter", defaultNetworkJitter,
		"Jitter to inject for network-delay experiments")
	flag.StringVar(&vars.NetworkLossPercent, "chaos-network-loss-percent", defaultNetworkLossPercent,
		"Packet loss percentage for network-loss experiments")

	return vars
}

// ToConfig converts FlagVars to a Config.
func (v *FlagVars) ToConfig() *Config {
	return &Config{
		Interval:           v.Interval,
		MinDuration:        v.MinDuration,
		MaxDuration:        v.MaxDuration,
		PodKillWeight:      v.PodKillWeight,
		NetworkDelayWeight: v.NetworkDelayWeight,
		NetworkLossWeight:  v.NetworkLossWeight,
		NetworkLatency:     v.NetworkLatency,
		NetworkJitter:      v.NetworkJitter,
		NetworkLossPercent: v.NetworkLossPercent,
	}
}

// TotalWeight returns the total weight of all experiment types.
func (c *Config) TotalWeight() int {
	return c.PodKillWeight + c.NetworkDelayWeight + c.NetworkLossWeight
}
