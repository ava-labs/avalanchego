// (c) 2021 Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package config

import (
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

func getAPIConfig(v *viper.Viper) (node.APIConfig, error) {
	config := node.APIConfig{}
	config.HTTPHost = v.GetString(HTTPHostKey)
	config.HTTPPort = uint16(v.GetUint(HTTPPortKey))
	config.HTTPSEnabled = v.GetBool(HTTPSEnabledKey)
	config.HTTPSKeyFile = os.ExpandEnv(v.GetString(HTTPSKeyFileKey))
	config.HTTPSCertFile = os.ExpandEnv(v.GetString(HTTPSCertFileKey))
	config.APIAllowedOrigins = v.GetStringSlice(HTTPAllowedOrigins)
	config.APIRequireAuthToken = v.GetBool(APIAuthRequiredKey)
	if config.APIRequireAuthToken {
		passwordFile := v.GetString(APIAuthPasswordFileKey)
		pwBytes, err := ioutil.ReadFile(passwordFile)
		if err != nil {
			return node.APIConfig{}, fmt.Errorf("api-auth-password-file %q failed to be read with: %w", passwordFile, err)
		}
		config.APIAuthPassword = strings.TrimSpace(string(pwBytes))
		if !password.SufficientlyStrong(config.APIAuthPassword, password.OK) {
			return node.APIConfig{}, errors.New("api-auth-password is not strong enough")
		}
	}
	config.AdminAPIEnabled = v.GetBool(AdminAPIEnabledKey)
	config.InfoAPIEnabled = v.GetBool(InfoAPIEnabledKey)
	config.KeystoreAPIEnabled = v.GetBool(KeystoreAPIEnabledKey)
	config.MetricsAPIEnabled = v.GetBool(MetricsAPIEnabledKey)
	config.HealthAPIEnabled = v.GetBool(HealthAPIEnabledKey)
	config.IPCAPIEnabled = v.GetBool(IpcAPIEnabledKey)
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
		return config, fmt.Errorf("%s must be in [0,1]", RouterHealthMaxDropRateKey)
	case config.MaxOutstandingDuration <= 0:
		return config, fmt.Errorf("%s must be positive", NetworkHealthMaxOutstandingDurationKey)
	}
	return config, nil
}

