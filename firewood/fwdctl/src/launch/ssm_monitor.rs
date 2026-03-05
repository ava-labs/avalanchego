// Copyright (C) 2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use std::time::{Duration, Instant};

use super::LaunchError;
use super::cloud_init::STATE_FILE;
use crate::launch::ec2_util::aws_config;
use aws_sdk_ssm::Client as SsmClient;
use aws_sdk_ssm::error::ProvideErrorMetadata;
use aws_sdk_ssm::types::{InstanceInformationFilter, InstanceInformationFilterKey, PingStatus};
use log::info;
use serde::Deserialize;
use serde::de::DeserializeOwned;
use tokio::time::sleep;

/// Maximum attempts while waiting for an instance to register as SSM `Online`.
const SSM_MAX_RETRIES: u32 = 30;
/// Delay between SSM registration retry attempts.
const SSM_RETRY_DELAY: Duration = Duration::from_secs(5);
/// Poll interval while waiting for an SSM command invocation status update.
const SSM_COMMAND_POLL_INTERVAL: Duration = Duration::from_millis(500);
/// Timeout for a single SSM command invocation.
const SSM_COMMAND_TIMEOUT: Duration = Duration::from_secs(120);
/// Poll interval for cloud-init state and bootstrap log streaming loops.
const LOG_POLL_INTERVAL: Duration = Duration::from_secs(3);
/// Number of log lines fetched per SSM read when tailing bootstrap output.
const LOG_CHUNK_SIZE: u64 = 500;
/// Log file containing benchmark re-execution output on the remote host.
const BOOTSTRAP_LOG: &str = "/var/log/bootstrap.log";
/// Maximum time to wait for cloud-init to publish its JSON state file.
const STATE_FILE_TIMEOUT: Duration = Duration::from_secs(600);

pub async fn ssm_client(region: &str) -> SsmClient {
    SsmClient::new(&aws_config(Some(region)).await)
}

pub async fn wait_for_ssm_registration(
    ssm: &SsmClient,
    instance_id: &str,
) -> Result<(), LaunchError> {
    let filter = InstanceInformationFilter::builder()
        .key(InstanceInformationFilterKey::InstanceIds)
        .value_set(instance_id)
        .build()
        .map_err(|e| LaunchError::AwsSdk(format!("failed to build SSM filter: {e}")))?;
    let mut last_error = None;

    for attempt in 1..=SSM_MAX_RETRIES {
        match ssm
            .describe_instance_information()
            .instance_information_filter_list(filter.clone())
            .send()
            .await
        {
            Ok(info) => {
                if info
                    .instance_information_list()
                    .iter()
                    .any(|i| i.ping_status() == Some(&PingStatus::Online))
                {
                    return Ok(());
                }
            }
            Err(err) => {
                if matches!(
                    err.as_service_error().and_then(|e| e.code()),
                    Some("AccessDeniedException" | "UnauthorizedOperation")
                ) {
                    return Err(err.into());
                }
                last_error = Some(err.to_string());
                log::debug!("describe_instance_information attempt {attempt} failed: {err}");
            }
        }

        log::debug!("Waiting for SSM registration ({attempt}/{SSM_MAX_RETRIES})");
        sleep(SSM_RETRY_DELAY).await;
    }

    let detail = last_error.unwrap_or_else(|| "no registration observed".to_owned());
    Err(LaunchError::AwsSdk(format!(
        "instance {instance_id} did not register with SSM: {detail}"
    )))
}

pub async fn stream_logs_via_ssm(
    ssm: &SsmClient,
    instance_id: &str,
    observe: bool,
) -> Result<(), LaunchError> {
    let mut tracker = StageTracker::default();
    let started = Instant::now();
    let mut saw_state = false;

    loop {
        if let Some(state) =
            read_optional_json::<CloudInitState>(ssm, instance_id, STATE_FILE).await?
        {
            saw_state = true;
            tracker.update(&state);
            if state.status == "failed" {
                return Err(LaunchError::AwsSdk(failure_context(&state)));
            }
            if state.status == "completed" && state.step == state.total {
                info!("");
                info!("All {} stages completed.", state.total);
                break;
            }
        } else if !saw_state && started.elapsed() > STATE_FILE_TIMEOUT {
            return Err(LaunchError::Timeout(
                "cloud-init state file",
                STATE_FILE_TIMEOUT.as_secs(),
            ));
        }

        sleep(LOG_POLL_INTERVAL).await;
    }

    if observe {
        info!("Observing re-execution progress from {BOOTSTRAP_LOG}...");
        stream_bootstrap_log(ssm, instance_id).await?;
    }

    Ok(())
}

