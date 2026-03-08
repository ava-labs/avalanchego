// Copyright (C) 2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use std::time::Duration;

use aws_config::BehaviorVersion;
use aws_sdk_ec2::Client as Ec2Client;
use aws_sdk_ec2::types::{
    ArchitectureType, BlockDeviceMapping, EbsBlockDevice, Filter, IamInstanceProfileSpecification,
    InstanceType as Ec2InstanceType, ResourceType, Tag, TagSpecification, VolumeType,
};
use aws_sdk_sts::Client as StsClient;
use log::info;
use tokio::sync::OnceCell;
use tokio::time::{sleep, timeout};

use super::{DeployOptions, LaunchError};

/// Time to wait after launch for the instance to transition to `running`.
const WAIT_RUNNING_TIMEOUT: Duration = Duration::from_secs(300);
/// Poll interval while waiting for the instance state to change.
const POLL_INTERVAL_RUNNING: Duration = Duration::from_secs(3);
/// Canonical's AWS account ID for official Ubuntu AMIs.
const UBUNTU_AMI_OWNER_ID: &str = "099720109477";
/// Template for Ubuntu 24.04 (Noble) server image names.
/// `{arch}` will be replaced with selected instance arch.
const UBUNTU_NOBLE_AMI_NAME_PATTERN: &str =
    "ubuntu/images/hvm-ssd-gp3/ubuntu-noble-24.04-{arch}-server-*";
/// Root EBS volume size (GiB) for launched benchmark instances.
const ROOT_VOLUME_SIZE_GIB: i32 = 50;
/// Tag key used to identify instances managed by `fwdctl`.
const MANAGED_BY_TAG_KEY: &str = "ManagedBy";
/// Tag value used to identify instances managed by `fwdctl`.
const MANAGED_BY_TAG_VALUE: &str = "fwdctl";
static AWS_USERNAME: OnceCell<String> = OnceCell::const_new();

#[derive(Debug, Clone)]
pub(super) struct ManagedInstance {
    pub instance_id: String,
    pub name: String,
    pub state: String,
    pub instance_type: String,
    pub public_ip: Option<String>,
    pub private_ip: Option<String>,
    pub launch_time: Option<String>,
    pub launched_by: Option<String>,
    pub custom_tag: Option<String>,
}

pub(crate) async fn aws_config(region: Option<&str>) -> aws_config::SdkConfig {
    let mut loader = aws_config::defaults(BehaviorVersion::latest());
    if let Some(r) = region {
        loader = loader.region(aws_config::Region::new(r.to_owned()));
    }
    loader.load().await
}

pub async fn ec2_client(region: &str) -> Ec2Client {
    Ec2Client::new(&aws_config(Some(region)).await)
}

async fn get_instance_architecture(
    client: &Ec2Client,
    instance_type: &str,
) -> Result<Option<ArchitectureType>, LaunchError> {
    let parsed = parse_instance_type(instance_type)?;
    let resp = client
        .describe_instance_types()
        .instance_types(parsed)
        .send()
        .await?;

    let arch = resp
        .instance_types()
        .first()
        .and_then(|i| i.processor_info())
        .map(aws_sdk_ec2::types::ProcessorInfo::supported_architectures)
        .and_then(|a| a.first().cloned());

    Ok(arch)
}

pub async fn latest_ubuntu_ami(
    ec2: &Ec2Client,
    instance_type: &str,
) -> Result<String, LaunchError> {
    let arch = match get_instance_architecture(ec2, instance_type).await? {
        Some(ArchitectureType::Arm64) => "arm64",
        Some(ArchitectureType::X8664) => "amd64",
        _ => {
            return Err(LaunchError::InvalidInstanceType(
                instance_type.to_string(),
                "unsupported architecture".to_string(),
            ));
        }
    };
    let name_pattern = UBUNTU_NOBLE_AMI_NAME_PATTERN.replace("{arch}", arch);

    let response = ec2
        .describe_images()
        .owners(UBUNTU_AMI_OWNER_ID)
        .filters(Filter::builder().name("name").values(&name_pattern).build())
        .filters(Filter::builder().name("state").values("available").build())
        .send()
        .await?;

    let mut images = response.images().to_vec();
    images.sort_by_key(|img| img.creation_date().unwrap_or_default().to_string());

    images
        .last()
        .and_then(|img| img.image_id())
        .map(String::from)
        .ok_or_else(|| LaunchError::NoMatchingAmi(arch.to_string()))
}

async fn ami_root_device_name(ec2: &Ec2Client, ami_id: &str) -> Result<String, LaunchError> {
    let response = ec2.describe_images().image_ids(ami_id).send().await?;
    let image = response
        .images()
        .first()
        .ok_or_else(|| LaunchError::AwsSdk(format!("AMI '{ami_id}' was not found")))?;
    image.root_device_name().map(str::to_owned).ok_or_else(|| {
        LaunchError::AwsSdk(format!("AMI '{ami_id}' does not expose a root device name"))
    })
}

