// (c) 2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package worker

import (
	"log"
	"os"
	"path/filepath"

	"sigs.k8s.io/yaml"
)

type Config struct {
	Endpoints   []string `json:"endpoints"`
	Concurrency int      `json:"concurrency"`
	BaseFee     uint64   `json:"base-fee"`
	PriorityFee uint64   `json:"priority-fee"`
}

// LoadConfig parses and validates the [config] in [.simulator]
func LoadConfig(configPath string) (cfg *Config, err error) {
	var d []byte
	d, err = os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}
	cfg = &Config{}
	if err = yaml.Unmarshal(d, cfg); err != nil {
		return nil, err
	}
	log.Printf(
		"loaded config (endpoints=%v concurrency=%d base fee=%d priority fee=%d)\n",
		cfg.Endpoints,
		cfg.BaseFee,
		cfg.PriorityFee,
		cfg.Concurrency,
	)
	return cfg, nil
}

const fsModeWrite = 0o600

func (c *Config) Save(p string) error {
	log.Printf("saving config to %q", p)
	if err := os.MkdirAll(filepath.Dir(p), fsModeWrite); err != nil {
		return err
	}
	ob, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	return os.WriteFile(p, ob, fsModeWrite)
}