func getIPCConfig(v *viper.Viper) node.IPCConfig {
	config := node.IPCConfig{}
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
	config := network.Config{}
	// Throttling
	config.InboundConnThrottlerConfig = throttling.InboundConnThrottlerConfig{
		AllowCooldown:  v.GetDuration(InboundConnThrottlerCooldownKey),
		MaxRecentConns: v.GetInt(InboundConnThrottlerMaxRecentConnsKey),
	}
	config.InboundThrottlerConfig = throttling.MsgThrottlerConfig{
		AtLargeAllocSize:    v.GetUint64(InboundThrottlerAtLargeAllocSizeKey),
		VdrAllocSize:        v.GetUint64(InboundThrottlerVdrAllocSizeKey),
		NodeMaxAtLargeBytes: v.GetUint64(InboundThrottlerNodeMaxAtLargeBytesKey),
	}
	config.OutboundThrottlerConfig = throttling.MsgThrottlerConfig{
		AtLargeAllocSize:    v.GetUint64(OutboundThrottlerAtLargeAllocSizeKey),
		VdrAllocSize:        v.GetUint64(OutboundThrottlerVdrAllocSizeKey),
		NodeMaxAtLargeBytes: v.GetUint64(OutboundThrottlerNodeMaxAtLargeBytesKey),
	}

	// Network Health Check
	config.HealthConfig = network.HealthConfig{
		MaxTimeSinceMsgSent:          v.GetDuration(NetworkHealthMaxTimeSinceMsgSentKey),
		MaxTimeSinceMsgReceived:      v.GetDuration(NetworkHealthMaxTimeSinceMsgReceivedKey),
		MaxPortionSendQueueBytesFull: v.GetFloat64(NetworkHealthMaxPortionSendQueueFillKey),
		MinConnectedPeers:            v.GetUint(NetworkHealthMinPeersKey),
		MaxSendFailRate:              v.GetFloat64(NetworkHealthMaxSendFailRateKey),
		MaxSendFailRateHalflife:      halflife,
	}
	switch {
	case config.HealthConfig.MaxTimeSinceMsgSent < 0:
		return network.Config{}, fmt.Errorf("%s must be > 0", NetworkHealthMaxTimeSinceMsgSentKey)
	case config.HealthConfig.MaxTimeSinceMsgReceived < 0:
		return network.Config{}, fmt.Errorf("%s must be > 0", NetworkHealthMaxTimeSinceMsgReceivedKey)
	case config.HealthConfig.MaxSendFailRate < 0 || config.HealthConfig.MaxSendFailRate > 1:
		return network.Config{}, fmt.Errorf("%s must be in [0,1]", NetworkHealthMaxSendFailRateKey)
	case config.HealthConfig.MaxPortionSendQueueBytesFull < 0 || config.HealthConfig.MaxPortionSendQueueBytesFull > 1:
		return network.Config{}, fmt.Errorf("%s must be in [0,1]", NetworkHealthMaxPortionSendQueueFillKey)
	}

	// Network Timeout
	config.AdaptiveTimeoutConfig = timer.AdaptiveTimeoutConfig{
		InitialTimeout:     v.GetDuration(NetworkInitialTimeoutKey),
		MinimumTimeout:     v.GetDuration(NetworkMinimumTimeoutKey),
		MaximumTimeout:     v.GetDuration(NetworkMaximumTimeoutKey),
		TimeoutHalflife:    v.GetDuration(NetworkTimeoutHalflifeKey),
		TimeoutCoefficient: v.GetFloat64(NetworkTimeoutCoefficientKey),
	}
	switch {
	case config.MinimumTimeout < 1:
		return network.Config{}, errors.New("minimum timeout must be positive")
	case config.MinimumTimeout > config.MaximumTimeout:
		return network.Config{}, errors.New("maximum timeout can't be less than minimum timeout")
	case config.InitialTimeout < config.MinimumTimeout ||
		config.InitialTimeout > config.MaximumTimeout:
		return network.Config{}, errors.New("initial timeout should be in the range [minimumTimeout, maximumTimeout]")
	case config.TimeoutHalflife <= 0:
		return network.Config{}, errors.New("network timeout halflife must be positive")
	case config.TimeoutCoefficient < 1:
		return network.Config{}, errors.New("network timeout coefficient must be >= 1")
	}

	// Outbound connection throttling
	config.DialerConfig = dialer.NewConfig(
		v.GetUint32(OutboundConnectionThrottlingRps),
		v.GetDuration(OutboundConnectionTimeout),
	)

	// Compression
	config.CompressionEnabled = v.GetBool(NetworkCompressionEnabledKey)

	// Peer alias
	config.PeerAliasTimeout = v.GetDuration(PeerAliasTimeoutKey)
	return config, nil
}

func getBenchlistConfig(v *viper.Viper, alpha, k int) benchlist.Config {
	config := benchlist.Config{}
	config.Threshold = v.GetInt(BenchlistFailThresholdKey)
	config.PeerSummaryEnabled = v.GetBool(BenchlistPeerSummaryEnabledKey)
	config.Duration = v.GetDuration(BenchlistDurationKey)
	config.MinimumFailingDuration = v.GetDuration(BenchlistMinFailingDurationKey)
	config.MaxPortion = (1.0 - (float64(alpha) / float64(k))) / 3.0
	return config
}

