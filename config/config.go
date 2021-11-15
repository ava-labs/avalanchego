// (c) 2021 Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package config

import (
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"math"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/viper"

	"github.com/ava-labs/avalanchego/app/runner"
	"github.com/ava-labs/avalanchego/chains"
	"github.com/ava-labs/avalanchego/genesis"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/ipcs"
	"github.com/ava-labs/avalanchego/nat"
	"github.com/ava-labs/avalanchego/network"
	"github.com/ava-labs/avalanchego/network/dialer"
	"github.com/ava-labs/avalanchego/network/throttling"
	"github.com/ava-labs/avalanchego/node"
	"github.com/ava-labs/avalanchego/snow/consensus/avalanche"
	"github.com/ava-labs/avalanchego/snow/consensus/snowball"
	"github.com/ava-labs/avalanchego/snow/networking/benchlist"
	"github.com/ava-labs/avalanchego/snow/networking/router"
	"github.com/ava-labs/avalanchego/staking"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/dynamicip"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/password"
	"github.com/ava-labs/avalanchego/utils/profiler"
	"github.com/ava-labs/avalanchego/utils/storage"
	"github.com/ava-labs/avalanchego/utils/timer"
	"github.com/ava-labs/avalanchego/utils/ulimit"
)

const (
	pluginsDirName       = "plugins"
	chainConfigFileName  = "config"
	chainUpgradeFileName = "upgrade"
	subnetConfigFileExt  = ".json"
)

var (
	deprecatedKeys = map[string]string{
		InboundConnUpgradeThrottlerMaxRecentKey: fmt.Sprintf("please use --%s to specify connection upgrade throttling", InboundThrottlerMaxConnsPerSecKey),
	}

	errInvalidStakerWeights          = errors.New("staking weights must be positive")
	errAuthPasswordTooWeak           = errors.New("API auth password is not strong enough")
	errInvalidUptimeRequirement      = errors.New("uptime requirement must be in the range [0, 1]")
	errMinValidatorStakeAboveMax     = errors.New("minimum validator stake can't be greater than maximum validator stake")
	errInvalidDelegationFee          = errors.New("delegation fee must be in the range [0, 1,000,000]")
	errInvalidMinStakeDuration       = errors.New("min stake duration must be > 0")
	errMinStakeDurationAboveMax      = errors.New("max stake duration can't be less than min stake duration")
	errStakeMintingPeriodBelowMin    = errors.New("stake minting period can't be less than max stake duration")
	errCannotWhitelistPrimaryNetwork = errors.New("cannot whitelist primary network")
)

func GetRunnerConfig(v *viper.Viper) (runner.Config, error) {
	config := runner.Config{
		DisplayVersionAndExit: v.GetBool(VersionKey),
		BuildDir:              os.ExpandEnv(v.GetString(BuildDirKey)),
		PluginMode:            v.GetBool(PluginModeKey),
	}

	// Build directory should have this structure:
	//
	// build
	// ├── avalanchego (the binary from compiling the app directory)
	// └── plugins
	//     └── evm
	validBuildDir := func(dir string) bool {
		info, err := os.Stat(dir)
		if err != nil || !info.IsDir() {
			return false
		}

		// make sure the expected subdirectory exists
		_, err = os.Stat(filepath.Join(dir, pluginsDirName))
		return err == nil
	}
	if validBuildDir(config.BuildDir) {
		return config, nil
	}

	foundBuildDir := false
	for _, dir := range defaultBuildDirs {
		if validBuildDir(dir) {
			config.BuildDir = dir
			foundBuildDir = true
			break
		}
	}
	if !foundBuildDir {
		return runner.Config{}, fmt.Errorf(
			"couldn't find valid build directory in any of the default locations: %s",
			defaultBuildDirs,
		)
	}
	return config, nil
}

func getConsensusConfig(v *viper.Viper) avalanche.Parameters {
	return avalanche.Parameters{
		Parameters: snowball.Parameters{
			K:                     v.GetInt(SnowSampleSizeKey),
			Alpha:                 v.GetInt(SnowQuorumSizeKey),
			BetaVirtuous:          v.GetInt(SnowVirtuousCommitThresholdKey),
			BetaRogue:             v.GetInt(SnowRogueCommitThresholdKey),
			ConcurrentRepolls:     v.GetInt(SnowConcurrentRepollsKey),
			OptimalProcessing:     v.GetInt(SnowOptimalProcessingKey),
			MaxOutstandingItems:   v.GetInt(SnowMaxProcessingKey),
			MaxItemProcessingTime: v.GetDuration(SnowMaxTimeProcessingKey),
		},
		BatchSize: v.GetInt(SnowAvalancheBatchSizeKey),
		Parents:   v.GetInt(SnowAvalancheNumParentsKey),
	}
}

func getLoggingConfig(v *viper.Viper) (logging.Config, error) {
	loggingConfig, err := logging.DefaultConfig()
	if err != nil {
		return loggingConfig, err
	}
	if v.IsSet(LogsDirKey) {
		loggingConfig.Directory = os.ExpandEnv(v.GetString(LogsDirKey))
	}
	loggingConfig.LogLevel, err = logging.ToLevel(v.GetString(LogLevelKey))
	if err != nil {
		return loggingConfig, err
	}
	logDisplayLevel := v.GetString(LogLevelKey)
	if v.IsSet(LogDisplayLevelKey) {
		logDisplayLevel = v.GetString(LogDisplayLevelKey)
	}
	loggingConfig.DisplayLevel, err = logging.ToLevel(logDisplayLevel)
	if err != nil {
		return loggingConfig, err
	}
	loggingConfig.DisplayHighlight, err = logging.ToHighlight(v.GetString(LogDisplayHighlightKey), os.Stdout.Fd())
	return loggingConfig, err
}

