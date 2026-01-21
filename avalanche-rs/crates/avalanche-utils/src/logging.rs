//! Logging configuration utilities.
//!
//! This module provides utilities for configuring the tracing-based
//! logging system used throughout Avalanche.

use tracing::Level;
use tracing_subscriber::{
    fmt::{self, format::FmtSpan},
    prelude::*,
    EnvFilter,
};

/// Log level configuration.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Default)]
pub enum LogLevel {
    /// Trace level - very verbose.
    Trace,
    /// Debug level - debug information.
    Debug,
    /// Info level - general information.
    #[default]
    Info,
    /// Warn level - warnings.
    Warn,
    /// Error level - errors only.
    Error,
}

impl LogLevel {
    /// Converts to a tracing Level.
    #[must_use]
    pub const fn to_tracing_level(self) -> Level {
        match self {
            Self::Trace => Level::TRACE,
            Self::Debug => Level::DEBUG,
            Self::Info => Level::INFO,
            Self::Warn => Level::WARN,
            Self::Error => Level::ERROR,
        }
    }
}

impl From<LogLevel> for Level {
    fn from(level: LogLevel) -> Self {
        level.to_tracing_level()
    }
}

impl std::str::FromStr for LogLevel {
    type Err = String;

    fn from_str(s: &str) -> Result<Self, Self::Err> {
        match s.to_lowercase().as_str() {
            "trace" => Ok(Self::Trace),
            "debug" => Ok(Self::Debug),
            "info" => Ok(Self::Info),
            "warn" | "warning" => Ok(Self::Warn),
            "error" => Ok(Self::Error),
            _ => Err(format!("invalid log level: {s}")),
        }
    }
}

impl std::fmt::Display for LogLevel {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::Trace => write!(f, "trace"),
            Self::Debug => write!(f, "debug"),
            Self::Info => write!(f, "info"),
            Self::Warn => write!(f, "warn"),
            Self::Error => write!(f, "error"),
        }
    }
}

/// Configuration for the logging system.
#[derive(Debug, Clone)]
pub struct LogConfig {
    /// The minimum log level.
    pub level: LogLevel,
    /// Whether to include timestamps.
    pub timestamps: bool,
    /// Whether to include target (module path).
    pub target: bool,
    /// Whether to include file and line numbers.
    pub file_line: bool,
    /// Whether to output in JSON format.
    pub json: bool,
    /// Whether to include span events.
    pub span_events: bool,
}

impl Default for LogConfig {
    fn default() -> Self {
        Self {
            level: LogLevel::Info,
            timestamps: true,
            target: true,
            file_line: false,
            json: false,
            span_events: false,
        }
    }
}

impl LogConfig {
    /// Creates a new log configuration with default values.
    #[must_use]
    pub fn new() -> Self {
        Self::default()
    }

    /// Sets the log level.
    #[must_use]
    pub const fn with_level(mut self, level: LogLevel) -> Self {
        self.level = level;
        self
    }

    /// Enables or disables timestamps.
    #[must_use]
    pub const fn with_timestamps(mut self, enabled: bool) -> Self {
        self.timestamps = enabled;
        self
    }

    /// Enables or disables target (module path) output.
    #[must_use]
    pub const fn with_target(mut self, enabled: bool) -> Self {
        self.target = enabled;
        self
    }

    /// Enables or disables file and line number output.
    #[must_use]
    pub const fn with_file_line(mut self, enabled: bool) -> Self {
        self.file_line = enabled;
        self
    }

    /// Enables or disables JSON output format.
    #[must_use]
    pub const fn with_json(mut self, enabled: bool) -> Self {
        self.json = enabled;
        self
    }

    /// Enables or disables span event logging.
    #[must_use]
    pub const fn with_span_events(mut self, enabled: bool) -> Self {
        self.span_events = enabled;
        self
    }
}

/// Initializes the logging system with the given configuration.
///
/// This should be called once at the start of the application.
///
/// # Panics
///
/// Panics if the logging system has already been initialized.
pub fn init(config: &LogConfig) {
    let filter = EnvFilter::new(config.level.to_string());

    let span_events = if config.span_events {
        FmtSpan::NEW | FmtSpan::CLOSE
    } else {
        FmtSpan::NONE
    };

    if config.json {
        let subscriber = tracing_subscriber::registry().with(filter).with(
            fmt::layer()
                .json()
                .with_target(config.target)
                .with_file(config.file_line)
                .with_line_number(config.file_line)
                .with_span_events(span_events),
        );
        tracing::subscriber::set_global_default(subscriber)
            .expect("failed to set global subscriber");
    } else {
        let subscriber = tracing_subscriber::registry().with(filter).with(
            fmt::layer()
                .with_target(config.target)
                .with_file(config.file_line)
                .with_line_number(config.file_line)
                .with_span_events(span_events),
        );
        tracing::subscriber::set_global_default(subscriber)
            .expect("failed to set global subscriber");
    }
}

/// Initializes the logging system with default configuration.
///
/// This is a convenience function for simple use cases.
pub fn init_default() {
    init(&LogConfig::default());
}

/// Tries to initialize logging, ignoring errors if already initialized.
///
/// This is useful in tests where multiple tests might try to initialize logging.
pub fn try_init(config: &LogConfig) {
    let filter = EnvFilter::new(config.level.to_string());

    let span_events = if config.span_events {
        FmtSpan::NEW | FmtSpan::CLOSE
    } else {
        FmtSpan::NONE
    };

    if config.json {
        let subscriber = tracing_subscriber::registry().with(filter).with(
            fmt::layer()
                .json()
                .with_target(config.target)
                .with_file(config.file_line)
                .with_line_number(config.file_line)
                .with_span_events(span_events),
        );
        let _ = tracing::subscriber::set_global_default(subscriber);
    } else {
        let subscriber = tracing_subscriber::registry().with(filter).with(
            fmt::layer()
                .with_target(config.target)
                .with_file(config.file_line)
                .with_line_number(config.file_line)
                .with_span_events(span_events),
        );
        let _ = tracing::subscriber::set_global_default(subscriber);
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_log_level_parse() {
        assert_eq!("trace".parse::<LogLevel>().unwrap(), LogLevel::Trace);
        assert_eq!("debug".parse::<LogLevel>().unwrap(), LogLevel::Debug);
        assert_eq!("info".parse::<LogLevel>().unwrap(), LogLevel::Info);
        assert_eq!("warn".parse::<LogLevel>().unwrap(), LogLevel::Warn);
        assert_eq!("warning".parse::<LogLevel>().unwrap(), LogLevel::Warn);
        assert_eq!("error".parse::<LogLevel>().unwrap(), LogLevel::Error);
        assert!("invalid".parse::<LogLevel>().is_err());
    }

    #[test]
    fn test_log_level_display() {
        assert_eq!(LogLevel::Trace.to_string(), "trace");
        assert_eq!(LogLevel::Debug.to_string(), "debug");
        assert_eq!(LogLevel::Info.to_string(), "info");
        assert_eq!(LogLevel::Warn.to_string(), "warn");
        assert_eq!(LogLevel::Error.to_string(), "error");
    }

    #[test]
    fn test_log_config_builder() {
        let config = LogConfig::new()
            .with_level(LogLevel::Debug)
            .with_timestamps(false)
            .with_json(true);

        assert_eq!(config.level, LogLevel::Debug);
        assert!(!config.timestamps);
        assert!(config.json);
    }
}