async fn stream_bootstrap_log(ssm: &SsmClient, instance_id: &str) -> Result<(), LaunchError> {
    let mut last_line = 0_u64;
    let mut observer = LogObserver::default();

    loop {
        let start_line = last_line.saturating_add(1);
        let (output, lines_read) =
            read_log_chunk(ssm, instance_id, BOOTSTRAP_LOG, start_line).await?;
        if !output.is_empty() {
            for line in output.lines() {
                if observer.process_line(line) {
                    observer.print_summary();
                    return Ok(());
                }
            }
            last_line = last_line.saturating_add(lines_read);
        }
        sleep(LOG_POLL_INTERVAL).await;
    }
}

#[derive(Debug, Deserialize)]
struct CloudInitState {
    step: usize,
    total: usize,
    name: String,
    status: String,
    #[serde(default)]
    stages: Vec<String>,
    #[serde(default)]
    last_error: Option<CommandError>,
}

#[derive(Debug, Deserialize)]
struct CommandError {
    stage: usize,
    cmd: usize,
    exit: i64,
}

#[derive(Default)]
struct StageTracker {
    shown_step: usize,
    shown_completed: bool,
}

impl StageTracker {
    fn update(&mut self, state: &CloudInitState) {
        for skipped in self.shown_step.saturating_add(1)..state.step {
            info!(
                "[{:>2}/{}] ✓ {}",
                skipped,
                state.total,
                stage_name(state, skipped)
            );
        }

        let is_complete = state.status == "completed";
        let should_print = state.step > self.shown_step
            || (state.step == self.shown_step && is_complete && !self.shown_completed);
        if should_print {
            let symbol = match state.status.as_str() {
                "completed" => "✓",
                "failed" => "✗",
                _ => "…",
            };
            info!(
                "[{:>2}/{}] {} {}",
                state.step, state.total, symbol, state.name
            );
            self.shown_step = state.step;
            self.shown_completed = is_complete;
        }
    }
}

fn failure_context(state: &CloudInitState) -> String {
    let Some(err) = &state.last_error else {
        return format!("stage {} [{}] failed", state.step, state.name);
    };
    format!(
        "stage {} [{}] failed (exit={}) command #{}",
        err.stage,
        stage_name(state, err.stage),
        err.exit,
        err.cmd
    )
}

fn stage_name(state: &CloudInitState, step: usize) -> &str {
    state.stages.get(step.saturating_sub(1)).map_or_else(
        || {
            if step == state.step {
                state.name.as_str()
            } else {
                "<unknown-stage>"
            }
        },
        String::as_str,
    )
}

#[derive(Default)]
struct LogObserver {
    last_progress: Option<(u64, f64)>,
    results: Vec<(String, String)>,
}

impl LogObserver {
    /// Process a line and return `true` once re-execution has finished.
    fn process_line(&mut self, line: &str) -> bool {
        if line.contains("executing block") && line.contains("progress_pct") {
            if let Some((height, pct, eta)) = Self::parse_progress(line)
                && self.should_show(height, pct)
            {
                eprint!("\r[{pct:>6.1}%] block {height:>10} | eta: {eta:>8}");
                self.last_progress = Some((height, pct));
            }
            return false;
        }

        if line.contains("BenchmarkReexecuteRange") && line.contains("result") {
            if let Some((metric, value)) = Self::parse_result(line) {
                self.results.push((metric, value));
            }
            return false;
        }

        if line.contains("finished executing sequence") {
            if self.last_progress.is_some() {
                eprintln!();
            }
            return true;
        }

        false
    }

    fn should_show(&self, height: u64, pct: f64) -> bool {
        match self.last_progress {
            None => true,
            Some((h, p)) => (pct - p).abs() >= 0.5 || height.saturating_sub(h) >= 50_000,
        }
    }

    fn parse_progress(line: &str) -> Option<(u64, f64, String)> {
        #[derive(Deserialize)]
        struct ProgressLine {
            height: u64,
            progress_pct: f64,
            #[serde(default)]
            eta: String,
        }

        let parsed: ProgressLine = parse_embedded_json(line)?;
        let eta = if parsed.eta.is_empty() {
            "-".into()
        } else {
            parsed.eta
        };
        Some((parsed.height, parsed.progress_pct, eta))
    }