func getAPIAuthConfig(v *viper.Viper) (node.APIAuthConfig, error) {
	config := node.APIAuthConfig{
		APIRequireAuthToken: v.GetBool(APIAuthRequiredKey),
	}
	if !config.APIRequireAuthToken {
		return config, nil
	}
	passwordFilePath := v.GetString(APIAuthPasswordFileKey)
	pwBytes, err := ioutil.ReadFile(passwordFilePath)
	if err != nil {
		return node.APIAuthConfig{}, fmt.Errorf("API auth password file %q failed to be read: %w", passwordFilePath, err)
	}
	config.APIAuthPassword = strings.TrimSpace(string(pwBytes))
	if !password.SufficientlyStrong(config.APIAuthPassword, password.OK) {
		return node.APIAuthConfig{}, errAuthPasswordTooWeak
	}
	return config, nil
}

func getIPCConfig(v *viper.Viper) node.IPCConfig {
	config := node.IPCConfig{
		IPCAPIEnabled: v.GetBool(IpcAPIEnabledKey),
		IPCPath:       ipcs.DefaultBaseURL,
	}
	if v.IsSet(IpcsChainIDsKey) {
		config.IPCDefaultChainIDs = strings.Split(v.GetString(IpcsChainIDsKey), ",")
	}
	if v.IsSet(IpcsPathKey) {
		config.IPCPath = os.ExpandEnv(v.GetString(IpcsPathKey))
	}
	return config
}

func getHTTPConfig(v *viper.Viper) (node.HTTPConfig, error) {
	config := node.HTTPConfig{
		APIConfig: node.APIConfig{
			APIIndexerConfig: node.APIIndexerConfig{
				IndexAPIEnabled:      v.GetBool(IndexEnabledKey),
				IndexAllowIncomplete: v.GetBool(IndexAllowIncompleteKey),
			},
			AdminAPIEnabled:    v.GetBool(AdminAPIEnabledKey),
			InfoAPIEnabled:     v.GetBool(InfoAPIEnabledKey),
			KeystoreAPIEnabled: v.GetBool(KeystoreAPIEnabledKey),
			MetricsAPIEnabled:  v.GetBool(MetricsAPIEnabledKey),
			HealthAPIEnabled:   v.GetBool(HealthAPIEnabledKey),
		},
		HTTPHost:          v.GetString(HTTPHostKey),
		HTTPPort:          uint16(v.GetUint(HTTPPortKey)),
		HTTPSEnabled:      v.GetBool(HTTPSEnabledKey),
		HTTPSKeyFile:      os.ExpandEnv(v.GetString(HTTPSKeyFileKey)),
		HTTPSCertFile:     os.ExpandEnv(v.GetString(HTTPSCertFileKey)),
		APIAllowedOrigins: v.GetStringSlice(HTTPAllowedOrigins),
	}
	var err error
	config.APIAuthConfig, err = getAPIAuthConfig(v)
	if err != nil {
		return node.HTTPConfig{}, err
	}
	config.IPCConfig = getIPCConfig(v)
	return config, nil
}

func getRouterHealthConfig(v *viper.Viper, halflife time.Duration) (router.HealthConfig, error) {
	config := router.HealthConfig{
		MaxDropRate:            v.GetFloat64(RouterHealthMaxDropRateKey),
		MaxOutstandingRequests: int(v.GetUint(RouterHealthMaxOutstandingRequestsKey)),
		MaxOutstandingDuration: v.GetDuration(NetworkHealthMaxOutstandingDurationKey),
		MaxRunTimeRequests:     v.GetDuration(NetworkMaximumTimeoutKey),
		MaxDropRateHalflife:    halflife,
	}
	switch {
	case config.MaxDropRate < 0 || config.MaxDropRate > 1:
		return router.HealthConfig{}, fmt.Errorf("%q must be in [0,1]", RouterHealthMaxDropRateKey)
	case config.MaxOutstandingDuration <= 0:
		return router.HealthConfig{}, fmt.Errorf("%q must be positive", NetworkHealthMaxOutstandingDurationKey)
	case config.MaxRunTimeRequests <= 0:
		return router.HealthConfig{}, fmt.Errorf("%q must be positive", NetworkMaximumTimeoutKey)
	}
	return config, nil
}