pub async fn launch_instance(
    ec2: &Ec2Client,
    ami_id: &str,
    opts: &DeployOptions,
    user_data_b64: &str,
) -> Result<String, LaunchError> {
    let instance_type = parse_instance_type(&opts.instance_type)?;
    let instance_name = build_instance_name(opts);
    let username = get_aws_username().await;
    let tags = build_tags(opts, &instance_name, &username);
    let root_device_name = ami_root_device_name(ec2, ami_id).await?;

    let root_volume = BlockDeviceMapping::builder()
        .device_name(root_device_name)
        .ebs(
            EbsBlockDevice::builder()
                .volume_size(ROOT_VOLUME_SIZE_GIB)
                .volume_type(VolumeType::Gp3)
                .build(),
        )
        .build();

    let mut request = ec2
        .run_instances()
        .image_id(ami_id)
        .instance_type(instance_type)
        .user_data(user_data_b64)
        .min_count(1)
        .max_count(1)
        .block_device_mappings(root_volume)
        .tag_specifications(
            TagSpecification::builder()
                .resource_type(ResourceType::Instance)
                .set_tags(Some(tags))
                .build(),
        );

    if let Some(key) = &opts.key_name {
        request = request.key_name(key);
    }
    request = request.set_security_group_ids(Some(vec![opts.security_group_id.clone()]));
    if !opts.iam_instance_profile_name.is_empty() {
        request = request.iam_instance_profile(
            IamInstanceProfileSpecification::builder()
                .name(&opts.iam_instance_profile_name)
                .build(),
        );
    }

    info!("Requesting EC2 instance: {}", opts.instance_type);
    let response = request.send().await?;

    response
        .instances()
        .first()
        .and_then(|i| i.instance_id())
        .map(String::from)
        .ok_or(LaunchError::MissingInstanceId)
}

fn parse_instance_type(s: &str) -> Result<Ec2InstanceType, LaunchError> {
    use std::str::FromStr;
    Ec2InstanceType::from_str(s)
        .map_err(|_| LaunchError::InvalidInstanceType(s.to_owned(), "SDK parse error".to_owned()))
}

fn build_instance_name(opts: &DeployOptions) -> String {
    let mut name = format!("{}-{:08X}", opts.name_prefix, rand::random::<u32>());
    // add compact branch tags to the instance name for easier identification of branch overrides
    // fw: firewood, ag: avalanchego, le: libevm
    for (prefix, (_, branch)) in ["-fw-", "-ag-", "-le-"].into_iter().zip(opts.branches()) {
        if let Some(b) = branch {
            name.push_str(prefix);
            name.push_str(b);
        }
    }
    name
}

fn build_tags(opts: &DeployOptions, instance_name: &str, username: &str) -> Vec<Tag> {
    let tag = |k: &str, v: &str| Tag::builder().key(k).value(v).build();
    let mut tags = vec![
        tag("Name", instance_name),
        tag("Component", "firewood"),
        tag("ManagedBy", "fwdctl"),
        tag("LaunchedBy", username),
    ];
    if let Some(t) = &opts.custom_tag {
        tags.push(tag("CustomTag", t));
    }
    tags.extend(
        ["FirewoodBranch", "AvalancheGoBranch", "LibEVMBranch"]
            .into_iter()
            .zip(opts.branches())
            .filter_map(|(tag_name, (_, branch))| branch.map(|value| tag(tag_name, value))),
    );
    tags
}

pub async fn wait_for_running(ec2: &Ec2Client, instance_id: &str) -> Result<(), LaunchError> {
    info!("Waiting for instance to enter 'running' state...");

    timeout(WAIT_RUNNING_TIMEOUT, async {
        loop {
            let state = get_instance_state(ec2, instance_id).await?;
            match state.as_str() {
                "running" => return Ok(()),
                "terminated" | "shutting-down" | "stopped" | "stopping" => {
                    return Err(LaunchError::TerminalInstanceState {
                        instance_id: instance_id.to_owned(),
                        state,
                    });
                }
                _ => sleep(POLL_INTERVAL_RUNNING).await,
            }
        }
    })
    .await
    .map_err(|_| LaunchError::Timeout("instance running", WAIT_RUNNING_TIMEOUT.as_secs()))?
}

async fn get_instance_state(ec2: &Ec2Client, instance_id: &str) -> Result<String, LaunchError> {
    Ok(describe_instance(ec2, instance_id)
        .await?
        .as_ref()
        .and_then(|i| i.state())
        .and_then(|s| s.name())
        .map_or("unknown", aws_sdk_ec2::types::InstanceStateName::as_str)
        .to_owned())
}

