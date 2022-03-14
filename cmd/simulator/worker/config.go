// (c) 2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package worker

import (
	"fmt"
	"log"

	"github.com/spf13/viper"
)

const (
	endpointsKey   = "endpoints"
	baseFeeKey     = "base-fee"
	priorityFeeKey = "priority-fee"
	concurrencyKey = "concurrency"
)

type Config struct {
	Endpoints   []string
	Concurrency int
	BaseFee     uint64
	PriorityFee uint64
}

// LoadConfig parses and validates the [config] in [.simulator]
func LoadConfig() (*Config, error) {
	v := viper.New()
	v.SetConfigName("config")
	v.AddConfigPath(".simulator")
	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("%w: unable to read config", err)
	}
	endpoints := v.GetStringSlice(endpointsKey)
	if len(endpoints) == 0 {
		log.Fatal("no available endpoints")
	}
	concurrency := v.GetInt(concurrencyKey)
	if concurrency == 0 {
		log.Fatal("concurrency is 0")
	}
	baseFee := v.GetUint64(baseFeeKey)
	if baseFee == 0 {
		log.Fatal("base fee is 0")
	}
	// We allow a priority fee of 0, so we don't check this
	priorityFee := v.GetUint64(priorityFeeKey)
	log.Printf(
		"loaded config (endpoints=%v concurrency=%d base fee=%d priority fee=%d)\n",
		endpoints,
		baseFee,
		priorityFee,
		concurrency,
	)
	return &Config{
		Endpoints:   endpoints,
		Concurrency: concurrency,
		BaseFee:     baseFee,
		PriorityFee: priorityFee,
	}, nil
}
