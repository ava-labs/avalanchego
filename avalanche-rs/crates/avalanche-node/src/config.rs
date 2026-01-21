//! Node configuration.

use std::path::{Path, PathBuf};
use std::net::IpAddr;

use serde::{Deserialize, Serialize};
use thiserror::Error;

/// Node configuration.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Config {
    /// Data directory for databases and state
    pub data_dir: PathBuf,
    /// Network configuration
    pub network: NetworkConfig,
    /// API configuration
    pub api: ApiConfig,
    /// Database configuration
    pub database: DatabaseConfig,
    /// Staking configuration
    pub staking: StakingConfig,
    /// Logging configuration
    pub logging: LoggingConfig,
}

impl Default for Config {
    fn default() -> Self {
        Self::default_for_network(1)
    }
}

impl Config {
    /// Creates default configuration for a network.
    pub fn default_for_network(network_id: u32) -> Self {
        let data_dir = directories::ProjectDirs::from("com", "avalanche", "avalanche-node")
            .map(|d| d.data_dir().to_path_buf())
            .unwrap_or_else(|| PathBuf::from(".avalanche"));

        Self {
            data_dir,
            network: NetworkConfig {
                network_id,
                staking_port: 9651,
                public_ip: None,
                bootstrap_nodes: default_bootstrap_nodes(network_id),
                max_peers: 50,
                peer_list_gossip_frequency_secs: 60,
            },
            api: ApiConfig {
                http_host: "127.0.0.1".parse().unwrap(),
                http_port: 9650,
                https_enabled: false,
                https_port: 9650,
                admin_api_enabled: false,
                info_api_enabled: true,
                health_api_enabled: true,
                metrics_api_enabled: true,
            },
            database: DatabaseConfig {
                db_type: "leveldb".to_string(),
                db_dir: None,
            },
            staking: StakingConfig {
                staking_enabled: network_id != 12345, // Disabled for local
                staking_tls_cert_file: None,
                staking_tls_key_file: None,
                staking_signer_key_file: None,
            },
            logging: LoggingConfig {
                log_level: "info".to_string(),
                log_display_level: "info".to_string(),
                log_dir: None,
            },
        }
    }

    /// Loads configuration from a file.
    pub fn load(path: &Path) -> Result<Self, ConfigError> {
        let content = std::fs::read_to_string(path)
            .map_err(|e| ConfigError::IoError(e.to_string()))?;

        toml::from_str(&content)
            .map_err(|e| ConfigError::ParseError(e.to_string()))
    }

    /// Saves configuration to a file.
    pub fn save(&self, path: &Path) -> Result<(), ConfigError> {
        let content = toml::to_string_pretty(self)
            .map_err(|e| ConfigError::SerializeError(e.to_string()))?;

        std::fs::write(path, content)
            .map_err(|e| ConfigError::IoError(e.to_string()))
    }

    /// Validates the configuration.
    pub fn validate(&self) -> Result<(), ConfigError> {
        // Check network ID
        if self.network.network_id == 0 {
            return Err(ConfigError::InvalidValue("network_id cannot be 0".to_string()));
        }

        // Check ports
        if self.api.http_port == 0 {
            return Err(ConfigError::InvalidValue("http_port cannot be 0".to_string()));
        }
        if self.network.staking_port == 0 {
            return Err(ConfigError::InvalidValue("staking_port cannot be 0".to_string()));
        }

        // Check staking configuration
        if self.staking.staking_enabled {
            if self.staking.staking_tls_cert_file.is_none() {
                return Err(ConfigError::MissingValue(
                    "staking_tls_cert_file required when staking is enabled".to_string()
                ));
            }
            if self.staking.staking_tls_key_file.is_none() {
                return Err(ConfigError::MissingValue(
                    "staking_tls_key_file required when staking is enabled".to_string()
                ));
            }
        }

        Ok(())
    }

    /// Returns the database directory.
    pub fn db_dir(&self) -> PathBuf {
        self.database.db_dir.clone()
            .unwrap_or_else(|| self.data_dir.join("db"))
    }

    /// Returns the log directory.
    pub fn log_dir(&self) -> PathBuf {
        self.logging.log_dir.clone()
            .unwrap_or_else(|| self.data_dir.join("logs"))
    }
}