func getAdaptiveTimeoutConfig(v *viper.Viper) (timer.AdaptiveTimeoutConfig, error) {
	config := timer.AdaptiveTimeoutConfig{
		InitialTimeout:     v.GetDuration(NetworkInitialTimeoutKey),
		MinimumTimeout:     v.GetDuration(NetworkMinimumTimeoutKey),
		MaximumTimeout:     v.GetDuration(NetworkMaximumTimeoutKey),
		TimeoutHalflife:    v.GetDuration(NetworkTimeoutHalflifeKey),
		TimeoutCoefficient: v.GetFloat64(NetworkTimeoutCoefficientKey),
	}
	switch {
	case config.MinimumTimeout < 1:
		return timer.AdaptiveTimeoutConfig{}, fmt.Errorf("%q must be positive", NetworkMinimumTimeoutKey)
	case config.MinimumTimeout > config.MaximumTimeout:
		return timer.AdaptiveTimeoutConfig{}, fmt.Errorf("%q must be >= %q", NetworkMaximumTimeoutKey, NetworkMinimumTimeoutKey)
	case config.InitialTimeout < config.MinimumTimeout || config.InitialTimeout > config.MaximumTimeout:
		return timer.AdaptiveTimeoutConfig{}, fmt.Errorf("%q must be in [%q, %q]", NetworkInitialTimeoutKey, NetworkMinimumTimeoutKey, NetworkMaximumTimeoutKey)
	case config.TimeoutHalflife <= 0:
		return timer.AdaptiveTimeoutConfig{}, fmt.Errorf("%q must > 0", NetworkTimeoutHalflifeKey)
	case config.TimeoutCoefficient < 1:
		return timer.AdaptiveTimeoutConfig{}, fmt.Errorf("%q must be >= 1", NetworkTimeoutCoefficientKey)
	}

	return config, nil
}

func getNetworkConfig(v *viper.Viper, halflife time.Duration) (network.Config, error) {
	// Set the max number of recent inbound connections upgraded to be
	// equal to the max number of inbound connections per second.
	maxInboundConnsPerSec := v.GetFloat64(InboundThrottlerMaxConnsPerSecKey)
	upgradeCooldown := v.GetDuration(InboundConnUpgradeThrottlerCooldownKey)
	upgradeCooldownInSeconds := upgradeCooldown.Seconds()
	maxRecentConnsUpgraded := int(math.Ceil(maxInboundConnsPerSec * upgradeCooldownInSeconds))
	config := network.Config{
		// Throttling
		ThrottlerConfig: network.ThrottlerConfig{
			MaxIncomingConnsPerSec: maxInboundConnsPerSec,
			InboundConnUpgradeThrottlerConfig: throttling.InboundConnUpgradeThrottlerConfig{
				UpgradeCooldown:        upgradeCooldown,
				MaxRecentConnsUpgraded: maxRecentConnsUpgraded,
			},

			InboundMsgThrottlerConfig: throttling.InboundMsgThrottlerConfig{
				MsgByteThrottlerConfig: throttling.MsgByteThrottlerConfig{
					AtLargeAllocSize:    v.GetUint64(InboundThrottlerAtLargeAllocSizeKey),
					VdrAllocSize:        v.GetUint64(InboundThrottlerVdrAllocSizeKey),
					NodeMaxAtLargeBytes: v.GetUint64(InboundThrottlerNodeMaxAtLargeBytesKey),
				},
				BandwidthThrottlerConfig: throttling.BandwidthThrottlerConfig{
					RefillRate:   v.GetUint64(InboundThrottlerBandwidthRefillRateKey),
					MaxBurstSize: v.GetUint64(InboundThrottlerBandwidthMaxBurstSizeKey),
				},
				MaxProcessingMsgsPerNode: v.GetUint64(InboundThrottlerMaxProcessingMsgsPerNodeKey),
			},

			OutboundMsgThrottlerConfig: throttling.MsgByteThrottlerConfig{
				AtLargeAllocSize:    v.GetUint64(OutboundThrottlerAtLargeAllocSizeKey),
				VdrAllocSize:        v.GetUint64(OutboundThrottlerVdrAllocSizeKey),
				NodeMaxAtLargeBytes: v.GetUint64(OutboundThrottlerNodeMaxAtLargeBytesKey),
			},
		},

		HealthConfig: network.HealthConfig{
			MaxTimeSinceMsgSent:          v.GetDuration(NetworkHealthMaxTimeSinceMsgSentKey),
			MaxTimeSinceMsgReceived:      v.GetDuration(NetworkHealthMaxTimeSinceMsgReceivedKey),
			MaxPortionSendQueueBytesFull: v.GetFloat64(NetworkHealthMaxPortionSendQueueFillKey),
			MinConnectedPeers:            v.GetUint(NetworkHealthMinPeersKey),
			MaxSendFailRate:              v.GetFloat64(NetworkHealthMaxSendFailRateKey),
			MaxSendFailRateHalflife:      halflife,
		},

		DialerConfig: dialer.Config{
			ThrottleRps:       v.GetUint32(OutboundConnectionThrottlingRps),
			ConnectionTimeout: v.GetDuration(OutboundConnectionTimeout),
		},

		TimeoutConfig: network.TimeoutConfig{
			PeerAliasTimeout:     v.GetDuration(PeerAliasTimeoutKey),
			GetVersionTimeout:    v.GetDuration(NetworkGetVersionTimeoutKey),
			PingPongTimeout:      v.GetDuration(NetworkPingTimeoutKey),
			ReadHandshakeTimeout: v.GetDuration(NetworkReadHandshakeTimeoutKey),
		},

		PeerListGossipConfig: network.PeerListGossipConfig{
			PeerListSize:                 v.GetUint32(NetworkPeerListSizeKey),
			PeerListGossipFreq:           v.GetDuration(NetworkPeerListGossipFreqKey),
			PeerListGossipSize:           v.GetUint32(NetworkPeerListGossipSizeKey),
			PeerListStakerGossipFraction: v.GetUint32(NetworkPeerListStakerGossipFractionKey),
		},

		GossipConfig: network.GossipConfig{
			GossipAcceptedFrontierSize: uint(v.GetUint32(ConsensusGossipAcceptedFrontierSizeKey)),
			GossipOnAcceptSize:         uint(v.GetUint32(ConsensusGossipOnAcceptSizeKey)),
			AppGossipNonValidatorSize:  uint(v.GetUint32(AppGossipNonValidatorSizeKey)),
			AppGossipValidatorSize:     uint(v.GetUint32(AppGossipValidatorSizeKey)),
		},

		DelayConfig: network.DelayConfig{
			MaxReconnectDelay:     v.GetDuration(NetworkMaxReconnectDelayKey),
			InitialReconnectDelay: v.GetDuration(NetworkInitialReconnectDelayKey),
		},

		MaxClockDifference: v.GetDuration(NetworkMaxClockDifferenceKey),
		CompressionEnabled: v.GetBool(NetworkCompressionEnabledKey),
		PingFrequency:      v.GetDuration(NetworkPingFrequencyKey),
		AllowPrivateIPs:    v.GetBool(NetworkAllowPrivateIPsKey),

		RequireValidatorToConnect: v.GetBool(NetworkRequireValidatorToConnectKey),
	}

	switch {
	case config.HealthConfig.MaxTimeSinceMsgSent < 0:
		return network.Config{}, fmt.Errorf("%s must be >= 0", NetworkHealthMaxTimeSinceMsgSentKey)
	case config.HealthConfig.MaxTimeSinceMsgReceived < 0:
		return network.Config{}, fmt.Errorf("%s must be >= 0", NetworkHealthMaxTimeSinceMsgReceivedKey)
	case config.HealthConfig.MaxSendFailRate < 0 || config.HealthConfig.MaxSendFailRate > 1:
		return network.Config{}, fmt.Errorf("%s must be in [0,1]", NetworkHealthMaxSendFailRateKey)
	case config.HealthConfig.MaxPortionSendQueueBytesFull < 0 || config.HealthConfig.MaxPortionSendQueueBytesFull > 1:
		return network.Config{}, fmt.Errorf("%s must be in [0,1]", NetworkHealthMaxPortionSendQueueFillKey)
	case config.DialerConfig.ConnectionTimeout < 0:
		return network.Config{}, fmt.Errorf("%q must be >= 0", OutboundConnectionTimeout)
	case config.PeerAliasTimeout < 0:
		return network.Config{}, fmt.Errorf("%q must be >= 0", PeerAliasTimeoutKey)
	case config.PeerListGossipFreq < 0:
		return network.Config{}, fmt.Errorf("%s must be >= 0", NetworkPeerListGossipFreqKey)
	case config.GetVersionTimeout < 0:
		return network.Config{}, fmt.Errorf("%s must be >= 0", NetworkGetVersionTimeoutKey)
	case config.PeerListStakerGossipFraction < 1:
		return network.Config{}, fmt.Errorf("%s must be >= 1", NetworkPeerListStakerGossipFractionKey)
	case config.MaxReconnectDelay < 0:
		return network.Config{}, fmt.Errorf("%s must be >= 0", NetworkMaxReconnectDelayKey)
	case config.InitialReconnectDelay < 0:
		return network.Config{}, fmt.Errorf("%s must be >= 0", NetworkInitialReconnectDelayKey)
	case config.MaxReconnectDelay < config.InitialReconnectDelay:
		return network.Config{}, fmt.Errorf("%s must be >= %s", NetworkMaxReconnectDelayKey, NetworkInitialReconnectDelayKey)
	case config.PingPongTimeout < 0:
		return network.Config{}, fmt.Errorf("%s must be >= 0", NetworkPingTimeoutKey)
	case config.PingFrequency < 0:
		return network.Config{}, fmt.Errorf("%s must be >= 0", NetworkPingFrequencyKey)
	case config.PingPongTimeout <= config.PingFrequency:
		return network.Config{}, fmt.Errorf("%s must be > %s", NetworkPingTimeoutKey, NetworkPingFrequencyKey)
	case config.ReadHandshakeTimeout < 0:
		return network.Config{}, fmt.Errorf("%s must be >= 0", NetworkReadHandshakeTimeoutKey)
	case config.MaxClockDifference < 0:
		return network.Config{}, fmt.Errorf("%s must be >= 0", NetworkMaxClockDifferenceKey)
	}

	return config, nil
}

