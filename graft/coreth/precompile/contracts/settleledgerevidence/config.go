package settleledger

import "github.com/ava-labs/avalanchego/graft/coreth/precompile/precompileconfig"

var _ precompileconfig.Config = (*Config)(nil)

// Config carries no settings of its own: the probe is either active or absent,
// and what it measures is chosen per call rather than per chain.
type Config struct {
	precompileconfig.Upgrade
}

func NewConfig(blockTimestamp *uint64) *Config {
	return &Config{Upgrade: precompileconfig.Upgrade{BlockTimestamp: blockTimestamp}}
}

func NewDisableConfig(blockTimestamp *uint64) *Config {
	return &Config{Upgrade: precompileconfig.Upgrade{BlockTimestamp: blockTimestamp, Disable: true}}
}

func (*Config) Key() string                               { return ConfigKey }
func (*Config) Verify(precompileconfig.ChainConfig) error { return nil }

func (c *Config) Equal(other precompileconfig.Config) bool {
	o, ok := other.(*Config)
	return ok && c.Upgrade.Equal(&o.Upgrade)
}