/// Network configuration.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct NetworkConfig {
    /// Network ID (1 = mainnet, 5 = fuji, 12345 = local)
    pub network_id: u32,
    /// Port for staking/P2P connections
    pub staking_port: u16,
    /// Public IP address (for advertising to peers)
    pub public_ip: Option<IpAddr>,
    /// Bootstrap nodes
    pub bootstrap_nodes: Vec<String>,
    /// Maximum number of peers
    pub max_peers: usize,
    /// How often to gossip peer list (seconds)
    pub peer_list_gossip_frequency_secs: u64,
}

/// API configuration.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ApiConfig {
    /// HTTP host to bind to
    pub http_host: IpAddr,
    /// HTTP port
    pub http_port: u16,
    /// Enable HTTPS
    pub https_enabled: bool,
    /// HTTPS port
    pub https_port: u16,
    /// Enable admin API
    pub admin_api_enabled: bool,
    /// Enable info API
    pub info_api_enabled: bool,
    /// Enable health API
    pub health_api_enabled: bool,
    /// Enable metrics API
    pub metrics_api_enabled: bool,
}

/// Database configuration.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct DatabaseConfig {
    /// Database type (leveldb, memdb)
    pub db_type: String,
    /// Database directory (defaults to data_dir/db)
    pub db_dir: Option<PathBuf>,
}

/// Staking configuration.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct StakingConfig {
    /// Whether staking is enabled
    pub staking_enabled: bool,
    /// Path to TLS certificate
    pub staking_tls_cert_file: Option<PathBuf>,
    /// Path to TLS key
    pub staking_tls_key_file: Option<PathBuf>,
    /// Path to BLS signer key
    pub staking_signer_key_file: Option<PathBuf>,
}

/// Logging configuration.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct LoggingConfig {
    /// Log level
    pub log_level: String,
    /// Display log level
    pub log_display_level: String,
    /// Log directory (defaults to data_dir/logs)
    pub log_dir: Option<PathBuf>,
}

/// Configuration errors.
#[derive(Debug, Error)]
pub enum ConfigError {
    #[error("IO error: {0}")]
    IoError(String),
    #[error("parse error: {0}")]
    ParseError(String),
    #[error("serialize error: {0}")]
    SerializeError(String),
    #[error("invalid value: {0}")]
    InvalidValue(String),
    #[error("missing value: {0}")]
    MissingValue(String),
}

/// Returns default bootstrap nodes for a network.
fn default_bootstrap_nodes(network_id: u32) -> Vec<String> {
    match network_id {
        1 => vec![
            // Mainnet bootstrap nodes
            "NodeID-7Xhw2mDxuDS44j42TCB6U5579esbSt3Lg@bootstrap-mainnet.avax.network:9651".to_string(),
        ],
        5 => vec![
            // Fuji bootstrap nodes
            "NodeID-7Xhw2mDxuDS44j42TCB6U5579esbSt3Lg@bootstrap-fuji.avax.network:9651".to_string(),
        ],
        12345 => vec![
            // Local network - no bootstrap needed
        ],
        _ => vec![],
    }
}

/// Network ID constants.
pub mod network_ids {
    /// Mainnet
    pub const MAINNET: u32 = 1;
    /// Fuji testnet
    pub const FUJI: u32 = 5;
    /// Local test network
    pub const LOCAL: u32 = 12345;
}

#[cfg(test)]
mod tests {
    use super::*;
    use tempfile::tempdir;

    #[test]
    fn test_default_config() {
        let config = Config::default();
        assert_eq!(config.network.network_id, 1);
        assert!(config.validate().is_err()); // Staking enabled but no certs
    }

    #[test]
    fn test_local_config() {
        let config = Config::default_for_network(12345);
        assert_eq!(config.network.network_id, 12345);
        assert!(!config.staking.staking_enabled);
        assert!(config.validate().is_ok());
    }

    #[test]
    fn test_config_save_load() {
        let dir = tempdir().unwrap();
        let path = dir.path().join("config.toml");

        let config = Config::default_for_network(12345);
        config.save(&path).unwrap();

        let loaded = Config::load(&path).unwrap();
        assert_eq!(loaded.network.network_id, 12345);
    }

    #[test]
    fn test_config_validation() {
        let mut config = Config::default_for_network(12345);
        assert!(config.validate().is_ok());

        config.network.network_id = 0;
        assert!(config.validate().is_err());
    }
}