func getBenchlistConfig(v *viper.Viper, alpha, k int) (benchlist.Config, error) {
	config := benchlist.Config{
		Threshold:              v.GetInt(BenchlistFailThresholdKey),
		PeerSummaryEnabled:     v.GetBool(BenchlistPeerSummaryEnabledKey),
		Duration:               v.GetDuration(BenchlistDurationKey),
		MinimumFailingDuration: v.GetDuration(BenchlistMinFailingDurationKey),
		MaxPortion:             (1.0 - (float64(alpha) / float64(k))) / 3.0,
	}
	switch {
	case config.Duration < 0:
		return benchlist.Config{}, fmt.Errorf("%q must be >= 0", BenchlistDurationKey)
	case config.MinimumFailingDuration < 0:
		return benchlist.Config{}, fmt.Errorf("%q must be >= 0", BenchlistMinFailingDurationKey)
	}
	return config, nil
}

func getBootstrapConfig(v *viper.Viper, networkID uint32) (node.BootstrapConfig, error) {
	config := node.BootstrapConfig{
		RetryBootstrap:                         v.GetBool(RetryBootstrapKey),
		RetryBootstrapWarnFrequency:            v.GetInt(RetryBootstrapWarnFrequencyKey),
		BootstrapBeaconConnectionTimeout:       v.GetDuration(BootstrapBeaconConnectionTimeoutKey),
		BootstrapMaxTimeGetAncestors:           v.GetDuration(BootstrapMaxTimeGetAncestorsKey),
		BootstrapMultiputMaxContainersSent:     int(v.GetUint(BootstrapMultiputMaxContainersSentKey)),
		BootstrapMultiputMaxContainersReceived: int(v.GetUint(BootstrapMultiputMaxContainersReceivedKey)),
	}

	bootstrapIPs, bootstrapIDs := genesis.SampleBeacons(networkID, 5)
	if v.IsSet(BootstrapIPsKey) {
		bootstrapIPs = strings.Split(v.GetString(BootstrapIPsKey), ",")
	}
	for _, ip := range bootstrapIPs {
		if ip == "" {
			continue
		}
		addr, err := utils.ToIPDesc(ip)
		if err != nil {
			return node.BootstrapConfig{}, fmt.Errorf("couldn't parse bootstrap ip %s: %w", ip, err)
		}
		config.BootstrapIPs = append(config.BootstrapIPs, addr)
	}

	if v.IsSet(BootstrapIDsKey) {
		bootstrapIDs = strings.Split(v.GetString(BootstrapIDsKey), ",")
	}
	for _, id := range bootstrapIDs {
		if id == "" {
			continue
		}
		nodeID, err := ids.ShortFromPrefixedString(id, constants.NodeIDPrefix)
		if err != nil {
			return node.BootstrapConfig{}, fmt.Errorf("couldn't parse bootstrap peer id: %w", err)
		}
		config.BootstrapIDs = append(config.BootstrapIDs, nodeID)
	}
	return config, nil
}