    fn parse_result(line: &str) -> Option<(String, String)> {
        #[derive(Deserialize)]
        struct ResultLine {
            result: String,
        }

        let parsed: ResultLine = parse_embedded_json(line)?;
        let mut parts = parsed.result.split_whitespace();
        let value = parts.next()?.to_owned();
        let metric = parts.collect::<Vec<_>>().join(" ");
        Some(if metric.is_empty() {
            ("result".into(), value)
        } else {
            (metric, value)
        })
    }

    fn print_summary(&self) {
        if self.results.is_empty() {
            return;
        }
        info!("");
        info!("=== Benchmark Results ===");
        for (metric, value) in &self.results {
            info!("  {metric:<30} {value}");
        }
    }
}

fn parse_embedded_json<T: DeserializeOwned>(line: &str) -> Option<T> {
    let json_start = line.find('{')?;
    let json_end = line.rfind('}')?;
    serde_json::from_str(&line[json_start..=json_end]).ok()
}

async fn read_optional_json<T: DeserializeOwned>(
    ssm: &SsmClient,
    instance_id: &str,
    path: &str,
) -> Result<Option<T>, LaunchError> {
    let output =
        run_ssm_command(ssm, instance_id, &format!("cat {path} 2>/dev/null || true")).await?;
    let trimmed = output.trim();
    if trimmed.is_empty() {
        return Ok(None);
    }
    serde_json::from_str(trimmed).map(Some).map_err(|err| {
        LaunchError::AwsSdk(format!(
            "invalid JSON in {path}: {err}; content: {}",
            truncate_for_log(trimmed, 512)
        ))
    })
}

async fn read_log_chunk(
    ssm: &SsmClient,
    instance_id: &str,
    log_path: &str,
    start_line: u64,
) -> Result<(String, u64), LaunchError> {
    let end_line = start_line.saturating_add(LOG_CHUNK_SIZE.saturating_sub(1));
    let command = format!("sudo sed -n '{start_line},{end_line}p' {log_path} 2>/dev/null || true");
    let output = run_ssm_command(ssm, instance_id, &command).await?;
    let lines_read = output.lines().count() as u64;
    Ok((output, lines_read))
}

async fn run_ssm_command(
    ssm: &SsmClient,
    instance_id: &str,
    command: &str,
) -> Result<String, LaunchError> {
    let resp = ssm
        .send_command()
        .document_name("AWS-RunShellScript")
        .instance_ids(instance_id)
        .parameters("commands", vec![command.to_owned()])
        .send()
        .await?;

    let command_id = resp
        .command()
        .and_then(|c| c.command_id())
        .map(str::to_owned)
        .ok_or_else(|| LaunchError::AwsSdk("missing SSM command ID".into()))?;

    let started = Instant::now();
    loop {
        if started.elapsed() > SSM_COMMAND_TIMEOUT {
            return Err(LaunchError::Timeout(
                "ssm command",
                SSM_COMMAND_TIMEOUT.as_secs(),
            ));
        }

        sleep(SSM_COMMAND_POLL_INTERVAL).await;
        let invocation = ssm
            .get_command_invocation()
            .command_id(&command_id)
            .instance_id(instance_id)
            .send()
            .await;

        let resp = match invocation {
            Ok(resp) => resp,
            Err(err)
                if matches!(
                    err.as_service_error().and_then(|e| e.code()),
                    Some("InvocationDoesNotExist")
                ) =>
            {
                continue;
            }
            Err(err) => return Err(err.into()),
        };

        let status = resp.status().map_or(
            "Pending",
            aws_sdk_ssm::types::CommandInvocationStatus::as_str,
        );
        match status {
            "Pending" | "InProgress" | "Delayed" => {}
            "Success" => return Ok(resp.standard_output_content().unwrap_or("").to_owned()),
            _ => {
                let detail = resp
                    .standard_error_content()
                    .map(str::trim)
                    .filter(|s| !s.is_empty())
                    .or_else(|| {
                        resp.standard_output_content()
                            .map(str::trim)
                            .filter(|s| !s.is_empty())
                    })
                    .unwrap_or("no output");
                return Err(LaunchError::AwsSdk(format!(
                    "SSM command failed with status '{status}': {detail}"
                )));
            }
        }
    }
}

fn truncate_for_log(input: &str, max_chars: usize) -> String {
    let mut chars = input.chars();
    let truncated: String = chars.by_ref().take(max_chars).collect();
    if chars.next().is_some() {
        format!("{truncated}...")
    } else {
        truncated
    }
}