func getBootstrapConfig(v *viper.Viper, nodeConfig *node.Config) {
	nodeConfig.RetryBootstrap = v.GetBool(RetryBootstrapKey)
	nodeConfig.RetryBootstrapMaxAttempts = v.GetInt(RetryBootstrapMaxAttemptsKey)
	nodeConfig.BootstrapBeaconConnectionTimeout = v.GetDuration(BootstrapBeaconConnectionTimeoutKey)
	nodeConfig.BootstrapMaxTimeGetAncestors = v.GetDuration(BootstrapMaxTimeGetAncestorsKey)
	nodeConfig.BootstrapMultiputMaxContainersSent = int(v.GetUint(BootstrapMultiputMaxContainersSentKey))
	nodeConfig.BootstrapMultiputMaxContainersReceived = int(v.GetUint(BootstrapMultiputMaxContainersReceivedKey))
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

func getProfilerConfig(v *viper.Viper) profiler.Config {
	return profiler.Config{
		Dir:         os.ExpandEnv(v.GetString(ProfileDirKey)),
		Enabled:     v.GetBool(ProfileContinuousEnabledKey),
		Freq:        v.GetDuration(ProfileContinuousFreqKey),
		MaxNumFiles: v.GetInt(ProfileContinuousMaxFilesKey),
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

	// NetworkID
	networkID, err := constants.NetworkID(v.GetString(NetworkNameKey))
	if err != nil {
		return node.Config{}, err
	}
	nodeConfig.NetworkID = networkID

	// Database
	nodeConfig.DBName = v.GetString(DBTypeKey)
	nodeConfig.DBPath = filepath.Join(
		os.ExpandEnv(v.GetString(DBPathKey)),
		constants.NetworkName(nodeConfig.NetworkID),
	)

	// IP configuration
	nodeConfig.IPConfig, err = getIPConfig(v)
	if err != nil {
		return node.Config{}, err
	}

	// Staking
	nodeConfig.EnableStaking = v.GetBool(StakingEnabledKey)
	nodeConfig.DisabledStakingWeight = v.GetUint64(StakingDisabledWeightKey)
	nodeConfig.MinStakeDuration = v.GetDuration(MinStakeDurationKey)
	nodeConfig.MaxStakeDuration = v.GetDuration(MaxStakeDurationKey)
	nodeConfig.StakeMintingPeriod = v.GetDuration(StakeMintingPeriodKey)
	if !nodeConfig.EnableStaking && nodeConfig.DisabledStakingWeight == 0 {
		return node.Config{}, errInvalidStakerWeights
	}

	if v.GetBool(StakingEphemeralCertEnabledKey) {
		// In fetch only mode or if explicitly set, use an ephemeral staking key/cert
		cert, err := staking.NewTLSCert()
		if err != nil {
			return node.Config{}, fmt.Errorf("couldn't generate ephemeral staking key/cert: %w", err)
		}
		nodeConfig.StakingTLSCert = *cert
	} else {
		// Parse the staking key/cert paths
		stakingKeyPath := os.ExpandEnv(v.GetString(StakingKeyPathKey))
		stakingCertPath := os.ExpandEnv(v.GetString(StakingCertPathKey))

		switch {
		// If staking key/cert locations are specified but not found, error
		case v.IsSet(StakingKeyPathKey) || v.IsSet(StakingCertPathKey):
			if _, err := os.Stat(stakingKeyPath); os.IsNotExist(err) {
				return node.Config{}, fmt.Errorf("couldn't find staking key at %s", stakingKeyPath)
			} else if _, err := os.Stat(stakingCertPath); os.IsNotExist(err) {
				return node.Config{}, fmt.Errorf("couldn't find staking certificate at %s", stakingCertPath)
			}
		default:
			// Create the staking key/cert if [stakingKeyPath] doesn't exist
			if err := staking.InitNodeStakingKeyPair(stakingKeyPath, stakingCertPath); err != nil {
				return node.Config{}, fmt.Errorf("couldn't generate staking key/cert: %w", err)
			}
		}

		// Load and parse the staking key/cert
		cert, err := staking.LoadTLSCert(stakingKeyPath, stakingCertPath)
		if err != nil {
			return node.Config{}, fmt.Errorf("problem reading staking certificate: %w", err)
		}
		nodeConfig.StakingTLSCert = *cert
	}

	if err := getBootstrapPeers(v, &nodeConfig); err != nil {
		return node.Config{}, err
	}

	nodeConfig.WhitelistedSubnets.Add(constants.PrimaryNetworkID)
	for _, subnet := range strings.Split(v.GetString(WhitelistedSubnetsKey), ",") {
		if subnet != "" {
			subnetID, err := ids.FromString(subnet)
			if err != nil {
				return node.Config{}, fmt.Errorf("couldn't parse subnetID %s: %w", subnet, err)
			}
			nodeConfig.WhitelistedSubnets.Add(subnetID)
		}
	}

	// HTTP APIs
	nodeConfig.APIConfig, err = getAPIConfig(v)
	if err != nil {
		return node.Config{}, err
	}

	// Health
	nodeConfig.HealthCheckFreq = v.GetDuration(HealthCheckFreqKey)
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
	nodeConfig.BenchlistConfig = getBenchlistConfig(v, nodeConfig.ConsensusParams.Alpha, nodeConfig.ConsensusParams.K)

	// File Descriptor Limit
	fdLimit := v.GetUint64(FdLimitKey)
	if err := ulimit.Set(fdLimit); err != nil {
		return node.Config{}, fmt.Errorf("failed to set fd limit correctly due to: %w", err)
	}

	// Network Parameters
	if networkID != constants.MainnetID && networkID != constants.FujiID {
		txFee := v.GetUint64(TxFeeKey)
		creationTxFee := v.GetUint64(CreationTxFeeKey)
		uptimeRequirement := v.GetFloat64(UptimeRequirementKey)
		nodeConfig.TxFee = txFee
		nodeConfig.CreationTxFee = creationTxFee
		nodeConfig.UptimeRequirement = uptimeRequirement

		minValidatorStake := v.GetUint64(MinValidatorStakeKey)
		maxValidatorStake := v.GetUint64(MaxValidatorStakeKey)
		minDelegatorStake := v.GetUint64(MinDelegatorStakeKey)
		minDelegationFee := v.GetUint64(MinDelegatorFeeKey)
		if minValidatorStake > maxValidatorStake {
			return node.Config{}, errors.New("minimum validator stake can't be greater than maximum validator stake")
		}

		nodeConfig.MinValidatorStake = minValidatorStake
		nodeConfig.MaxValidatorStake = maxValidatorStake
		nodeConfig.MinDelegatorStake = minDelegatorStake

		if minDelegationFee > 1_000_000 {
			return node.Config{}, errors.New("delegation fee must be in the range [0, 1000000]")
		}
		nodeConfig.MinDelegationFee = uint32(minDelegationFee)

		if nodeConfig.MinStakeDuration == 0 {
			return node.Config{}, errors.New("min stake duration can't be zero")
		}
		if nodeConfig.MaxStakeDuration < nodeConfig.MinStakeDuration {
			return node.Config{}, errors.New("max stake duration can't be less than min stake duration")
		}
		if nodeConfig.StakeMintingPeriod < nodeConfig.MaxStakeDuration {
			return node.Config{}, errors.New("stake minting period can't be less than max stake duration")
		}

		nodeConfig.EpochFirstTransition = time.Unix(v.GetInt64(SnowEpochFirstTransition), 0)
		nodeConfig.EpochDuration = v.GetDuration(SnowEpochDuration)
	} else {
		nodeConfig.Params = *genesis.GetParams(networkID)
	}

	// Load genesis data
	nodeConfig.GenesisBytes, nodeConfig.AvaxAssetID, err = genesis.Genesis(
		networkID,
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
	// TODO use same pattern as elsewhere
	getBootstrapConfig(v, &nodeConfig)

	// Chain Configs
	nodeConfig.ChainConfigs, err = getChainConfigs(v)
	if err != nil {
		return node.Config{}, err
	}

	// Profiler
	nodeConfig.ProfilerConfig = getProfilerConfig(v)

	// VM Aliases
	nodeConfig.VMAliases, err = getVMAliases(v)
	if err != nil {
		return node.Config{}, err
	}
	return nodeConfig, nil
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
			return nil, fmt.Errorf("config for coreth(C) is already provided in chain config files")
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

// Initialize config.BootstrapPeers.
func getBootstrapPeers(v *viper.Viper, config *node.Config) error {
	bootstrapIPs, bootstrapIDs := genesis.SampleBeacons(config.NetworkID, 5)
	if v.IsSet(BootstrapIPsKey) {
		bootstrapIPs = strings.Split(v.GetString(BootstrapIPsKey), ",")
	}
	for _, ip := range bootstrapIPs {
		if ip == "" {
			continue
		}
		addr, err := utils.ToIPDesc(ip)
		if err != nil {
			return fmt.Errorf("couldn't parse bootstrap ip %s: %w", ip, err)
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
			return fmt.Errorf("couldn't parse bootstrap peer id: %w", err)
		}
		config.BootstrapIDs = append(config.BootstrapIDs, nodeID)
	}
	return nil
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