func getIPConfig(v *viper.Viper) (node.IPConfig, error) {
	config := node.IPConfig{}
	// Resolves our public IP, or does nothing
	config.DynamicPublicIPResolver = dynamicip.NewResolver(v.GetString(DynamicPublicIPResolverKey))
	config.DynamicUpdateDuration = v.GetDuration(DynamicUpdateDurationKey)
	if config.DynamicUpdateDuration < 0 {
		return node.IPConfig{}, fmt.Errorf("%q must be <= 0", DynamicUpdateDurationKey)
	}

	var (
		ip  net.IP
		err error
	)
	publicIP := v.GetString(PublicIPKey)
	switch {
	case config.DynamicPublicIPResolver.IsResolver():
		// User specified to use dynamic IP resolution; don't use NAT traversal
		config.Nat = nat.NewNoRouter()
		ip, err = dynamicip.FetchExternalIP(config.DynamicPublicIPResolver)
		if err != nil {
			return node.IPConfig{}, fmt.Errorf("dynamic ip address fetch failed: %s", err)
		}
	case publicIP == "":
		// User didn't specify a public IP to use; try with NAT traversal
		config.AttemptedNATTraversal = true
		config.Nat = nat.GetRouter()
		ip, err = config.Nat.ExternalIP()
		if err != nil {
			ip = net.IPv4zero // Couldn't get my IP...set to 0.0.0.0
		}
	default:
		// User specified a public IP to use; don't use NAT
		config.Nat = nat.NewNoRouter()
		ip = net.ParseIP(publicIP)
	}
	if ip == nil {
		return node.IPConfig{}, fmt.Errorf("invalid IP Address %s", publicIP)
	}

	stakingPort := uint16(v.GetUint(StakingPortKey))
	config.IP = utils.NewDynamicIPDesc(ip, stakingPort)
	return config, nil
}

func getProfilerConfig(v *viper.Viper) (profiler.Config, error) {
	config := profiler.Config{
		Dir:         os.ExpandEnv(v.GetString(ProfileDirKey)),
		Enabled:     v.GetBool(ProfileContinuousEnabledKey),
		Freq:        v.GetDuration(ProfileContinuousFreqKey),
		MaxNumFiles: v.GetInt(ProfileContinuousMaxFilesKey),
	}
	if config.Freq < 0 {
		return profiler.Config{}, fmt.Errorf("%s must be >= 0", ProfileContinuousFreqKey)
	}
	return config, nil
}

func getStakingTLSCert(v *viper.Viper) (tls.Certificate, error) {
	if v.GetBool(StakingEphemeralCertEnabledKey) {
		// Use an ephemeral staking key/cert
		cert, err := staking.NewTLSCert()
		if err != nil {
			return tls.Certificate{}, fmt.Errorf("couldn't generate ephemeral staking key/cert: %w", err)
		}
		return *cert, nil
	}

	// Parse the staking key/cert paths and expand environment variables
	stakingKeyPath := os.ExpandEnv(v.GetString(StakingKeyPathKey))
	stakingCertPath := os.ExpandEnv(v.GetString(StakingCertPathKey))

	// If staking key/cert locations are specified but not found, error
	if v.IsSet(StakingKeyPathKey) || v.IsSet(StakingCertPathKey) {
		if _, err := os.Stat(stakingKeyPath); os.IsNotExist(err) {
			return tls.Certificate{}, fmt.Errorf("couldn't find staking key at %s", stakingKeyPath)
		} else if _, err := os.Stat(stakingCertPath); os.IsNotExist(err) {
			return tls.Certificate{}, fmt.Errorf("couldn't find staking certificate at %s", stakingCertPath)
		}
	} else {
		// Create the staking key/cert if [stakingKeyPath] and [stakingCertPath] don't exist
		if err := staking.InitNodeStakingKeyPair(stakingKeyPath, stakingCertPath); err != nil {
			return tls.Certificate{}, fmt.Errorf("couldn't generate staking key/cert: %w", err)
		}
	}

	// Load and parse the staking key/cert
	cert, err := staking.LoadTLSCert(stakingKeyPath, stakingCertPath)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("couldn't read staking certificate: %w", err)
	}
	return *cert, nil
}

