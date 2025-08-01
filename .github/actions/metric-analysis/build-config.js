const fs = require('fs');
const path = require('path');

async function buildConfig({ inputs, github, context, core, actionPath }) {
  const BASELINE_COUNT = parseInt(inputs.baseline_count);
  const WORKFLOW = inputs.workflow;
  const CURRENT_RUN_ID = context.runId.toString();

  console.log(`#9`, {
    inputs,
    github,
    context,
    core,
    actionPath,
    BASELINE_COUNT,
    WORKFLOW,
    CURRENT_RUN_ID
  })

  const currentMetadata = getCurrentRunMetadata();
  const baselines = await downloadBaselineMetadata({
    github,
    context,
    jobName: currentMetadata.job_id,
    workflow: WORKFLOW,
    baselineCount: BASELINE_COUNT,
    currentRunId: CURRENT_RUN_ID
  });

  console.log(`#20`, {
    currentMetadata,
    baselines
  })

  const config = {
    query: inputs.query,
    metric_name: inputs.metric_name,
    x_axis_label: inputs.x_axis_label,
    y_axis_label: inputs.y_axis_label || inputs.metric_name,
    candidate: {
      start_time: new Date(currentMetadata.start_timestamp).getTime(),
      end_time: new Date(currentMetadata.end_timestamp).getTime(),
      name: `Candidate Run (#${CURRENT_RUN_ID})`,
      labels: {
        gh_run_id: CURRENT_RUN_ID,
        gh_job_id: currentMetadata.job_id,
        gh_run_attempt: currentMetadata.run_attempt,
        gh_repo: currentMetadata.repository,
        is_ephemeral_node: "false"
      }
    },
    baselines: baselines,
    output_file: path.join(actionPath, `metric_visualization_${CURRENT_RUN_ID}.html`)
  };

  const configPath = path.join(actionPath, 'metric_config.json');
  fs.writeFileSync(configPath, JSON.stringify(config, null, 2));

  return {
    baselines_found: baselines.length,
    config_path: configPath
  };
}

function getCurrentRunMetadata() {
  const metadataFile = '/tmp/run-metadata/run_metadata.json';

  if (!fs.existsSync(metadataFile)) {
    throw new Error('Current run metadata not found');
  }

  return JSON.parse(fs.readFileSync(metadataFile, 'utf8'));
}

async function downloadBaselineMetadata({ github, context, jobName, workflow, baselineCount, currentRunId }) {
  const { data: workflowRuns } = await github.rest.actions.listWorkflowRuns({
    owner: context.repo.owner,
    repo: context.repo.repo,
    workflow_id: workflow,
    status: 'completed',
    conclusion: 'success',
    per_page: 50
  });

  console.log(`downloadBaselineMetadata#85`, workflowRuns)

  const baselines = [];

  for (const run of workflowRuns.workflow_runs) {
    if (baselines.length >= baselineCount) break;
    if (run.id.toString() === currentRunId) continue;

    // Check for metadata artifact
    const { data: artifacts } = await github.rest.actions.listWorkflowRunArtifacts({
      owner: context.repo.owner,
      repo: context.repo.repo,
      run_id: run.id
    });

    const metadataArtifact = artifacts.artifacts.find(artifact =>
      artifact.name === `run-metadata-${jobName}-${run.id}` && !artifact.expired
    );

    if (metadataArtifact) {
      baselines.push({
        name: `Baseline ${run.run_number} (#${run.id})`,
        start_time: new Date(run.created_at).getTime(),
        end_time: new Date(run.updated_at).getTime(),
        labels: {
          gh_run_id: run.id.toString(),
          gh_job_id: jobName,
          gh_run_attempt: "1",
          gh_repo: context.repo.owner + "/" + context.repo.repo,
          is_ephemeral_node: "false"
        }
      });
    }
  }

  return baselines;
}

module.exports = buildConfig;
