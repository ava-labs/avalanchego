// Copyright (C) 2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use base64::Engine;
use base64::engine::general_purpose::STANDARD as BASE64;
use serde::Serialize;
use serde_json::json;
use std::collections::HashMap;

use super::stage_config::{StageConfig, TemplateContext};
use super::{DeployOptions, LaunchError};

#[derive(Serialize)]
struct CloudInitYaml {
    package_update: bool,
    package_upgrade: bool,
    packages: Vec<String>,
    users: Vec<serde_yaml::Value>,
    #[serde(skip_serializing_if = "Option::is_none")]
    swap: Option<SwapConfig>,
    write_files: Vec<WriteFile>,
    runcmd: Vec<String>,
}

#[derive(Serialize)]
struct SwapConfig {
    filename: &'static str,
    size: String,
    maxsize: String,
}

#[derive(Serialize, Clone)]
struct WriteFile {
    path: &'static str,
    permissions: &'static str,
    content: String,
}

pub struct CloudInitContext {
    swap_gib: u64,
    scenario_name: String,
    template_ctx: TemplateContext,
    config: StageConfig,
}

impl CloudInitContext {
    pub fn new(opts: &DeployOptions) -> Result<Self, LaunchError> {
        let config = match StageConfig::load() {
            Ok(c) => c,
            Err(e) => {
                log::warn!("Failed to load stage config: {e}, using embedded default");
                serde_yaml::from_str(include_str!("../../../benchmark/launch/launch-stages.yaml"))?
            }
        };
        let end_block = opts.end_block().to_string();
        let nblocks = opts.nblocks.as_str().to_owned();
        let cli_overrides = opts.variable_overrides_map();

        let template_ctx = TemplateContext {
            args: HashMap::from([
                ("end_block".into(), end_block),
                ("nblocks".into(), nblocks),
                ("config".into(), opts.config.clone()),
                ("metrics_server".into(), opts.metrics_server.to_string()),
            ]),
            branches: opts
                .branches()
                .into_iter()
                .map(|(name, branch)| {
                    (
                        name.into(),
                        branch.map_or_else(String::new, |value| format!("--branch {value}")),
                    )
                })
                .collect(),
            cli_overrides,
            ..TemplateContext::default()
        };
        Ok(Self {
            swap_gib: 16,
            scenario_name: opts.scenario_name().to_string(),
            template_ctx,
            config,
        })
    }

    pub fn render_yaml(&self) -> Result<String, LaunchError> {
        let yaml = self.build_yaml()?;
        let mut output = String::from("#cloud-config\n");
        output.push_str(&serde_yaml::to_string(&yaml)?);
        Ok(output)
    }

    pub fn render_base64(&self) -> Result<String, LaunchError> {
        Ok(BASE64.encode(self.render_yaml()?.as_bytes()))
    }

    fn build_yaml(&self) -> Result<CloudInitYaml, LaunchError> {
        Ok(CloudInitYaml {
            package_update: true,
            package_upgrade: true,
            packages: self.config.packages.clone(),
            users: self.build_users()?,
            swap: self.build_swap(),
            write_files: self.build_write_files(),
            runcmd: self.build_runcmd()?,
        })
    }

    fn build_users(&self) -> Result<Vec<serde_yaml::Value>, serde_yaml::Error> {
        let mut users = vec![serde_yaml::Value::String("default".into())];
        for user in &self.config.users {
            users.push(serde_yaml::to_value(user)?);
        }
        Ok(users)
    }

    fn build_swap(&self) -> Option<SwapConfig> {
        (self.swap_gib > 0).then(|| SwapConfig {
            filename: "/swapfile",
            size: format!("{}G", self.swap_gib),
            maxsize: format!("{}G", self.swap_gib),
        })
    }