func getStakingConfig(v *viper.Viper, networkID uint32) (node.StakingConfig, error) {
	config := node.StakingConfig{
		EnableStaking:         v.GetBool(StakingEnabledKey),
		DisabledStakingWeight: v.GetUint64(StakingDisabledWeightKey),
		StakingKeyPath:        os.ExpandEnv(v.GetString(StakingKeyPathKey)),
		StakingCertPath:       os.ExpandEnv(v.GetString(StakingCertPathKey)),
	}
	if !config.EnableStaking && config.DisabledStakingWeight == 0 {
		return node.StakingConfig{}, errInvalidStakerWeights
	}

	var err error
	config.StakingTLSCert, err = getStakingTLSCert(v)
	if err != nil {
		return node.StakingConfig{}, err
	}
	if networkID != constants.MainnetID && networkID != constants.FujiID {
		config.UptimeRequirement = v.GetFloat64(UptimeRequirementKey)
		config.MinValidatorStake = v.GetUint64(MinValidatorStakeKey)
		config.MaxValidatorStake = v.GetUint64(MaxValidatorStakeKey)
		config.MinDelegatorStake = v.GetUint64(MinDelegatorStakeKey)
		config.MinStakeDuration = v.GetDuration(MinStakeDurationKey)
		config.MaxStakeDuration = v.GetDuration(MaxStakeDurationKey)
		config.StakeMintingPeriod = v.GetDuration(StakeMintingPeriodKey)
		config.MinDelegationFee = v.GetUint32(MinDelegatorFeeKey)
		switch {
		case config.UptimeRequirement < 0 || config.UptimeRequirement > 1:
			return node.StakingConfig{}, errInvalidUptimeRequirement
		case config.MinValidatorStake > config.MaxValidatorStake:
			return node.StakingConfig{}, errMinValidatorStakeAboveMax
		case config.MinDelegationFee > 1_000_000:
			return node.StakingConfig{}, errInvalidDelegationFee
		case config.MinStakeDuration <= 0:
			return node.StakingConfig{}, errInvalidMinStakeDuration
		case config.MaxStakeDuration < config.MinStakeDuration:
			return node.StakingConfig{}, errMinStakeDurationAboveMax
		case config.StakeMintingPeriod < config.MaxStakeDuration:
			return node.StakingConfig{}, errStakeMintingPeriodBelowMin
		}
	} else {
		config.StakingConfig = genesis.GetStakingConfig(networkID)
	}
	return config, nil
}

func getTxFeeConfig(v *viper.Viper, networkID uint32) genesis.TxFeeConfig {
	if networkID != constants.MainnetID && networkID != constants.FujiID {
		return genesis.TxFeeConfig{
			TxFee:                 v.GetUint64(TxFeeKey),
			CreateAssetTxFee:      v.GetUint64(CreateAssetTxFeeKey),
			CreateSubnetTxFee:     v.GetUint64(CreateSubnetTxFeeKey),
			CreateBlockchainTxFee: v.GetUint64(CreateBlockchainTxFeeKey),
		}
	}
	return genesis.GetTxFeeConfig(networkID)
}

func getWhitelistedSubnets(v *viper.Viper) (ids.Set, error) {
	whitelistedSubnetIDs := ids.Set{}
	for _, subnet := range strings.Split(v.GetString(WhitelistedSubnetsKey), ",") {
		if subnet == "" {
			continue
		}
		subnetID, err := ids.FromString(subnet)
		if err != nil {
			return nil, fmt.Errorf("couldn't parse subnetID %q: %w", subnet, err)
		}
		if subnetID == constants.PrimaryNetworkID {
			return nil, errCannotWhitelistPrimaryNetwork
		}
		whitelistedSubnetIDs.Add(subnetID)
	}
	return whitelistedSubnetIDs, nil
}

func getDatabaseConfig(v *viper.Viper, networkID uint32) (node.DatabaseConfig, error) {
	var configBytes []byte
	if v.IsSet(DBConfigFileKey) {
		path := os.ExpandEnv(v.GetString(DBConfigFileKey))
		var err error
		configBytes, err = ioutil.ReadFile(path)
		if err != nil {
			return node.DatabaseConfig{}, err
		}
	}

	return node.DatabaseConfig{
		Name: v.GetString(DBTypeKey),
		Path: filepath.Join(
			os.ExpandEnv(v.GetString(DBPathKey)),
			constants.NetworkName(networkID),
		),
		Config: configBytes,
	}, nil
}

func getVMAliases(v *viper.Viper) (map[ids.ID][]string, error) {
	aliasFilePath := filepath.Clean(v.GetString(VMAliasesFileKey))
	exists, err := storage.FileExists(aliasFilePath)
	if err != nil {
		return nil, err
	}

	if !exists {
		if v.IsSet(VMAliasesFileKey) {
			return nil, fmt.Errorf("vm alias file does not exist in %v", aliasFilePath)
		}
		return nil, nil
	}

	fileBytes, err := ioutil.ReadFile(aliasFilePath)
	if err != nil {
		return nil, err
	}

	vmAliasMap := make(map[ids.ID][]string)
	if err := json.Unmarshal(fileBytes, &vmAliasMap); err != nil {
		return nil, fmt.Errorf("problem unmarshaling vmAliases: %w", err)
	}
	return vmAliasMap, nil
}

