package main

import (
	"context"
	"fmt"
	"log"

	"github.com/spf13/viper"

	"github.com/ava-labs/subnet-evm/cmd/simulator/worker"
)

const (
	endpointsKey   = "endpoints"
	chainIdKey     = "chain-id"
	baseFeeKey     = "base-fee"
	priorityFeeKey = "priority-fee"
	concurrencyKey = "concurrency"
)

func loadConfig() (*viper.Viper, error) {
	v := viper.New()
	v.SetConfigName("config")
	v.AddConfigPath(".simulator")

	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("%w: unable to read config", err)
	}
	return v, nil
}

func main() {
	v, err := loadConfig()
	if err != nil {
		log.Fatal(err)
	}
	endpoints := v.GetStringSlice(endpointsKey)
	if len(endpoints) == 0 {
		log.Fatal("no available endpoints")
	}
	chainId := v.GetUint64(chainIdKey)
	if chainId == 0 {
		log.Fatal("chainID is 0")
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
		"starting simulator (endpoints=%v chainID=%d concurrency=%d base fee=%d priority fee=%d)\n",
		endpoints,
		chainId,
		baseFee,
		priorityFee,
		concurrency,
	)
	ctx := context.Background()
	log.Fatal(worker.Run(ctx, endpoints, chainId, concurrency, baseFee, priorityFee))
}