pub async fn describe_ips(
    ec2: &Ec2Client,
    instance_id: &str,
) -> Result<(Option<String>, Option<String>), LaunchError> {
    Ok(describe_instance(ec2, instance_id).await?.map_or_else(
        || (None, None),
        |instance| {
            (
                instance.public_ip_address().map(Into::into),
                instance.private_ip_address().map(Into::into),
            )
        },
    ))
}

async fn describe_instance(
    ec2: &Ec2Client,
    instance_id: &str,
) -> Result<Option<aws_sdk_ec2::types::Instance>, LaunchError> {
    let response = ec2
        .describe_instances()
        .instance_ids(instance_id)
        .send()
        .await?;

    Ok(response
        .reservations()
        .first()
        .and_then(|r| r.instances().first().cloned()))
}

pub(super) async fn list_instances(
    ec2: &Ec2Client,
    running_only: bool,
) -> Result<Vec<ManagedInstance>, LaunchError> {
    let mut next_token = None::<String>;
    let mut instances = Vec::new();

    loop {
        let mut request = ec2
            .describe_instances()
            .filters(managed_filter())
            .filters(state_filter(running_only));
        if let Some(token) = next_token.as_deref() {
            request = request.next_token(token);
        }

        let response = request.send().await?;
        for reservation in response.reservations() {
            for instance in reservation.instances() {
                instances.push(map_managed_instance(instance));
            }
        }

        match response.next_token() {
            Some(token) => next_token = Some(token.to_owned()),
            None => break,
        }
    }

    instances.sort_by(|left, right| right.launch_time.cmp(&left.launch_time));
    Ok(instances)
}

pub(super) async fn terminate_instances(
    ec2: &Ec2Client,
    instance_ids: &[String],
) -> Result<(), LaunchError> {
    if instance_ids.is_empty() {
        return Ok(());
    }
    ec2.terminate_instances()
        .set_instance_ids(Some(instance_ids.to_vec()))
        .send()
        .await?;
    Ok(())
}

fn managed_filter() -> Filter {
    Filter::builder()
        .name(format!("tag:{MANAGED_BY_TAG_KEY}"))
        .values(MANAGED_BY_TAG_VALUE)
        .build()
}

fn state_filter(running_only: bool) -> Filter {
    let mut builder = Filter::builder()
        .name("instance-state-name")
        .values("pending")
        .values("running");

    if !running_only {
        builder = builder
            .values("stopping")
            .values("stopped")
            .values("shutting-down");
    }

    builder.build()
}

fn map_managed_instance(instance: &aws_sdk_ec2::types::Instance) -> ManagedInstance {
    ManagedInstance {
        instance_id: instance.instance_id().unwrap_or_default().to_owned(),
        name: instance_tag(instance, "Name").unwrap_or_default(),
        state: instance
            .state()
            .and_then(|state| state.name())
            .map_or_else(|| "unknown".to_owned(), |state| state.as_str().to_owned()),
        instance_type: instance.instance_type().map_or_else(
            || "unknown".to_owned(),
            |instance_type| instance_type.as_str().to_owned(),
        ),
        public_ip: instance.public_ip_address().map(ToOwned::to_owned),
        private_ip: instance.private_ip_address().map(ToOwned::to_owned),
        launch_time: instance.launch_time().map(ToString::to_string),
        launched_by: instance_tag(instance, "LaunchedBy"),
        custom_tag: instance_tag(instance, "CustomTag"),
    }
}

fn instance_tag(instance: &aws_sdk_ec2::types::Instance, key: &str) -> Option<String> {
    instance
        .tags()
        .iter()
        .find(|tag| tag.key() == Some(key))
        .and_then(|tag| tag.value())
        .map(ToOwned::to_owned)
}

/// Returns the AWS username for the current caller identity.
pub(super) async fn get_aws_username() -> String {
    AWS_USERNAME
        .get_or_init(|| async {
            let fallback_username = || {
                std::env::var("USER")
                    .or_else(|_| std::env::var("USERNAME"))
                    .unwrap_or_else(|_| "unknown".into())
            };

            // STS GetCallerIdentity is region-independent; use us-west-2.
            let sts = StsClient::new(&aws_config(Some("us-west-2")).await);
            match sts.get_caller_identity().send().await {
                Ok(id) => id
                    .arn()
                    .and_then(|arn| {
                        arn.rsplit('/')
                            .next()
                            .or_else(|| arn.rsplit(':').next())
                            .map(ToOwned::to_owned)
                    })
                    .unwrap_or_else(fallback_username),
                Err(e) => {
                    log::debug!("STS GetCallerIdentity failed: {e}");
                    fallback_username()
                }
            }
        })
        .await
        .clone()
}