// getPathFromDirKey reads flag value from viper instance and then checks the folder existence
func getPathFromDirKey(v *viper.Viper, configKey string) (string, error) {
	configDir := os.ExpandEnv(v.GetString(configKey))
	cleanPath := filepath.Clean(configDir)
	ok, err := storage.FolderExists(cleanPath)
	if err != nil {
		return "", err
	}
	if ok {
		return cleanPath, nil
	}
	if v.IsSet(configKey) {
		// user specified a config dir explicitly, but dir does not exist.
		return "", fmt.Errorf("cannot read directory: %v", cleanPath)
	}
	return "", nil
}

// getChainConfigs reads & puts chainConfigs to node config
func getChainConfigs(v *viper.Viper) (map[string]chains.ChainConfig, error) {
	chainConfigPath, err := getPathFromDirKey(v, ChainConfigDirKey)
	if err != nil {
		return nil, err
	}

	if len(chainConfigPath) == 0 {
		return make(map[string]chains.ChainConfig), nil
	}

	chainConfigs, err := readChainConfigPath(chainConfigPath)
	if err != nil {
		return nil, fmt.Errorf("couldn't read chain configs: %w", err)
	}
	return chainConfigs, nil
}

// ReadsChainConfigs reads chain config files from static directories and returns map with contents,
// if successful.
func readChainConfigPath(chainConfigPath string) (map[string]chains.ChainConfig, error) {
	chainDirs, err := filepath.Glob(filepath.Join(chainConfigPath, "*"))
	if err != nil {
		return nil, err
	}
	chainConfigMap := make(map[string]chains.ChainConfig)
	for _, chainDir := range chainDirs {
		dirInfo, err := os.Stat(chainDir)
		if err != nil {
			return nil, err
		}

		if !dirInfo.IsDir() {
			continue
		}

		// chainconfigdir/chainId/config.*
		configData, err := storage.ReadFileWithName(chainDir, chainConfigFileName)
		if err != nil {
			return chainConfigMap, err
		}

		// chainconfigdir/chainId/upgrade.*
		upgradeData, err := storage.ReadFileWithName(chainDir, chainUpgradeFileName)
		if err != nil {
			return chainConfigMap, err
		}

		chainConfigMap[dirInfo.Name()] = chains.ChainConfig{
			Config:  configData,
			Upgrade: upgradeData,
		}
	}
	return chainConfigMap, nil
}

// getSubnetConfigs reads SubnetConfigs to node config map
func getSubnetConfigs(v *viper.Viper, subnetIDs []ids.ID) (map[ids.ID]chains.SubnetConfig, error) {
	subnetConfigPath, err := getPathFromDirKey(v, SubnetConfigDirKey)
	if err != nil {
		return nil, err
	}
	if len(subnetConfigPath) == 0 {
		// subnet config path does not exist but not explicitly specified, so ignore it
		return make(map[ids.ID]chains.SubnetConfig), nil
	}

	subnetConfigs, err := readSubnetConfigs(subnetConfigPath, subnetIDs, defaultSubnetConfig(v))
	if err != nil {
		return nil, fmt.Errorf("couldn't read subnet configs: %w", err)
	}
	return subnetConfigs, nil
}

// readSubnetConfigs reads subnet config files from a path and given subnetIDs and returns a map.
func readSubnetConfigs(subnetConfigPath string, subnetIDs []ids.ID, defaultSubnetConfig chains.SubnetConfig) (map[ids.ID]chains.SubnetConfig, error) {
	subnetConfigs := make(map[ids.ID]chains.SubnetConfig)
	for _, subnetID := range subnetIDs {
		filePath := filepath.Join(subnetConfigPath, subnetID.String()+subnetConfigFileExt)
		fileInfo, err := os.Stat(filePath)
		switch {
		case errors.Is(err, os.ErrNotExist):
			// this subnet config does not exist, move to the next one
			continue
		case err != nil:
			return nil, err
		case fileInfo.IsDir():
			return nil, fmt.Errorf("%q is a directory, expected a file", fileInfo.Name())
		}

		// subnetConfigDir/subnetID.json
		file, err := ioutil.ReadFile(filePath)
		if err != nil {
			return nil, err
		}

		configData := defaultSubnetConfig
		if err := json.Unmarshal(file, &configData); err != nil {
			return nil, err
		}
		if err := configData.ConsensusParameters.Valid(); err != nil {
			return nil, err
		}
		subnetConfigs[subnetID] = configData
	}

	return subnetConfigs, nil
}

func defaultSubnetConfig(v *viper.Viper) chains.SubnetConfig {
	return chains.SubnetConfig{
		ConsensusParameters: getConsensusConfig(v),
		ValidatorOnly:       false,
	}
}

