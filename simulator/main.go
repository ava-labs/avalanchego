package main

import (
	"fmt"
	"log"

	"github.com/spf13/viper"
)

const (
	endpointsKey      = "endpoints"
	maxBaseFeeKey     = "max-base-fee"
	maxPriorityFeeKey = "max-priority-fee"
	concurrencyKey    = "concurrency"
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
	maxBaseFee := v.GetUint64(maxBaseFeeKey)
	if maxBaseFee == 0 {
		log.Fatal("max base fee is 0")
	}
	// We allow a priority fee of 0, so we don't check this
	maxPriorityFee := v.GetUint64(maxPriorityFeeKey)
	concurrency := v.GetInt(concurrencyKey)
	if concurrency == 0 {
		log.Fatal("concurrency is 0")
	}
	log.Printf(
		"starting simulator (endpoints=%v max base fee=%d max priority fee=%d concurrency=%d)\n",
		endpoints,
		maxBaseFee,
		maxPriorityFee,
		concurrency,
	)
}
