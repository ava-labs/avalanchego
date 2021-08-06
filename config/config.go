// (c) 2021 Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package config

import (
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/viper"

	"github.com/ava-labs/avalanchego/app/process"
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
	"github.com/ava-labs/avalanchego/utils/timer"
	"github.com/ava-labs/avalanchego/utils/ulimit"
)

const (
	pluginsDirName       = "plugins"
	chainConfigFileName  = "config"
	chainUpgradeFileName = "upgrade"
)

var (
	deprecatedKeys = map[string]string{
		CorethConfigKey: "please use --config-file to specify C-Chain config",
	}

	errInvalidStakerWeights = errors.New("staking weights must be positive")
)

func GetProcessConfig(v *viper.Viper) (process.Config, error) {
	config := process.Config{
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
		return process.Config{}, fmt.Errorf(
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
	if err != nil {
		return loggingConfig, err
	}
	return loggingConfig, nil
}

func getAPIAuthConfig(v *viper.Viper) (node.APIAuthConfig, error) {
	config := node.APIAuthConfig{
		APIRequireAuthToken: v.GetBool(APIAuthRequiredKey),
	}
	if config.APIRequireAuthToken {
		passwordFilePath := v.GetString(APIAuthPasswordFileKey)
		pwBytes, err := ioutil.ReadFile(passwordFilePath)
		if err != nil {
			return node.APIAuthConfig{}, fmt.Errorf("API auth password file %q failed to be read: %w", passwordFilePath, err)
		}
		config.APIAuthPassword = strings.TrimSpace(string(pwBytes))
		if !password.SufficientlyStrong(config.APIAuthPassword, password.OK) {
			return node.APIAuthConfig{}, errors.New("API auth password is not strong enough")
		}
	}
	return config, nil
}

func getAPIConfig(v *viper.Viper) (node.APIConfig, error) {
	config := node.APIConfig{}
	var err error
	config.APIAuthConfig, err = getAPIAuthConfig(v)
	if err != nil {
		return node.APIConfig{}, err
	}
	config.HTTPHost = v.GetString(HTTPHostKey)
	config.HTTPPort = uint16(v.GetUint(HTTPPortKey))
	config.HTTPSEnabled = v.GetBool(HTTPSEnabledKey)
	config.HTTPSKeyFile = os.ExpandEnv(v.GetString(HTTPSKeyFileKey))
	config.HTTPSCertFile = os.ExpandEnv(v.GetString(HTTPSCertFileKey))
	config.APIAllowedOrigins = v.GetStringSlice(HTTPAllowedOrigins)
	config.AdminAPIEnabled = v.GetBool(AdminAPIEnabledKey)
	config.InfoAPIEnabled = v.GetBool(InfoAPIEnabledKey)
	config.KeystoreAPIEnabled = v.GetBool(KeystoreAPIEnabledKey)
	config.MetricsAPIEnabled = v.GetBool(MetricsAPIEnabledKey)
	config.HealthAPIEnabled = v.GetBool(HealthAPIEnabledKey)
	config.IndexAPIEnabled = v.GetBool(IndexEnabledKey)
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

func getIPCConfig(v *viper.Viper) node.IPCConfig {
	config := node.IPCConfig{
		IPCAPIEnabled: v.GetBool(IpcAPIEnabledKey),
	}
	if v.IsSet(IpcsChainIDsKey) {
		config.IPCDefaultChainIDs = strings.Split(v.GetString(IpcsChainIDsKey), ",")
	}
	if v.IsSet(IpcsPathKey) {
		config.IPCPath = os.ExpandEnv(v.GetString(IpcsPathKey))
	} else {
		config.IPCPath = ipcs.DefaultBaseURL
	}
	return config
}

func getNetworkConfig(v *viper.Viper, halflife time.Duration) (network.Config, error) {
	config := network.Config{
		// Throttling
		InboundConnThrottlerConfig: throttling.InboundConnThrottlerConfig{
			AllowCooldown:  v.GetDuration(InboundConnThrottlerCooldownKey),
			MaxRecentConns: v.GetInt(InboundConnThrottlerMaxRecentConnsKey),
		},
		InboundThrottlerConfig: throttling.MsgThrottlerConfig{
			AtLargeAllocSize:    v.GetUint64(InboundThrottlerAtLargeAllocSizeKey),
			VdrAllocSize:        v.GetUint64(InboundThrottlerVdrAllocSizeKey),
			NodeMaxAtLargeBytes: v.GetUint64(InboundThrottlerNodeMaxAtLargeBytesKey),
		},
		OutboundThrottlerConfig: throttling.MsgThrottlerConfig{
			AtLargeAllocSize:    v.GetUint64(OutboundThrottlerAtLargeAllocSizeKey),
			VdrAllocSize:        v.GetUint64(OutboundThrottlerVdrAllocSizeKey),
			NodeMaxAtLargeBytes: v.GetUint64(OutboundThrottlerNodeMaxAtLargeBytesKey),
		},
		// Network Health Check
		HealthConfig: network.HealthConfig{
			MaxTimeSinceMsgSent:          v.GetDuration(NetworkHealthMaxTimeSinceMsgSentKey),
			MaxTimeSinceMsgReceived:      v.GetDuration(NetworkHealthMaxTimeSinceMsgReceivedKey),
			MaxPortionSendQueueBytesFull: v.GetFloat64(NetworkHealthMaxPortionSendQueueFillKey),
			MinConnectedPeers:            v.GetUint(NetworkHealthMinPeersKey),
			MaxSendFailRate:              v.GetFloat64(NetworkHealthMaxSendFailRateKey),
			MaxSendFailRateHalflife:      halflife,
		},
		AdaptiveTimeoutConfig: timer.AdaptiveTimeoutConfig{
			InitialTimeout:     v.GetDuration(NetworkInitialTimeoutKey),
			MinimumTimeout:     v.GetDuration(NetworkMinimumTimeoutKey),
			MaximumTimeout:     v.GetDuration(NetworkMaximumTimeoutKey),
			TimeoutHalflife:    v.GetDuration(NetworkTimeoutHalflifeKey),
			TimeoutCoefficient: v.GetFloat64(NetworkTimeoutCoefficientKey),
		},
		CompressionEnabled: v.GetBool(NetworkCompressionEnabledKey),
		DialerConfig: dialer.Config{
			ThrottleRps:       v.GetUint32(OutboundConnectionThrottlingRps),
			ConnectionTimeout: v.GetDuration(OutboundConnectionTimeout),
		},
		PeerAliasTimeout: v.GetDuration(PeerAliasTimeoutKey),
	}
	switch {
	case config.MinimumTimeout < 1:
		return network.Config{}, fmt.Errorf("%q must be positive", NetworkMinimumTimeoutKey)
	case config.MinimumTimeout > config.MaximumTimeout:
		return network.Config{}, fmt.Errorf("%q must be >= %q", NetworkMaximumTimeoutKey, NetworkMinimumTimeoutKey)
	case config.InitialTimeout < config.MinimumTimeout || config.InitialTimeout > config.MaximumTimeout:
		return network.Config{}, fmt.Errorf("%q must be in [%q, %q]", NetworkInitialTimeoutKey, NetworkMinimumTimeoutKey, NetworkMaximumTimeoutKey)
	case config.TimeoutHalflife <= 0:
		return network.Config{}, fmt.Errorf("%q must > 0", NetworkTimeoutHalflifeKey)
	case config.TimeoutCoefficient < 1:
		return network.Config{}, fmt.Errorf("%q must be >= 1", NetworkTimeoutCoefficientKey)
	case config.HealthConfig.MaxTimeSinceMsgSent < 0:
		return network.Config{}, fmt.Errorf("%s must be > 0", NetworkHealthMaxTimeSinceMsgSentKey)
	case config.HealthConfig.MaxTimeSinceMsgReceived < 0:
		return network.Config{}, fmt.Errorf("%s must be > 0", NetworkHealthMaxTimeSinceMsgReceivedKey)
	case config.HealthConfig.MaxSendFailRate < 0 || config.HealthConfig.MaxSendFailRate > 1:
		return network.Config{}, fmt.Errorf("%s must be in [0,1]", NetworkHealthMaxSendFailRateKey)
	case config.HealthConfig.MaxPortionSendQueueBytesFull < 0 || config.HealthConfig.MaxPortionSendQueueBytesFull > 1:
		return network.Config{}, fmt.Errorf("%s must be in [0,1]", NetworkHealthMaxPortionSendQueueFillKey)
	case config.DialerConfig.ConnectionTimeout < 0:
		return network.Config{}, fmt.Errorf("%q must be >= 0", OutboundConnectionTimeout)
	case config.PeerAliasTimeout < 0:
		return network.Config{}, fmt.Errorf("%q must be >= 0", PeerAliasTimeoutKey)
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
		RetryBootstrapMaxAttempts:              v.GetInt(RetryBootstrapMaxAttemptsKey),
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

func getGossipConfig(v *viper.Viper) (node.GossipConfig, error) {
	config := node.GossipConfig{
		ConsensusGossipConfig: node.ConsensusGossipConfig{
			ConsensusGossipFrequency:            v.GetDuration(ConsensusGossipFrequencyKey),
			ConsensusGossipAcceptedFrontierSize: uint(v.GetUint32(ConsensusGossipAcceptedFrontierSizeKey)),
			ConsensusGossipOnAcceptSize:         uint(v.GetUint32(ConsensusGossipOnAcceptSizeKey)),
		},
		PeerListGossipConfig: node.PeerListGossipConfig{
			// Node will gossip [PeerListSize] peers to [PeerListGossipSize] every [PeerListGossipFreq]
			PeerListSize:       v.GetUint32(NetworkPeerListSizeKey),
			PeerListGossipFreq: v.GetDuration(NetworkPeerListGossipFreqKey),
			PeerListGossipSize: v.GetUint32(NetworkPeerListGossipSizeKey),
		},
	}
	switch {
	case config.ConsensusGossipFrequency < 0:
		return node.GossipConfig{}, fmt.Errorf("%s must be >= 0", ConsensusGossipFrequencyKey)
	case config.PeerListGossipFreq < 0:
		return node.GossipConfig{}, fmt.Errorf("%s must be >= 0", NetworkPeerListGossipFreqKey)
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
	// TODO add the below
	//	nodeConfig.StakingKeyFile = stakingKeyPath
	//	nodeConfig.StakingCertFile = stakingCertPath

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
		case config.UptimeRequirement < 0:
			return node.StakingConfig{}, fmt.Errorf("%q must be <= 0", UptimeRequirementKey)
		case config.MinValidatorStake > config.MaxValidatorStake:
			return node.StakingConfig{}, errors.New("minimum validator stake can't be greater than maximum validator stake")
		case config.MinDelegationFee > 1_000_000:
			return node.StakingConfig{}, errors.New("delegation fee must be in the range [0, 1,000,000]")
		case config.MinStakeDuration <= 0:
			return node.StakingConfig{}, errors.New("min stake duration must be > 0")
		case config.MaxStakeDuration < config.MinStakeDuration:
			return node.StakingConfig{}, errors.New("max stake duration can't be less than min stake duration")
		case config.StakeMintingPeriod < config.MaxStakeDuration:
			return node.StakingConfig{}, errors.New("stake minting period can't be less than max stake duration")
		}
	} else {
		config.StakingConfig = genesis.GetStakingConfig(networkID)
	}
	return config, nil
}

func getTxFeeConfig(v *viper.Viper, networkID uint32) genesis.TxFeeConfig {
	if networkID != constants.MainnetID && networkID != constants.FujiID {
		return genesis.TxFeeConfig{
			TxFee:         v.GetUint64(TxFeeKey),
			CreationTxFee: v.GetUint64(CreationTxFeeKey),
		}
	}
	return genesis.GetTxFeeConfig(networkID)
}

func getEpochConfig(v *viper.Viper, networkID uint32) (genesis.EpochConfig, error) {
	if networkID != constants.MainnetID && networkID != constants.FujiID {
		config := genesis.EpochConfig{
			EpochFirstTransition: time.Unix(v.GetInt64(SnowEpochFirstTransitionKey), 0),
			EpochDuration:        v.GetDuration(SnowEpochDurationKey),
		}
		if config.EpochDuration <= 0 {
			return genesis.EpochConfig{}, fmt.Errorf("%s must be > 0", SnowEpochDurationKey)
		}
		return config, nil
	}
	return genesis.GetEpochConfig(networkID), nil
}

func getWhitelistedSubnets(v *viper.Viper) (ids.Set, error) {
	whitelistedSubnetIDs := ids.Set{}
	whitelistedSubnetIDs.Add(constants.PrimaryNetworkID)
	for _, subnet := range strings.Split(v.GetString(WhitelistedSubnetsKey), ",") {
		if subnet == "" {
			continue
		}
		subnetID, err := ids.FromString(subnet)
		if err != nil {
			return nil, fmt.Errorf("couldn't parse subnetID %q: %w", subnet, err)
		}
		whitelistedSubnetIDs.Add(subnetID)
	}
	return whitelistedSubnetIDs, nil
}

func getDatabaseConfig(v *viper.Viper, networkID uint32) node.DatabaseConfig {
	return node.DatabaseConfig{
		Name: v.GetString(DBTypeKey),
		Path: filepath.Join(
			os.ExpandEnv(v.GetString(DBPathKey)),
			constants.NetworkName(networkID),
		),
	}
}

func getVMAliases(v *viper.Viper) (map[ids.ID][]string, error) {
	aliasFilePath := path.Clean(v.GetString(VMAliasesFileKey))
	exists, err := fileExists(aliasFilePath)
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

// getChainConfigs reads & puts chainConfigs to node config
func getChainConfigs(v *viper.Viper) (map[string]chains.ChainConfig, error) {
	chainConfigDir := v.GetString(ChainConfigDirKey)
	chainsPath := path.Clean(chainConfigDir)
	// user specified a chain config dir explicitly, but dir does not exist.
	if v.IsSet(ChainConfigDirKey) {
		info, err := os.Stat(chainsPath)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("not a directory: %v", chainsPath)
		}
	}
	// gets direct subdirs
	chainDirs, err := filepath.Glob(path.Join(chainsPath, "*"))
	if err != nil {
		return nil, err
	}
	chainConfigs, err := readChainConfigDirs(chainDirs)
	if err != nil {
		return nil, fmt.Errorf("couldn't read chain configs: %w", err)
	}

	// Coreth Plugin
	if v.IsSet(CorethConfigKey) {
		// error if C config is already populated
		if isCChainConfigSet(chainConfigs) {
			return nil, errors.New("C-Chain config is already provided in chain config files")
		}
		corethConfigValue := v.Get(CorethConfigKey)
		var corethConfigBytes []byte
		switch value := corethConfigValue.(type) {
		case string:
			corethConfigBytes = []byte(value)
		default:
			corethConfigBytes, err = json.Marshal(value)
			if err != nil {
				return nil, fmt.Errorf("couldn't parse coreth config: %w", err)
			}
		}
		cChainPrimaryAlias := genesis.GetCChainAliases()[0]
		cChainConfig := chainConfigs[cChainPrimaryAlias]
		cChainConfig.Config = corethConfigBytes
		chainConfigs[cChainPrimaryAlias] = cChainConfig
	}
	return chainConfigs, nil
}

// ReadsChainConfigs reads chain config files from static directories and returns map with contents,
// if successful.
func readChainConfigDirs(chainDirs []string) (map[string]chains.ChainConfig, error) {
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
		configData, err := readSingleFile(chainDir, chainConfigFileName)
		if err != nil {
			return chainConfigMap, err
		}

		// chainconfigdir/chainId/upgrade.*
		upgradeData, err := readSingleFile(chainDir, chainUpgradeFileName)
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

// safeReadFile reads a file but does not return an error if there is no file exists at path
func safeReadFile(path string) ([]byte, error) {
	ok, err := fileExists(path)
	if err == nil && ok {
		return ioutil.ReadFile(path)
	}
	return nil, err
}

// fileExists checks if a file exists before we
// try using it to prevent further errors.
func fileExists(filePath string) (bool, error) {
	info, err := os.Stat(filePath)
	if err == nil {
		return !info.IsDir(), nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, err
}

// readSingleFile reads a single file with name fileName without specifying any extension.
// it errors when there are more than 1 file with the given fileName
func readSingleFile(parentDir string, fileName string) ([]byte, error) {
	filePath := path.Join(parentDir, fileName)
	files, err := filepath.Glob(filePath + ".*") // all possible extensions
	if err != nil {
		return nil, err
	}
	if len(files) > 1 {
		return nil, fmt.Errorf("too much %s file in %s", fileName, parentDir)
	}
	if len(files) == 0 { // no file found, return nothing
		return nil, nil
	}
	return safeReadFile(files[0])
}

// checks if C chain config bytes already set in map with alias key.
// it does only checks alias key, chainId is not available at this point.
func isCChainConfigSet(chainConfigs map[string]chains.ChainConfig) bool {
	cChainAliases := genesis.GetCChainAliases()
	for _, alias := range cChainAliases {
		val, ok := chainConfigs[alias]
		if ok && len(val.Config) > 1 {
			return true
		}
	}
	return false
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
	var err error
	nodeConfig.GossipConfig, err = getGossipConfig(v)
	if err != nil {
		return node.Config{}, err
	}

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
	nodeConfig.DatabaseConfig = getDatabaseConfig(v, nodeConfig.NetworkID)

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
	nodeConfig.APIConfig, err = getAPIConfig(v)
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

	// IPCs
	nodeConfig.IPCConfig = getIPCConfig(v)

	// Metrics
	nodeConfig.MeterVMEnabled = v.GetBool(MeterVMsEnabledKey)

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

	// Epoch
	nodeConfig.EpochConfig, err = getEpochConfig(v, nodeConfig.NetworkID)
	if err != nil {
		return node.Config{}, fmt.Errorf("couldn't load epoch config: %w", err)
	}

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

	// Indexer
	nodeConfig.IndexAllowIncomplete = v.GetBool(IndexAllowIncompleteKey)

	// Bootstrap Configs
	nodeConfig.BootstrapConfig, err = getBootstrapConfig(v, nodeConfig.NetworkID)
	if err != nil {
		return node.Config{}, err
	}

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