func GetNodeConfig(v *viper.Viper, buildDir string) (node.Config, error) {
	nodeConfig := node.Config{}

	// Plugin directory defaults to [buildDir]/[pluginsDirName]
	nodeConfig.PluginDir = filepath.Join(buildDir, pluginsDirName)

	// Consensus Parameters
	nodeConfig.ConsensusParams = getConsensusConfig(v)
	if err := nodeConfig.ConsensusParams.Valid(); err != nil {
		return node.Config{}, err
	}
	nodeConfig.ConsensusShutdownTimeout = v.GetDuration(ConsensusShutdownTimeoutKey)
	if nodeConfig.ConsensusShutdownTimeout < 0 {
		return node.Config{}, fmt.Errorf("%q must be >= 0", ConsensusShutdownTimeoutKey)
	}

	// Gossiping
	nodeConfig.ConsensusGossipFrequency = v.GetDuration(ConsensusGossipFrequencyKey)
	if nodeConfig.ConsensusGossipFrequency < 0 {
		return node.Config{}, fmt.Errorf("%s must be >= 0", ConsensusGossipFrequencyKey)
	}

	var err error
	// Logging
	nodeConfig.LoggingConfig, err = getLoggingConfig(v)
	if err != nil {
		return node.Config{}, err
	}

	// Network ID
	nodeConfig.NetworkID, err = constants.NetworkID(v.GetString(NetworkNameKey))
	if err != nil {
		return node.Config{}, err
	}

	// Database
	nodeConfig.DatabaseConfig, err = getDatabaseConfig(v, nodeConfig.NetworkID)
	if err != nil {
		return node.Config{}, err
	}

	// IP configuration
	nodeConfig.IPConfig, err = getIPConfig(v)
	if err != nil {
		return node.Config{}, err
	}

	// Staking
	nodeConfig.StakingConfig, err = getStakingConfig(v, nodeConfig.NetworkID)
	if err != nil {
		return node.Config{}, err
	}

	// Whitelisted Subnets
	nodeConfig.WhitelistedSubnets, err = getWhitelistedSubnets(v)
	if err != nil {
		return node.Config{}, err
	}

	// HTTP APIs
	nodeConfig.HTTPConfig, err = getHTTPConfig(v)
	if err != nil {
		return node.Config{}, err
	}

	// Health
	nodeConfig.HealthCheckFreq = v.GetDuration(HealthCheckFreqKey)
	if nodeConfig.HealthCheckFreq < 0 {
		return node.Config{}, fmt.Errorf("%s must be positive", HealthCheckFreqKey)
	}
	// Halflife of continuous averager used in health checks
	healthCheckAveragerHalflife := v.GetDuration(HealthCheckAveragerHalflifeKey)
	if healthCheckAveragerHalflife <= 0 {
		return node.Config{}, fmt.Errorf("%s must be positive", HealthCheckAveragerHalflifeKey)
	}

	// Router
	nodeConfig.ConsensusRouter = &router.ChainRouter{}
	nodeConfig.RouterHealthConfig, err = getRouterHealthConfig(v, healthCheckAveragerHalflife)
	if err != nil {
		return node.Config{}, err
	}

	// Metrics
	nodeConfig.MeterVMEnabled = v.GetBool(MeterVMsEnabledKey)

	// Adaptive Timeout Config
	nodeConfig.AdaptiveTimeoutConfig, err = getAdaptiveTimeoutConfig(v)
	if err != nil {
		return node.Config{}, err
	}

	// Network Config
	nodeConfig.NetworkConfig, err = getNetworkConfig(v, healthCheckAveragerHalflife)
	if err != nil {
		return node.Config{}, err
	}

	// Benchlist
	nodeConfig.BenchlistConfig, err = getBenchlistConfig(v, nodeConfig.ConsensusParams.Alpha, nodeConfig.ConsensusParams.K)
	if err != nil {
		return node.Config{}, err
	}

	// File Descriptor Limit
	fdLimit := v.GetUint64(FdLimitKey)
	if err := ulimit.Set(fdLimit); err != nil {
		return node.Config{}, fmt.Errorf("failed to set fd limit correctly due to: %w", err)
	}

	// Tx Fee
	nodeConfig.TxFeeConfig = getTxFeeConfig(v, nodeConfig.NetworkID)

	// Genesis Data
	nodeConfig.GenesisBytes, nodeConfig.AvaxAssetID, err = genesis.Genesis(
		nodeConfig.NetworkID,
		os.ExpandEnv(v.GetString(GenesisConfigFileKey)),
	)
	if err != nil {
		return node.Config{}, fmt.Errorf("unable to load genesis file: %w", err)
	}

	// Assertions
	nodeConfig.EnableAssertions = v.GetBool(AssertionsEnabledKey)

	// Crypto
	nodeConfig.EnableCrypto = v.GetBool(SignatureVerificationEnabledKey)

	// Bootstrap Configs
	nodeConfig.BootstrapConfig, err = getBootstrapConfig(v, nodeConfig.NetworkID)
	if err != nil {
		return node.Config{}, err
	}

	// Subnet Configs
	subnetConfigs, err := getSubnetConfigs(v, nodeConfig.WhitelistedSubnets.List())
	if err != nil {
		return node.Config{}, err
	}
	nodeConfig.SubnetConfigs = subnetConfigs

	// Chain Configs
	nodeConfig.ChainConfigs, err = getChainConfigs(v)
	if err != nil {
		return node.Config{}, err
	}

	// Profiler
	nodeConfig.ProfilerConfig, err = getProfilerConfig(v)
	if err != nil {
		return node.Config{}, err
	}

	// VM Aliases
	nodeConfig.VMAliases, err = getVMAliases(v)
	if err != nil {
		return node.Config{}, err
	}
	return nodeConfig, nil
}
