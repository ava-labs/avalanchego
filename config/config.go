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
	"github.com/ava-labs/avalanchego/snow/networking/router"
	"github.com/ava-labs/avalanchego/staking"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/dynamicip"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/password"
	"github.com/ava-labs/avalanchego/utils/timer"
	"github.com/ava-labs/avalanchego/utils/ulimit"
)

const (
	avalanchegoLatest     = "avalanchego-latest"
	avalanchegoPreupgrade = "avalanchego-preupgrade"
	chainConfigFileName   = "config"
	chainUpgradeFileName  = "upgrade"
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
	// build
	// |_avalanchego-latest
	//   |_avalanchego-process (the binary from compiling the app directory)
	//   |_plugins
	//     |_evm
	// |_avalanchego-preupgrade
	//   |_avalanchego-process (the binary from compiling the app directory)
	//   |_plugins
	//     |_evm
	validBuildDir := func(dir string) bool {
		info, err := os.Stat(dir)
		if err != nil || !info.IsDir() {
			return false
		}
		// make sure both expected subdirectories exist
		if _, err := os.Stat(filepath.Join(dir, avalanchegoLatest)); err != nil {
			return false
		}
		if _, err := os.Stat(filepath.Join(dir, avalanchegoPreupgrade)); err != nil {
			return false
		}
		return true
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

func GetNodeConfig(v *viper.Viper, buildDir string) (node.Config, error) {
	// TODO Divide this function into smaller parts (see getChainConfigs) for efficient testing
	// First, get the process config
	nodeConfig := node.Config{}

	// Plugin directory defaults to [buildDirectory]/avalanchego-latest/plugins
	nodeConfig.PluginDir = filepath.Join(buildDir, avalanchegoLatest, "plugins")

	nodeConfig.FetchOnly = v.GetBool(FetchOnlyKey)

	// Consensus Parameters
	nodeConfig.ConsensusParams.K = v.GetInt(SnowSampleSizeKey)
	nodeConfig.ConsensusParams.Alpha = v.GetInt(SnowQuorumSizeKey)
	nodeConfig.ConsensusParams.BetaVirtuous = v.GetInt(SnowVirtuousCommitThresholdKey)
	nodeConfig.ConsensusParams.BetaRogue = v.GetInt(SnowRogueCommitThresholdKey)
	nodeConfig.ConsensusParams.Parents = v.GetInt(SnowAvalancheNumParentsKey)
	nodeConfig.ConsensusParams.BatchSize = v.GetInt(SnowAvalancheBatchSizeKey)
	nodeConfig.ConsensusParams.ConcurrentRepolls = v.GetInt(SnowConcurrentRepollsKey)
	nodeConfig.ConsensusParams.OptimalProcessing = v.GetInt(SnowOptimalProcessingKey)
	nodeConfig.ConsensusParams.MaxOutstandingItems = v.GetInt(SnowMaxProcessingKey)
	nodeConfig.ConsensusParams.MaxItemProcessingTime = v.GetDuration(SnowMaxTimeProcessingKey)
	nodeConfig.ConsensusGossipFrequency = v.GetDuration(ConsensusGossipFrequencyKey)
	nodeConfig.ConsensusShutdownTimeout = v.GetDuration(ConsensusShutdownTimeoutKey)
	nodeConfig.ConsensusGossipAcceptedFrontierSize = uint(v.GetUint32(ConsensusGossipAcceptedFrontierSizeKey))
	nodeConfig.ConsensusGossipOnAcceptSize = uint(v.GetUint32(ConsensusGossipOnAcceptSizeKey))

	// Logging:
	loggingConfig, err := logging.DefaultConfig()
	if err != nil {
		return node.Config{}, err
	}
	if v.IsSet(LogsDirKey) {
		loggingConfig.Directory = os.ExpandEnv(v.GetString(LogsDirKey))
	}
	loggingConfig.LogLevel, err = logging.ToLevel(v.GetString(LogLevelKey))
	if err != nil {
		return node.Config{}, err
	}
	logDisplayLevel := v.GetString(LogLevelKey)
	if v.IsSet(LogDisplayLevelKey) {
		logDisplayLevel = v.GetString(LogDisplayLevelKey)
	}
	displayLevel, err := logging.ToLevel(logDisplayLevel)
	if err != nil {
		return node.Config{}, err
	}
	loggingConfig.DisplayLevel = displayLevel

	loggingConfig.DisplayHighlight, err = logging.ToHighlight(v.GetString(LogDisplayHighlightKey), os.Stdout.Fd())
	if err != nil {
		return node.Config{}, err
	}

	nodeConfig.LoggingConfig = loggingConfig

	// NetworkID
	networkID, err := constants.NetworkID(v.GetString(NetworkNameKey))
	if err != nil {
		return node.Config{}, err
	}
	nodeConfig.NetworkID = networkID

	// DB:
	nodeConfig.DBName = v.GetString(DBTypeKey)
	nodeConfig.DBPath = filepath.Join(
		os.ExpandEnv(v.GetString(DBPathKey)),
		constants.NetworkName(nodeConfig.NetworkID),
	)

	// IP configuration
	// Resolves our public IP, or does nothing
	nodeConfig.DynamicPublicIPResolver = dynamicip.NewResolver(v.GetString(DynamicPublicIPResolverKey))

	var ip net.IP
	publicIP := v.GetString(PublicIPKey)
	switch {
	case nodeConfig.DynamicPublicIPResolver.IsResolver():
		// User specified to use dynamic IP resolution; don't use NAT traversal
		nodeConfig.Nat = nat.NewNoRouter()
		ip, err = dynamicip.FetchExternalIP(nodeConfig.DynamicPublicIPResolver)
		if err != nil {
			return node.Config{}, fmt.Errorf("dynamic ip address fetch failed: %s", err)
		}

	case publicIP == "":
		// User didn't specify a public IP to use; try with NAT traversal
		nodeConfig.AttemptedNATTraversal = true
		nodeConfig.Nat = nat.GetRouter()
		ip, err = nodeConfig.Nat.ExternalIP()
		if err != nil {
			ip = net.IPv4zero // Couldn't get my IP...set to 0.0.0.0
		}
	default:
		// User specified a public IP to use; don't use NAT
		nodeConfig.Nat = nat.NewNoRouter()
		ip = net.ParseIP(publicIP)
	}

	if ip == nil {
		return node.Config{}, fmt.Errorf("invalid IP Address %s", publicIP)
	}

	stakingPort := uint16(v.GetUint(StakingPortKey))

	nodeConfig.StakingIP = utils.NewDynamicIPDesc(ip, stakingPort)

	nodeConfig.DynamicUpdateDuration = v.GetDuration(DynamicUpdateDurationKey)
	nodeConfig.ConnMeterResetDuration = v.GetDuration(ConnMeterResetDurationKey)
	nodeConfig.ConnMeterMaxConns = v.GetInt(ConnMeterMaxConnsKey)

	// Staking:
	nodeConfig.EnableStaking = v.GetBool(StakingEnabledKey)
	nodeConfig.DisabledStakingWeight = v.GetUint64(StakingDisabledWeightKey)
	nodeConfig.MinStakeDuration = v.GetDuration(MinStakeDurationKey)
	nodeConfig.MaxStakeDuration = v.GetDuration(MaxStakeDurationKey)
	nodeConfig.StakeMintingPeriod = v.GetDuration(StakeMintingPeriodKey)
	if !nodeConfig.EnableStaking && nodeConfig.DisabledStakingWeight == 0 {
		return node.Config{}, errInvalidStakerWeights
	}

	if nodeConfig.FetchOnly || v.GetBool(StakingEphemeralCertEnabledKey) {
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

	if err := initBootstrapPeers(v, &nodeConfig); err != nil {
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

	// HTTP:
	nodeConfig.HTTPHost = v.GetString(HTTPHostKey)
	nodeConfig.HTTPPort = uint16(v.GetUint(HTTPPortKey))
	nodeConfig.HTTPSEnabled = v.GetBool(HTTPSEnabledKey)
	nodeConfig.HTTPSKeyFile = os.ExpandEnv(v.GetString(HTTPSKeyFileKey))
	nodeConfig.HTTPSCertFile = os.ExpandEnv(v.GetString(HTTPSCertFileKey))
	nodeConfig.APIAllowedOrigins = v.GetStringSlice(HTTPAllowedOrigins)

	// API Auth
	nodeConfig.APIRequireAuthToken = v.GetBool(APIAuthRequiredKey)
	if nodeConfig.APIRequireAuthToken {
		passwordFile := v.GetString(APIAuthPasswordFileKey)
		pwBytes, err := ioutil.ReadFile(passwordFile)
		if err != nil {
			return node.Config{}, fmt.Errorf("api-auth-password-file %q failed to be read with: %w", passwordFile, err)
		}
		nodeConfig.APIAuthPassword = strings.TrimSpace(string(pwBytes))
		if !password.SufficientlyStrong(nodeConfig.APIAuthPassword, password.OK) {
			return node.Config{}, errors.New("api-auth-password is not strong enough")
		}
	}

	// APIs
	nodeConfig.AdminAPIEnabled = v.GetBool(AdminAPIEnabledKey)
	nodeConfig.InfoAPIEnabled = v.GetBool(InfoAPIEnabledKey)
	nodeConfig.KeystoreAPIEnabled = v.GetBool(KeystoreAPIEnabledKey)
	nodeConfig.MetricsAPIEnabled = v.GetBool(MetricsAPIEnabledKey)
	nodeConfig.HealthAPIEnabled = v.GetBool(HealthAPIEnabledKey)
	nodeConfig.IPCAPIEnabled = v.GetBool(IpcAPIEnabledKey)
	nodeConfig.IndexAPIEnabled = v.GetBool(IndexEnabledKey)

	// Halflife of continuous averager used in health checks
	healthCheckAveragerHalflife := v.GetDuration(HealthCheckAveragerHalflifeKey)
	if healthCheckAveragerHalflife <= 0 {
		return node.Config{}, fmt.Errorf("%s must be positive", HealthCheckAveragerHalflifeKey)
	}

	// Router
	nodeConfig.ConsensusRouter = &router.ChainRouter{}
	nodeConfig.RouterHealthConfig.MaxDropRate = v.GetFloat64(RouterHealthMaxDropRateKey)
	nodeConfig.RouterHealthConfig.MaxOutstandingRequests = int(v.GetUint(RouterHealthMaxOutstandingRequestsKey))
	nodeConfig.RouterHealthConfig.MaxOutstandingDuration = v.GetDuration(NetworkHealthMaxOutstandingDurationKey)
	nodeConfig.RouterHealthConfig.MaxRunTimeRequests = v.GetDuration(NetworkMaximumTimeoutKey)
	nodeConfig.RouterHealthConfig.MaxDropRateHalflife = healthCheckAveragerHalflife
	switch {
	case nodeConfig.RouterHealthConfig.MaxDropRate < 0 || nodeConfig.RouterHealthConfig.MaxDropRate > 1:
		return node.Config{}, fmt.Errorf("%s must be in [0,1]", RouterHealthMaxDropRateKey)
	case nodeConfig.RouterHealthConfig.MaxOutstandingDuration <= 0:
		return node.Config{}, fmt.Errorf("%s must be positive", NetworkHealthMaxOutstandingDurationKey)
	}

	// IPCs
	if v.IsSet(IpcsChainIDsKey) {
		nodeConfig.IPCDefaultChainIDs = strings.Split(v.GetString(IpcsChainIDsKey), ",")
	}

	if v.IsSet(IpcsPathKey) {
		nodeConfig.IPCPath = os.ExpandEnv(v.GetString(IpcsPathKey))
	} else {
		nodeConfig.IPCPath = ipcs.DefaultBaseURL
	}

	// Metrics
	nodeConfig.MeterVMEnabled = v.GetBool(MeterVMsEnabledKey)

	// Throttling
	nodeConfig.NetworkConfig.InboundThrottlerConfig = throttling.MsgThrottlerConfig{
		AtLargeAllocSize:    v.GetUint64(InboundThrottlerAtLargeAllocSizeKey),
		VdrAllocSize:        v.GetUint64(InboundThrottlerVdrAllocSizeKey),
		NodeMaxAtLargeBytes: v.GetUint64(InboundThrottlerNodeMaxAtLargeBytesKey),
	}
	nodeConfig.NetworkConfig.OutboundThrottlerConfig = throttling.MsgThrottlerConfig{
		AtLargeAllocSize:    v.GetUint64(OutboundThrottlerAtLargeAllocSizeKey),
		VdrAllocSize:        v.GetUint64(OutboundThrottlerVdrAllocSizeKey),
		NodeMaxAtLargeBytes: v.GetUint64(OutboundThrottlerNodeMaxAtLargeBytesKey),
	}

	// Health
	nodeConfig.HealthCheckFreq = v.GetDuration(HealthCheckFreqKey)
	// Network Health Check
	nodeConfig.NetworkConfig.HealthConfig = network.HealthConfig{
		MaxTimeSinceMsgSent:          v.GetDuration(NetworkHealthMaxTimeSinceMsgSentKey),
		MaxTimeSinceMsgReceived:      v.GetDuration(NetworkHealthMaxTimeSinceMsgReceivedKey),
		MaxPortionSendQueueBytesFull: v.GetFloat64(NetworkHealthMaxPortionSendQueueFillKey),
		MinConnectedPeers:            v.GetUint(NetworkHealthMinPeersKey),
		MaxSendFailRate:              v.GetFloat64(NetworkHealthMaxSendFailRateKey),
		MaxSendFailRateHalflife:      healthCheckAveragerHalflife,
	}
	switch {
	case nodeConfig.NetworkConfig.HealthConfig.MaxTimeSinceMsgSent < 0:
		return node.Config{}, fmt.Errorf("%s must be > 0", NetworkHealthMaxTimeSinceMsgSentKey)
	case nodeConfig.NetworkConfig.HealthConfig.MaxTimeSinceMsgReceived < 0:
		return node.Config{}, fmt.Errorf("%s must be > 0", NetworkHealthMaxTimeSinceMsgReceivedKey)
	case nodeConfig.NetworkConfig.HealthConfig.MaxSendFailRate < 0 || nodeConfig.NetworkConfig.HealthConfig.MaxSendFailRate > 1:
		return node.Config{}, fmt.Errorf("%s must be in [0,1]", NetworkHealthMaxSendFailRateKey)
	case nodeConfig.NetworkConfig.HealthConfig.MaxPortionSendQueueBytesFull < 0 || nodeConfig.NetworkConfig.HealthConfig.MaxPortionSendQueueBytesFull > 1:
		return node.Config{}, fmt.Errorf("%s must be in [0,1]", NetworkHealthMaxPortionSendQueueFillKey)
	}

	// Network Timeout
	nodeConfig.NetworkConfig.AdaptiveTimeoutConfig = timer.AdaptiveTimeoutConfig{
		InitialTimeout:     v.GetDuration(NetworkInitialTimeoutKey),
		MinimumTimeout:     v.GetDuration(NetworkMinimumTimeoutKey),
		MaximumTimeout:     v.GetDuration(NetworkMaximumTimeoutKey),
		TimeoutHalflife:    v.GetDuration(NetworkTimeoutHalflifeKey),
		TimeoutCoefficient: v.GetFloat64(NetworkTimeoutCoefficientKey),
	}
	switch {
	case nodeConfig.NetworkConfig.MinimumTimeout < 1:
		return node.Config{}, errors.New("minimum timeout must be positive")
	case nodeConfig.NetworkConfig.MinimumTimeout > nodeConfig.NetworkConfig.MaximumTimeout:
		return node.Config{}, errors.New("maximum timeout can't be less than minimum timeout")
	case nodeConfig.NetworkConfig.InitialTimeout < nodeConfig.NetworkConfig.MinimumTimeout ||
		nodeConfig.NetworkConfig.InitialTimeout > nodeConfig.NetworkConfig.MaximumTimeout:
		return node.Config{}, errors.New("initial timeout should be in the range [minimumTimeout, maximumTimeout]")
	case nodeConfig.NetworkConfig.TimeoutHalflife <= 0:
		return node.Config{}, errors.New("network timeout halflife must be positive")
	case nodeConfig.NetworkConfig.TimeoutCoefficient < 1:
		return node.Config{}, errors.New("network timeout coefficient must be >= 1")
	}

	// Metrics Namespace
	nodeConfig.NetworkConfig.MetricsNamespace = constants.PlatformName

	// Node will gossip [PeerListSize] peers to [PeerListGossipSize] every
	// [PeerListGossipFreq]
	nodeConfig.PeerListSize = v.GetUint32(NetworkPeerListSizeKey)
	nodeConfig.PeerListGossipFreq = v.GetDuration(NetworkPeerListGossipFreqKey)
	nodeConfig.PeerListGossipSize = v.GetUint32(NetworkPeerListGossipSizeKey)

	// Outbound connection throttling
	nodeConfig.NetworkConfig.DialerConfig = dialer.NewConfig(
		v.GetUint32(OutboundConnectionThrottlingRps),
		v.GetDuration(OutboundConnectionTimeout),
	)

	// Benchlist
	nodeConfig.BenchlistConfig.Threshold = v.GetInt(BenchlistFailThresholdKey)
	nodeConfig.BenchlistConfig.PeerSummaryEnabled = v.GetBool(BenchlistPeerSummaryEnabledKey)
	nodeConfig.BenchlistConfig.Duration = v.GetDuration(BenchlistDurationKey)
	nodeConfig.BenchlistConfig.MinimumFailingDuration = v.GetDuration(BenchlistMinFailingDurationKey)
	nodeConfig.BenchlistConfig.MaxPortion = (1.0 - (float64(nodeConfig.ConsensusParams.Alpha) / float64(nodeConfig.ConsensusParams.K))) / 3.0

	if nodeConfig.ConsensusGossipFrequency < 0 {
		return node.Config{}, errors.New("gossip frequency can't be negative")
	}
	if nodeConfig.ConsensusShutdownTimeout < 0 {
		return node.Config{}, errors.New("gossip frequency can't be negative")
	}

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
	nodeConfig.RetryBootstrap = v.GetBool(RetryBootstrapKey)
	nodeConfig.RetryBootstrapMaxAttempts = v.GetInt(RetryBootstrapMaxAttemptsKey)
	nodeConfig.BootstrapBeaconConnectionTimeout = v.GetDuration(BootstrapBeaconConnectionTimeoutKey)
	nodeConfig.BootstrapMaxTimeGetAncestors = v.GetDuration(BootstrapMaxTimeGetAncestorsKey)
	nodeConfig.BootstrapMultiputMaxContainersSent = int(v.GetUint(BootstrapMultiputMaxContainersSentKey))
	nodeConfig.BootstrapMultiputMaxContainersReceived = int(v.GetUint(BootstrapMultiputMaxContainersReceivedKey))

	// Peer alias
	nodeConfig.PeerAliasTimeout = v.GetDuration(PeerAliasTimeoutKey)

	// Chain Configs
	chainConfigs, err := getChainConfigs(v)
	if err != nil {
		return node.Config{}, err
	}
	nodeConfig.ChainConfigs = chainConfigs

	// Profile config
	nodeConfig.ProfilerConfig.Dir = os.ExpandEnv(v.GetString(ProfileDirKey))
	nodeConfig.ProfilerConfig.Enabled = v.GetBool(ProfileContinuousEnabledKey)
	nodeConfig.ProfilerConfig.Freq = v.GetDuration(ProfileContinuousFreqKey)
	nodeConfig.ProfilerConfig.MaxNumFiles = v.GetInt(ProfileContinuousMaxFilesKey)

	// VM Aliases
	vmAliases, err := readVMAliases(v)
	if err != nil {
		return node.Config{}, err
	}
	nodeConfig.VMAliases = vmAliases
	return nodeConfig, nil
}

func readVMAliases(v *viper.Viper) (map[ids.ID][]string, error) {
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
func initBootstrapPeers(v *viper.Viper, config *node.Config) error {
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