    fn build_write_files(&self) -> Vec<WriteFile> {
        vec![
            WriteFile {
                path: "/etc/sudoers.d/91-cloud-init-enable-D-option",
                permissions: "0440",
                content: "Defaults runcwd=*".into(),
            },
            WriteFile {
                path: "/etc/profile.d/go_path.sh",
                permissions: "0644",
                content: r#"export PATH="$PATH:/usr/local/go/bin""#.into(),
            },
            WriteFile {
                path: "/etc/profile.d/rust_path.sh",
                permissions: "0644",
                content: concat!(
                    "export RUSTUP_HOME=/usr/local/rust\n",
                    r#"export PATH="$PATH:/usr/local/rust/bin""#
                )
                .into(),
            },
            WriteFile {
                path: STATE_HELPER,
                permissions: "0755",
                content: state_helper_script(),
            },
        ]
    }

    fn build_runcmd(&self) -> Result<Vec<String>, LaunchError> {
        let stages = self
            .config
            .process(&self.template_ctx, &self.scenario_name)?;
        let total = stages.len();
        let mut runcmd = Vec::new();
        let stage_names: Vec<_> = stages.iter().map(|stage| stage.name.as_str()).collect();
        let state = json!({
            "step": 0,
            "total": total,
            "name": "",
            "status": "pending",
            "stages": stage_names,
            "last_error": serde_json::Value::Null,
        });
        runcmd.push(write_json_file_command(STATE_FILE, &state)?);

        for (stage_idx, stage) in stages.iter().enumerate() {
            let step = stage_idx.saturating_add(1);
            runcmd.push(state_update_command(step, "in_progress"));

            for (cmd_idx, cmd) in stage.commands.iter().enumerate() {
                let cmd_num = cmd_idx.saturating_add(1);
                runcmd.push(cmd.clone());
                runcmd.push(state_fail_if_needed_command(step, cmd_num));
            }

            runcmd.push(state_update_command(step, "completed"));
        }

        Ok(runcmd)
    }
}

fn write_json_file_command<T: Serialize>(path: &str, payload: &T) -> Result<String, LaunchError> {
    let value = serde_json::to_string(payload)?;
    Ok(format!(
        "printf '%s\\n' '{}' > {path}",
        shell_single_quote(&value)
    ))
}

fn state_update_command(step: usize, status: &str) -> String {
    format!("{STATE_HELPER} update {step} {status} || true")
}

fn state_fail_if_needed_command(step: usize, cmd: usize) -> String {
    format!(
        "_ec=$?; if [ $_ec -ne 0 ]; then {STATE_HELPER} fail {step} {cmd} \"$_ec\" || true; exit $_ec; fi"
    )
}

fn shell_single_quote(value: &str) -> String {
    value.replace('\'', "'\\''")
}

pub const STATE_FILE: &str = "/var/log/cloud-init-state.json";
const STATE_HELPER: &str = "/usr/local/bin/fwdctl-state";

fn state_helper_script() -> String {
    format!(
        r#"#!/usr/bin/env bash
set -euo pipefail

STATE_FILE="{STATE_FILE}"
STATE_FILE_TMP="$(mktemp "$STATE_FILE.tmp.XXXXXX")"
trap 'rm -f "$STATE_FILE_TMP"' EXIT

cmd="${{1:-}}"
shift || true

case "$cmd" in
  update)
    step="${{1:?missing step}}"
    status="${{2:?missing status}}"
    jq --argjson step "$step" --arg status "$status" \
      '.step=$step | .name=(.stages[$step-1] // ("stage-\($step)")) | .status=$status | .last_error=null' \
      "$STATE_FILE" > "$STATE_FILE_TMP"
    ;;
  fail)
    step="${{1:?missing step}}"
    cmd_idx="${{2:?missing cmd}}"
    exit_code="${{3:?missing exit}}"
    jq --argjson step "$step" --argjson cmd "$cmd_idx" --argjson exit "$exit_code" \
      '.step=$step | .name=(.stages[$step-1] // ("stage-\($step)")) | .status="failed" | .last_error={{"stage":$step,"cmd":$cmd,"exit":$exit}}' \
      "$STATE_FILE" > "$STATE_FILE_TMP"
    ;;
  *)
    echo "unknown subcommand: $cmd" >&2
    exit 2
    ;;
esac

mv "$STATE_FILE_TMP" "$STATE_FILE"
"#
    )
}
