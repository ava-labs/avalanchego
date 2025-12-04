const baseURL = window.location.origin;
let currentTheme = localStorage.getItem('avago-theme') || 'dark';

function setTheme(theme) {
    currentTheme = theme;
    document.body.setAttribute('data-theme', theme);
    localStorage.setItem('avago-theme', theme);
    document.getElementById('lightBtn').classList.toggle('active', theme === 'light');
    document.getElementById('darkBtn').classList.toggle('active', theme === 'dark');
    // Redraw charts with new theme colors
    if (metricsHistory.polls.length > 0) {
        drawAllCharts();
    }
}
setTheme(currentTheme);

// Metrics history for charts
const metricsHistory = {
    polls: [],
    latency: [],
    blocks: [],
    processing: [],
    timestamps: [],
    maxDataPoints: 60
};

let metricsInterval = null;

const tabLoaded = {};
function showTab(tabId, event) {
    document.querySelectorAll('.tab').forEach(t => t.classList.remove('active'));
    document.querySelectorAll('.tab-content').forEach(c => c.classList.remove('active'));
    event.target.classList.add('active');
    document.getElementById('tab-' + tabId).classList.add('active');
    
    // Load tab data on first view
    if (!tabLoaded[tabId]) {
        tabLoaded[tabId] = true;
        if (tabId === 'network') {
            fetchPeersDetailed();
            fetchNetworkStats();
            fetchACPs();
        } else if (tabId === 'chains') {
            fetchVMs();
            fetchSubnets();
        } else if (tabId === 'health') {
            fetchHealthDetailed();
            fetchLiveness();
            fetchReadiness();
        } else if (tabId === 'advanced') {
            fetchUpgrades();
            fetchTxFees();
            fetchPChainHeight();
            fetchCChainInfo();
            fetchValidators();
        } else if (tabId === 'metrics') {
            startMetricsPolling();
            fetchMetricsOnce();
        } else if (tabId === 'l1validators') {
            fetchL1ValidatorsOverview();
        }
    }
    
    // Handle metrics polling based on tab visibility
    if (tabId === 'metrics') {
        startMetricsPolling();
    } else {
        stopMetricsPolling();
    }
}

async function fetchJSON(endpoint, method = 'POST', body = null) {
    try {
        const opts = { method, headers: { 'Content-Type': 'application/json' } };
        if (body) opts.body = JSON.stringify(body);
        const res = await fetch(baseURL + endpoint, opts);
        return await res.json();
    } catch (e) { console.error(e); return null; }
}

async function rpcCall(endpoint, method, params = {}) {
    return fetchJSON(endpoint, 'POST', { jsonrpc: '2.0', id: 1, method, params });
}

async function refreshSection(btn, fn) {
    btn.classList.add('spinning');
    await fn();
    btn.classList.remove('spinning');
}

function nanoToAvax(n) { return (parseInt(n) / 1e9).toFixed(4); }

// HTML escape function to prevent XSS
function esc(str) {
    if (str === null || str === undefined) return '';
    const div = document.createElement('div');
    div.textContent = String(str);
    return div.innerHTML;
}

// ============ METRICS & CHARTS ============

// Parse Prometheus metrics text format
function parsePrometheusMetrics(text) {
    const metrics = {};
    const lines = text.split('\n');
    for (const line of lines) {
        if (line.startsWith('#') || line.trim() === '') continue;
        const match = line.match(/^([a-zA-Z_:][a-zA-Z0-9_:]*)\{?([^}]*)\}?\s+(.+)$/);
        if (match) {
            const [, name, labels, value] = match;
            if (!metrics[name]) metrics[name] = [];
            metrics[name].push({ labels: labels || '', value: parseFloat(value) });
        } else {
            const simpleMatch = line.match(/^([a-zA-Z_:][a-zA-Z0-9_:]*)\s+(.+)$/);
            if (simpleMatch) {
                const [, name, value] = simpleMatch;
                if (!metrics[name]) metrics[name] = [];
                metrics[name].push({ labels: '', value: parseFloat(value) });
            }
        }
    }
    return metrics;
}

// Get metric value by name (exact match)
function getMetricValue(metrics, name, defaultVal = 0) {
    if (metrics[name] && metrics[name].length > 0) {
        return metrics[name][0].value;
    }
    return defaultVal;
}

// Find metrics matching a suffix pattern and return the sum
// This handles prefixed metrics like avalanche_C_polls_successful, avalanche_P_polls_successful
function getMetricBySuffix(metrics, suffix, defaultVal = 0) {
    let total = 0;
    let found = false;
    for (const key of Object.keys(metrics)) {
        if (key === suffix || key.endsWith('_' + suffix)) {
            for (const m of metrics[key]) {
                total += m.value;
                found = true;
            }
        }
    }
    return found ? total : defaultVal;
}

// Get the max value of metrics matching a suffix (useful for heights)
function getMetricMaxBySuffix(metrics, suffix, defaultVal = 0) {
    let maxVal = defaultVal;
    for (const key of Object.keys(metrics)) {
        if (key === suffix || key.endsWith('_' + suffix)) {
            for (const m of metrics[key]) {
                maxVal = Math.max(maxVal, m.value);
            }
        }
    }
    return maxVal;
}

// Get metric by name and label
function getMetricByLabel(metrics, name, labelMatch) {
    if (!metrics[name]) return null;
    for (const m of metrics[name]) {
        if (m.labels.includes(labelMatch)) {
            return m.value;
        }
    }
    return null;
}

// Simple canvas chart drawing
function drawLineChart(canvasId, data, options = {}) {
    const canvas = document.getElementById(canvasId);
    if (!canvas) return;
    
    const ctx = canvas.getContext('2d');
    const dpr = window.devicePixelRatio || 1;
    const rect = canvas.getBoundingClientRect();
    
    canvas.width = rect.width * dpr;
    canvas.height = rect.height * dpr;
    ctx.scale(dpr, dpr);
    
    const width = rect.width;
    const height = rect.height;
    const padding = { top: 20, right: 20, bottom: 30, left: 50 };
    const chartWidth = width - padding.left - padding.right;
    const chartHeight = height - padding.top - padding.bottom;
    
    // Theme colors
    const isDark = currentTheme === 'dark';
    const textColor = isDark ? '#a1a1aa' : '#52525b';
    const gridColor = isDark ? '#27272a' : '#e4e4e7';
    const lineColor = options.color || '#e84142';
    const fillColor = options.fillColor || (isDark ? 'rgba(232, 65, 66, 0.15)' : 'rgba(232, 65, 66, 0.1)');
    
    ctx.clearRect(0, 0, width, height);
    
    if (data.length < 2) {
        ctx.fillStyle = textColor;
        ctx.font = '12px -apple-system, BlinkMacSystemFont, sans-serif';
        ctx.textAlign = 'center';
        ctx.fillText('Collecting data...', width / 2, height / 2);
        return;
    }
    
    // Calculate min/max
    let minVal = Math.min(...data);
    let maxVal = Math.max(...data);
    if (minVal === maxVal) {
        minVal = minVal - 1;
        maxVal = maxVal + 1;
    }
    const range = maxVal - minVal;
    minVal = minVal - range * 0.1;
    maxVal = maxVal + range * 0.1;
    
    // Draw grid
    ctx.strokeStyle = gridColor;
    ctx.lineWidth = 1;
    for (let i = 0; i <= 4; i++) {
        const y = padding.top + (chartHeight / 4) * i;
        ctx.beginPath();
        ctx.moveTo(padding.left, y);
        ctx.lineTo(width - padding.right, y);
        ctx.stroke();
    }
    
    // Draw Y-axis labels
    ctx.fillStyle = textColor;
    ctx.font = '10px ui-monospace, monospace';
    ctx.textAlign = 'right';
    for (let i = 0; i <= 4; i++) {
        const y = padding.top + (chartHeight / 4) * i;
        const val = maxVal - ((maxVal - minVal) / 4) * i;
        ctx.fillText(formatNumber(val), padding.left - 8, y + 3);
    }
    
    // Draw line
    ctx.beginPath();
    ctx.strokeStyle = lineColor;
    ctx.lineWidth = 2;
    ctx.lineJoin = 'round';
    ctx.lineCap = 'round';
    
    const xStep = chartWidth / (data.length - 1);
    
    for (let i = 0; i < data.length; i++) {
        const x = padding.left + i * xStep;
        const y = padding.top + chartHeight - ((data[i] - minVal) / (maxVal - minVal)) * chartHeight;
        if (i === 0) ctx.moveTo(x, y);
        else ctx.lineTo(x, y);
    }
    ctx.stroke();
    
    // Fill area
    ctx.lineTo(padding.left + (data.length - 1) * xStep, padding.top + chartHeight);
    ctx.lineTo(padding.left, padding.top + chartHeight);
    ctx.closePath();
    ctx.fillStyle = fillColor;
    ctx.fill();
    
    // Draw latest value
    if (options.showLatest !== false && data.length > 0) {
        const lastVal = data[data.length - 1];
        ctx.fillStyle = lineColor;
        ctx.font = 'bold 14px -apple-system, BlinkMacSystemFont, sans-serif';
        ctx.textAlign = 'right';
        ctx.fillText(formatNumber(lastVal) + (options.unit || ''), width - padding.right, padding.top - 5);
    }
    
    // Draw title
    if (options.title) {
        ctx.fillStyle = textColor;
        ctx.font = '11px -apple-system, BlinkMacSystemFont, sans-serif';
        ctx.textAlign = 'left';
        ctx.fillText(options.title, padding.left, padding.top - 5);
    }
}

function formatNumber(n) {
    if (Math.abs(n) >= 1e9) return (n / 1e9).toFixed(2) + 'B';
    if (Math.abs(n) >= 1e6) return (n / 1e6).toFixed(2) + 'M';
    if (Math.abs(n) >= 1e3) return (n / 1e3).toFixed(2) + 'K';
    if (Number.isInteger(n)) return n.toString();
    return n.toFixed(2);
}

function drawBarChart(canvasId, data, labels, options = {}) {
    const canvas = document.getElementById(canvasId);
    if (!canvas) return;
    
    const ctx = canvas.getContext('2d');
    const dpr = window.devicePixelRatio || 1;
    const rect = canvas.getBoundingClientRect();
    
    canvas.width = rect.width * dpr;
    canvas.height = rect.height * dpr;
    ctx.scale(dpr, dpr);
    
    const width = rect.width;
    const height = rect.height;
    const padding = { top: 20, right: 20, bottom: 40, left: 50 };
    const chartWidth = width - padding.left - padding.right;
    const chartHeight = height - padding.top - padding.bottom;
    
    const isDark = currentTheme === 'dark';
    const textColor = isDark ? '#a1a1aa' : '#52525b';
    const colors = options.colors || ['#e84142', '#22c55e', '#eab308', '#3b82f6'];
    
    ctx.clearRect(0, 0, width, height);
    
    if (data.length === 0) {
        ctx.fillStyle = textColor;
        ctx.font = '12px -apple-system, BlinkMacSystemFont, sans-serif';
        ctx.textAlign = 'center';
        ctx.fillText('No data', width / 2, height / 2);
        return;
    }
    
    const maxVal = Math.max(...data, 1);
    const barWidth = (chartWidth / data.length) * 0.7;
    const barGap = (chartWidth / data.length) * 0.15;
    
    for (let i = 0; i < data.length; i++) {
        const barHeight = (data[i] / maxVal) * chartHeight;
        const x = padding.left + i * (chartWidth / data.length) + barGap;
        const y = padding.top + chartHeight - barHeight;
        
        ctx.fillStyle = colors[i % colors.length];
        ctx.beginPath();
        ctx.roundRect(x, y, barWidth, barHeight, 4);
        ctx.fill();
        
        // Label
        ctx.fillStyle = textColor;
        ctx.font = '10px -apple-system, BlinkMacSystemFont, sans-serif';
        ctx.textAlign = 'center';
        ctx.fillText(labels[i] || '', x + barWidth / 2, height - 10);
        
        // Value
        ctx.fillText(formatNumber(data[i]), x + barWidth / 2, y - 5);
    }
}

async function fetchMetricsOnce() {
    try {
        const res = await fetch(baseURL + '/ext/metrics');
        const text = await res.text();
        const metrics = parsePrometheusMetrics(text);
        
        // Debug: Log available metric keys on first fetch
        if (metricsHistory.timestamps.length === 0) {
            const relevantKeys = Object.keys(metrics).filter(k => 
                k.includes('poll') || k.includes('blk') || k.includes('height') || k.includes('accepted')
            );
            console.log('Available metrics:', relevantKeys.slice(0, 30));
        }
        
        // Update history
        const now = Date.now();
        metricsHistory.timestamps.push(now);
        
        // Polls (successful vs failed) - sum across all chains
        // Metric names: avalanche_*_polls_successful, avalanche_*_polls_failed
        const successfulPolls = getMetricBySuffix(metrics, 'polls_successful', 0);
        const failedPolls = getMetricBySuffix(metrics, 'polls_failed', 0);
        metricsHistory.polls.push({ successful: successfulPolls, failed: failedPolls });
        
        // Block latency (accepted) - sum across all chains then compute avg
        // Metric names: avalanche_*_blks_accepted_sum, avalanche_*_blks_accepted_count
        const latAcceptedSum = getMetricBySuffix(metrics, 'blks_accepted_sum', 0);
        const latAcceptedCount = getMetricBySuffix(metrics, 'blks_accepted_count', 0);
        const avgLatency = latAcceptedCount > 0 ? latAcceptedSum / latAcceptedCount / 1e6 : 0; // Convert to ms
        metricsHistory.latency.push(avgLatency);
        
        // Last accepted height - get max across all chains
        // Metric name: avalanche_*_last_accepted_height
        const height = getMetricMaxBySuffix(metrics, 'last_accepted_height', 0);
        metricsHistory.blocks.push(height);
        
        // Processing blocks - sum across all chains
        // Metric name: avalanche_*_blks_processing
        const processing = getMetricBySuffix(metrics, 'blks_processing', 0);
        metricsHistory.processing.push(processing);
        
        // Trim history
        while (metricsHistory.timestamps.length > metricsHistory.maxDataPoints) {
            metricsHistory.timestamps.shift();
            metricsHistory.polls.shift();
            metricsHistory.latency.shift();
            metricsHistory.blocks.shift();
            metricsHistory.processing.shift();
        }
        
        updateMetricsDisplay(metrics);
        drawAllCharts();
    } catch (e) {
        console.error('Failed to fetch metrics:', e);
        // Show error in UI
        const pollsEl = document.getElementById('metricPolls');
        if (pollsEl) pollsEl.innerHTML = '<span style="color: var(--color-error);">Error fetching</span>';
    }
}

function updateMetricsDisplay(metrics) {
    // Update stat cards
    const pollsEl = document.getElementById('metricPolls');
    const latencyEl = document.getElementById('metricLatency');
    const heightEl = document.getElementById('metricHeight');
    const processingEl = document.getElementById('metricProcessing');
    
    if (pollsEl) {
        const successful = getMetricBySuffix(metrics, 'polls_successful', 0);
        const failed = getMetricBySuffix(metrics, 'polls_failed', 0);
        pollsEl.innerHTML = `
            <span style="color: var(--color-success);">${formatNumber(successful)}</span> / 
            <span style="color: var(--color-error);">${formatNumber(failed)}</span>
        `;
    }
    
    if (latencyEl && metricsHistory.latency.length > 0) {
        const lat = metricsHistory.latency[metricsHistory.latency.length - 1];
        latencyEl.textContent = lat.toFixed(2) + ' ms';
    }
    
    if (heightEl && metricsHistory.blocks.length > 0) {
        const height = metricsHistory.blocks[metricsHistory.blocks.length - 1];
        heightEl.textContent = formatNumber(height);
    }
    
    if (processingEl && metricsHistory.processing.length > 0) {
        const proc = metricsHistory.processing[metricsHistory.processing.length - 1];
        processingEl.textContent = formatNumber(proc);
    }
}

function drawAllCharts() {
    // Draw block height chart
    drawLineChart('blockHeightChart', metricsHistory.blocks, {
        title: 'Block Height',
        color: '#3b82f6',
        fillColor: currentTheme === 'dark' ? 'rgba(59, 130, 246, 0.15)' : 'rgba(59, 130, 246, 0.1)'
    });
    
    // Draw latency chart
    drawLineChart('latencyChart', metricsHistory.latency, {
        title: 'Avg Accept Latency',
        color: '#22c55e',
        fillColor: currentTheme === 'dark' ? 'rgba(34, 197, 94, 0.15)' : 'rgba(34, 197, 94, 0.1)',
        unit: ' ms'
    });
    
    // Draw processing blocks chart
    drawLineChart('processingChart', metricsHistory.processing, {
        title: 'Processing Blocks',
        color: '#eab308',
        fillColor: currentTheme === 'dark' ? 'rgba(234, 179, 8, 0.15)' : 'rgba(234, 179, 8, 0.1)'
    });
    
    // Draw polls chart (show rate of change)
    if (metricsHistory.polls.length > 1) {
        const pollRates = [];
        for (let i = 1; i < metricsHistory.polls.length; i++) {
            const deltaSucc = metricsHistory.polls[i].successful - metricsHistory.polls[i-1].successful;
            pollRates.push(Math.max(0, deltaSucc));
        }
        drawLineChart('pollsChart', pollRates, {
            title: 'Polls/sec (successful)',
            color: '#e84142',
            fillColor: currentTheme === 'dark' ? 'rgba(232, 65, 66, 0.15)' : 'rgba(232, 65, 66, 0.1)',
            unit: '/s'
        });
    }
}

function startMetricsPolling() {
    if (metricsInterval) return;
    metricsInterval = setInterval(fetchMetricsOnce, 2000);
}

function stopMetricsPolling() {
    if (metricsInterval) {
        clearInterval(metricsInterval);
        metricsInterval = null;
    }
}

// ============ L1 VALIDATORS ============

async function fetchL1ValidatorsOverview() {
    const el = document.getElementById('l1ValidatorsCard');
    el.innerHTML = '<div class="loading" style="height: 200px;"></div>';
    
    // Fetch fee state and config in parallel
    const [configData, feeStateData] = await Promise.all([
        rpcCall('/ext/admin', 'admin.getConfig'),
        rpcCall('/ext/P', 'platform.getFeeState')
    ]);
    
    // Get current fee price (in nAVAX per unit)
    let feePrice = 1; // default to 1 nAVAX
    if (feeStateData?.result?.price) {
        feePrice = parseInt(feeStateData.result.price);
    }
    
    let trackedSubnetIds = [];
    
    if (configData?.result?.config) {
        const config = configData.result.config;
        const trackSubnets = config['track-subnets'] || config.trackSubnets || '';
        if (trackSubnets) {
            trackedSubnetIds = trackSubnets.split(',').map(id => id.trim()).filter(id => id);
        }
    }
    
    if (trackedSubnetIds.length === 0) {
        el.innerHTML = `
            <div class="empty-state">
                <div style="margin-bottom: 0.5rem;">No tracked L1s/Subnets configured</div>
                <div style="font-size: 0.7rem; color: var(--text-muted);">
                    Configure <code>--track-subnets</code> to track L1 validators
                </div>
            </div>
        `;
        return;
    }
    
    let html = '';
    
    // Show fee state info
    html += `
        <div class="fee-state-banner">
            <div class="fee-state-item">
                <span class="fee-state-label">Current Fee Price</span>
                <span class="fee-state-value">${feePrice} nAVAX/unit/sec</span>
            </div>
            <div class="fee-state-item">
                <span class="fee-state-label">Fee Capacity</span>
                <span class="fee-state-value">${formatNumber(parseInt(feeStateData?.result?.capacity || 0))}</span>
            </div>
            <div class="fee-state-item">
                <span class="fee-state-label">Excess</span>
                <span class="fee-state-value">${formatNumber(parseInt(feeStateData?.result?.excess || 0))}</span>
            </div>
        </div>
    `;
    
    for (const subnetID of trackedSubnetIds) {
        // Get validators for this subnet
        const validatorsData = await rpcCall('/ext/P', 'platform.getCurrentValidators', { subnetID });
        
        if (!validatorsData?.result?.validators) continue;
        
        const validators = validatorsData.result.validators;
        const l1Validators = validators.filter(v => v.balance !== undefined);
        
        if (l1Validators.length === 0) continue;
        
        // Calculate totals
        let totalBalance = 0;
        let totalWeight = 0;
        
        l1Validators.forEach(v => {
            totalBalance += parseInt(v.balance || 0);
            totalWeight += parseInt(v.weight || 0);
        });
        
        // Calculate subnet total daily cost
        const totalDailyCost = totalWeight * feePrice * 86400; // nAVAX per day
        
        html += `
            <div class="l1-subnet-section">
                <div class="l1-subnet-header">
                    <div class="l1-subnet-title">
                        <span class="chain-badge">L1</span>
                        <span class="subnet-id-short" title="${esc(subnetID)}">${esc(subnetID.substring(0, 12))}...</span>
                    </div>
                    <div class="l1-subnet-stats">
                        <span class="l1-stat"><strong>${l1Validators.length}</strong> validators</span>
                        <span class="l1-stat"><strong>${nanoToAvax(totalBalance)}</strong> AVAX balance</span>
                        <span class="l1-stat"><strong>${nanoToAvax(totalDailyCost)}</strong> AVAX/day (est.)</span>
                    </div>
                </div>
                <div class="l1-validators-table">
                    <table class="data-table">
                        <thead>
                            <tr>
                                <th>Node ID</th>
                                <th style="text-align: right;">Weight</th>
                                <th style="text-align: right;">Balance</th>
                                <th style="text-align: right;">Daily Cost</th>
                                <th style="text-align: right;">Days Left</th>
                                <th>Status</th>
                            </tr>
                        </thead>
                        <tbody>
                            ${l1Validators.slice(0, 50).map(v => {
                                const balance = parseInt(v.balance || 0);
                                const weight = parseInt(v.weight || 0);
                                
                                // Calculate daily fee based on actual fee price
                                // Fee = weight * price * seconds_per_day
                                const dailyFee = weight * feePrice * 86400; // in nAVAX
                                const daysLeft = dailyFee > 0 ? balance / dailyFee : Infinity;
                                
                                let statusClass = 'synced';
                                let statusText = 'Active';
                                if (balance === 0) {
                                    statusClass = 'syncing';
                                    statusText = 'Inactive';
                                } else if (daysLeft < 7) {
                                    statusClass = 'warning';
                                    statusText = 'Low Balance';
                                }
                                
                                return `
                                    <tr>
                                        <td title="${esc(v.nodeID)}">${esc(v.nodeID?.substring(0, 20))}...</td>
                                        <td style="text-align: right; font-variant-numeric: tabular-nums;">${formatNumber(weight)}</td>
                                        <td style="text-align: right; font-variant-numeric: tabular-nums;">
                                            <span class="balance-value">${nanoToAvax(balance)}</span> AVAX
                                        </td>
                                        <td style="text-align: right; font-variant-numeric: tabular-nums; color: var(--text-muted);">
                                            ${nanoToAvax(dailyFee)} AVAX
                                        </td>
                                        <td style="text-align: right; font-variant-numeric: tabular-nums;">
                                            <span class="${daysLeft < 7 ? 'balance-value' : ''}">${daysLeft === Infinity ? '∞' : daysLeft < 1 ? '<1' : Math.floor(daysLeft)}</span>
                                        </td>
                                        <td><span class="chain-status ${statusClass}">${statusText}</span></td>
                                    </tr>
                                `;
                            }).join('')}
                        </tbody>
                    </table>
                    ${l1Validators.length > 50 ? `<div class="empty-state" style="padding: 0.5rem;">Showing 50 of ${l1Validators.length} validators</div>` : ''}
                </div>
            </div>
        `;
    }
    
    if (html === '' || !html.includes('l1-subnet-section')) {
        html += `
            <div class="empty-state">
                <div style="margin-bottom: 0.5rem;">No L1 validators found</div>
                <div style="font-size: 0.7rem; color: var(--text-muted);">
                    The tracked subnets may not be L1s or may not have active validators
                </div>
            </div>
        `;
    }
    
    el.innerHTML = html;
}

async function fetchL1ValidatorDetails(validationID) {
    const data = await rpcCall('/ext/P', 'platform.getL1Validator', { validationID });
    return data?.result || null;
}

// API Explorer Functions
async function callApi(endpoint, method, params = {}) {
    const reqEl = document.getElementById('apiRequest');
    const resEl = document.getElementById('apiResponse');
    const statusEl = document.getElementById('apiStatus');
    
    const requestBody = { jsonrpc: '2.0', id: 1, method: method, params: params };
    reqEl.textContent = 'POST ' + endpoint + '\n\n' + JSON.stringify(requestBody, null, 2);
    resEl.textContent = 'Loading...';
    statusEl.textContent = '';
    statusEl.className = '';
    
    try {
        const start = performance.now();
        const res = await fetch(baseURL + endpoint, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(requestBody)
        });
        const elapsed = (performance.now() - start).toFixed(0);
        const text = await res.text();
        let output;
        try {
            output = JSON.stringify(JSON.parse(text), null, 2);
        } catch {
            output = text;
        }
        resEl.textContent = output;
        statusEl.textContent = res.status + (res.ok ? ' OK' : ' Error') + ' — ' + elapsed + 'ms';
        statusEl.className = res.ok ? 'success' : 'error';
    } catch (e) {
        resEl.textContent = 'Error: ' + e.message;
        statusEl.textContent = 'Failed';
        statusEl.className = 'error';
    }
}

function callApiWithParams(endpoint, method, params) {
    callApi(endpoint, method, params);
}

async function callGetApi(endpoint) {
    const reqEl = document.getElementById('apiRequest');
    const resEl = document.getElementById('apiResponse');
    const statusEl = document.getElementById('apiStatus');
    
    reqEl.textContent = 'GET ' + endpoint;
    resEl.textContent = 'Loading...';
    statusEl.textContent = '';
    statusEl.className = '';
    
    try {
        const start = performance.now();
        const res = await fetch(baseURL + endpoint);
        const elapsed = (performance.now() - start).toFixed(0);
        const contentType = res.headers.get('content-type') || '';
        let data;
        if (contentType.includes('application/json')) {
            data = JSON.stringify(await res.json(), null, 2);
        } else {
            const text = await res.text();
            data = text.length > 5000 ? text.substring(0, 5000) + '\n\n... (truncated)' : text;
        }
        resEl.textContent = data;
        statusEl.textContent = res.status + ' OK — ' + elapsed + 'ms';
        statusEl.className = res.ok ? 'success' : 'error';
    } catch (e) {
        resEl.textContent = 'Error: ' + e.message;
        statusEl.textContent = 'Failed';
        statusEl.className = 'error';
    }
}

async function callEthApi(method, params) {
    const endpoint = '/ext/bc/C/rpc';
    const reqEl = document.getElementById('apiRequest');
    const resEl = document.getElementById('apiResponse');
    const statusEl = document.getElementById('apiStatus');
    
    const requestBody = { jsonrpc: '2.0', id: 1, method: method, params: params };
    reqEl.textContent = 'POST ' + endpoint + '\n\n' + JSON.stringify(requestBody, null, 2);
    resEl.textContent = 'Loading...';
    statusEl.textContent = '';
    statusEl.className = '';
    
    try {
        const start = performance.now();
        const res = await fetch(baseURL + endpoint, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(requestBody)
        });
        const elapsed = (performance.now() - start).toFixed(0);
        const text = await res.text();
        let output;
        try {
            output = JSON.stringify(JSON.parse(text), null, 2);
        } catch {
            output = text;
        }
        resEl.textContent = output;
        statusEl.textContent = res.status + (res.ok ? ' OK' : ' Error') + ' — ' + elapsed + 'ms';
        statusEl.className = res.ok ? 'success' : 'error';
    } catch (e) {
        resEl.textContent = 'Error: ' + e.message;
        statusEl.textContent = 'Failed';
        statusEl.className = 'error';
    }
}

async function sendCustomRequest() {
    const endpoint = document.getElementById('customEndpoint').value;
    const method = document.getElementById('customMethod').value;
    let params = {};
    try {
        params = JSON.parse(document.getElementById('customParams').value || '{}');
    } catch (e) {
        alert('Invalid JSON in params');
        return;
    }
    await callApi(endpoint, method, params);
}

function clearApiResponse() {
    document.getElementById('apiRequest').textContent = 'Click a method on the left to execute a request';
    document.getElementById('apiResponse').textContent = 'Response will appear here';
    document.getElementById('apiStatus').textContent = '';
    document.getElementById('apiStatus').className = '';
}

// Node Info
async function fetchNodeInfo() {
    const [ver, id, ip] = await Promise.all([
        rpcCall('/ext/info', 'info.getNodeVersion'),
        rpcCall('/ext/info', 'info.getNodeID'),
        rpcCall('/ext/info', 'info.getNodeIP')
    ]);
    const el = document.getElementById('nodeInfoCard');
    const nodeId = id?.result?.nodeID || '';
    
    // Get BLS public key and proof of possession
    const blsPublicKey = id?.result?.nodePOP?.publicKey || '';
    const blsProofOfPossession = id?.result?.nodePOP?.proofOfPossession || '';
    
    if (ver?.result) {
        const v = ver.result;
        el.innerHTML = `
            <div class="info-list">
                <div class="info-row"><span class="info-label">Version</span><span class="info-value">${esc(v.version)}</span></div>
                <div class="info-row"><span class="info-label">Database</span><span class="info-value">${esc(v.databaseVersion)}</span></div>
                <div class="info-row"><span class="info-label">RPC Protocol</span><span class="info-value">${esc(v.rpcProtocolVersion)}</span></div>
                <div class="info-row"><span class="info-label">Git Commit</span><span class="info-value">${esc(v.gitCommit)}</span></div>
                <div class="info-row"><span class="info-label">Public IP</span><span class="info-value">${esc(ip?.result?.ip || 'N/A')}</span></div>
            </div>
            <div class="mono-box" style="margin-top:0.75rem;">
                <span class="label">Node ID</span>
                <span title="${esc(nodeId)}">${esc(nodeId || 'Unknown')}</span>
            </div>
            ${blsPublicKey ? `
                <div class="mono-box" style="margin-top:0.5rem;">
                    <span class="label">BLS Public Key</span>
                    <span title="${esc(blsPublicKey)}">${esc(blsPublicKey)}</span>
                </div>
            ` : ''}
            ${blsProofOfPossession ? `
                <div class="mono-box" style="margin-top:0.5rem;">
                    <span class="label">BLS Proof of Possession</span>
                    <span title="${esc(blsProofOfPossession)}">${esc(blsProofOfPossession)}</span>
                </div>
            ` : ''}
        `;
    } else {
        el.innerHTML = '<div class="error-state">Failed to load node info</div>';
    }
}

// Network Info
async function fetchNetworkInfo() {
    const [net, name] = await Promise.all([
        rpcCall('/ext/info', 'info.getNetworkID'),
        rpcCall('/ext/info', 'info.getNetworkName')
    ]);
    const el = document.getElementById('networkInfoCard');
    el.innerHTML = `
        <div class="info-list">
            <div class="info-row"><span class="info-label">Network</span><span class="info-value">${esc(name?.result?.networkName || 'Unknown')}</span></div>
            <div class="info-row"><span class="info-label">Network ID</span><span class="info-value">${esc(net?.result?.networkID || 'Unknown')}</span></div>
            <div class="info-row"><span class="info-label">API Endpoint</span><span class="info-value">${esc(baseURL)}</span></div>
        </div>
    `;
}

// Uptime
async function fetchUptime() {
    const data = await rpcCall('/ext/info', 'info.uptime');
    const el = document.getElementById('uptimeCard');
    if (data?.result) {
        const w = (data.result.weightedAveragePercentage * 100).toFixed(2);
        const r = (data.result.rewardingStakePercentage * 100).toFixed(2);
        document.getElementById('statUptime').textContent = w + '%';
        el.innerHTML = `
            <div class="uptime-display">
                <div class="uptime-value">${w}%</div>
                <div class="uptime-label">Weighted Average Uptime</div>
            </div>
            <div class="info-list" style="margin-top:0.75rem;">
                <div class="info-row"><span class="info-label">Rewarding Stake</span><span class="info-value">${r}%</span></div>
            </div>
            <div class="uptime-bar"><div class="uptime-fill" style="width:${w}%"></div></div>
        `;
    } else {
        document.getElementById('statUptime').textContent = 'N/A';
        el.innerHTML = '<div class="empty-state">Uptime not available (not a validator?)</div>';
    }
}

// Peers
async function fetchPeers() {
    const data = await rpcCall('/ext/info', 'info.peers');
    const el = document.getElementById('peersCard');
    if (data?.result?.peers) {
        const peers = data.result.peers;
        document.getElementById('statPeers').textContent = peers.length;
        if (peers.length === 0) {
            el.innerHTML = '<div class="empty-state">No peers connected</div>';
        } else {
            el.innerHTML = '<div class="peer-list">' + peers.slice(0, 20).map(p => `
                <div class="peer-item">
                    <div><div class="peer-id">${esc(p.nodeID)}</div><div class="peer-meta">${esc(p.ip || 'Unknown')}</div></div>
                    <span class="peer-version">${esc(p.version || '?')}</span>
                </div>
            `).join('') + (peers.length > 20 ? '<div class="empty-state">+' + (peers.length - 20) + ' more</div>' : '') + '</div>';
        }
    } else {
        document.getElementById('statPeers').textContent = '0';
        el.innerHTML = '<div class="error-state">Failed to load peers</div>';
    }
}

async function fetchPeersDetailed() {
    const data = await rpcCall('/ext/info', 'info.peers');
    const el = document.getElementById('peersTableCard');
    if (data?.result?.peers?.length) {
        const peers = data.result.peers;
        el.innerHTML = `
            <div style="max-height:350px;overflow-y:auto;">
                <table class="data-table">
                    <thead>
                        <tr>
                            <th>Node ID</th>
                            <th>IP</th>
                            <th>Version</th>
                            <th>Last Sent</th>
                            <th>Last Received</th>
                        </tr>
                    </thead>
                    <tbody>${peers.slice(0, 30).map(p => `
                        <tr>
                            <td title="${esc(p.nodeID)}">${esc(p.nodeID?.substring(0, 20))}...</td>
                            <td>${esc(p.ip || 'N/A')}</td>
                            <td>${esc(p.version || 'N/A')}</td>
                            <td>${p.lastSent ? esc(new Date(p.lastSent).toLocaleTimeString()) : 'N/A'}</td>
                            <td>${p.lastReceived ? esc(new Date(p.lastReceived).toLocaleTimeString()) : 'N/A'}</td>
                        </tr>
                    `).join('')}</tbody>
                </table>
            </div>
            ${peers.length > 30 ? `<div class="empty-state" style="padding:0.5rem;">Showing 30 of ${peers.length} peers</div>` : ''}`;
    } else {
        el.innerHTML = '<div class="empty-state">No peer data</div>';
    }
}

async function fetchNetworkStats() {
    const data = await rpcCall('/ext/info', 'info.peers');
    const el = document.getElementById('networkStatsCard');
    if (data?.result?.peers) {
        const peers = data.result.peers;
        const versions = {};
        peers.forEach(p => { const v = p.version || 'unknown'; versions[v] = (versions[v] || 0) + 1; });
        const sortedVersions = Object.entries(versions).sort((a,b) => b[1] - a[1]);
        
        el.innerHTML = `
            <div class="info-list" style="margin-bottom:1rem;">
                <div class="info-row">
                    <span class="info-label">Total Peers</span>
                    <span class="info-value" style="font-size:1rem;font-weight:600;color:var(--avax-red);">${peers.length}</span>
                </div>
                <div class="info-row">
                    <span class="info-label">Unique Versions</span>
                    <span class="info-value">${Object.keys(versions).length}</span>
                </div>
            </div>
            <div style="margin-bottom:0.5rem;">
                <span style="font-size:0.65rem;color:var(--text-muted);text-transform:uppercase;letter-spacing:0.05em;">Version Distribution</span>
            </div>
            <div class="info-list">
                ${sortedVersions.slice(0, 5).map(([v, c]) => {
                    const pct = ((c / peers.length) * 100).toFixed(1);
                    return `
                        <div class="info-row" style="flex-wrap:wrap;">
                            <span class="info-label" style="flex:1;min-width:150px;">${esc(v)}</span>
                            <span class="info-value" style="min-width:80px;">${c} <span style="color:var(--text-muted);">(${pct}%)</span></span>
                        </div>`;
                }).join('')}
                ${sortedVersions.length > 5 ? `<div class="empty-state" style="padding:0.5rem;">+${sortedVersions.length - 5} more versions</div>` : ''}
            </div>
        `;
    } else {
        el.innerHTML = '<div class="error-state">Failed to load stats</div>';
    }
}

// Health
async function fetchHealth() {
    let data;
    try { data = await (await fetch(baseURL + '/ext/health')).json(); } catch (e) { data = null; }
    const statusDot = document.getElementById('statusDot');
    const statusText = document.getElementById('statusText');
    const el = document.getElementById('healthCard');
    if (data) {
        statusDot.className = 'status-dot ' + (data.healthy ? 'healthy' : 'unhealthy');
        statusText.textContent = data.healthy ? 'Healthy' : 'Unhealthy';
        if (data.checks) {
            const checks = Object.entries(data.checks);
            const passing = checks.filter(([_,c]) => !c.error).length;
            document.getElementById('statHealth').textContent = passing + '/' + checks.length;
            el.innerHTML = '<div class="health-grid">' + checks.map(([n,c]) => `
                <div class="health-item">
                    <span class="health-name">${esc(n)}</span>
                    <span class="health-badge ${c.error ? 'fail' : 'pass'}">${c.error ? 'FAIL' : 'PASS'}</span>
                </div>
            `).join('') + '</div>';
        }
    } else {
        statusDot.className = 'status-dot unhealthy';
        statusText.textContent = 'Unreachable';
        document.getElementById('statHealth').textContent = '—';
        el.innerHTML = '<div class="error-state">Failed to fetch health</div>';
    }
}

// Helper to format health check message (full version for detailed view)
function formatHealthMessageFull(check) {
    if (check.error) {
        if (typeof check.error === 'string') return check.error;
        if (check.error.message) return check.error.message;
        return JSON.stringify(check.error, null, 2);
    }
    if (check.message) {
        if (typeof check.message === 'string') return check.message;
        if (typeof check.message === 'object') {
            // Show full JSON for detailed view
            return JSON.stringify(check.message, null, 2);
        }
        return String(check.message);
    }
    return '—';
}

// Helper to format health check message (short version for overview)
function formatHealthMessage(check) {
    if (check.error) {
        if (typeof check.error === 'string') return check.error.substring(0, 100);
        if (check.error.message) return check.error.message.substring(0, 100);
        return JSON.stringify(check.error).substring(0, 100);
    }
    if (check.message) {
        if (typeof check.message === 'string') return check.message.substring(0, 100);
        if (typeof check.message === 'object') {
            const msg = check.message;
            // If message has a direct message property, use that
            if (msg.message && typeof msg.message === 'string') {
                return msg.message.substring(0, 100);
            }
            // For subsystem status objects, show subsystem names
            const keys = Object.keys(msg);
            if (keys.length === 0) return 'OK';
            if (keys.every(k => typeof msg[k] === 'object')) {
                return keys.join(', ');
            }
            const simpleEntries = keys.filter(k => typeof msg[k] !== 'object');
            if (simpleEntries.length > 0) {
                return simpleEntries.map(k => `${k}: ${msg[k]}`).join(', ').substring(0, 100);
            }
            return `${keys.length} subsystems`;
        }
        return String(check.message).substring(0, 100);
    }
    return '—';
}

async function fetchHealthDetailed() {
    let data;
    try { data = await (await fetch(baseURL + '/ext/health')).json(); } catch (e) { data = null; }
    const el = document.getElementById('healthDetailCard');
    if (data?.checks) {
        const checks = Object.entries(data.checks);
        const passing = checks.filter(([_, c]) => !c.error).length;
        
        el.innerHTML = `
            <div style="margin-bottom:0.75rem;display:flex;gap:1rem;align-items:center;">
                <span style="font-size:0.8rem;color:var(--text-secondary);">
                    <strong style="color:var(--color-success);">${passing}</strong> passing, 
                    <strong style="color:var(--color-error);">${checks.length - passing}</strong> failing
                </span>
            </div>
            <div class="table-container" style="max-height:500px;overflow-y:auto;">
                <table class="data-table">
                    <thead>
                        <tr>
                            <th style="min-width:120px;">Check</th>
                            <th style="width:70px;">Status</th>
                            <th>Message</th>
                            <th style="min-width:130px;">Timestamp</th>
                        </tr>
                    </thead>
                    <tbody>${checks.map(([n, c]) => {
                        const fullMsg = formatHealthMessageFull(c);
                        const isJson = fullMsg.startsWith('{') || fullMsg.startsWith('[');
                        return `
                        <tr>
                            <td style="vertical-align:top;"><strong>${esc(n)}</strong></td>
                            <td style="vertical-align:top;"><span class="health-badge ${c.error ? 'fail' : 'pass'}">${c.error ? 'FAIL' : 'PASS'}</span></td>
                            <td style="vertical-align:top;"><pre class="api-code-block" style="margin:0;padding:0.5rem;min-height:auto;max-height:150px;overflow:auto;font-size:0.65rem;${isJson ? '' : 'white-space:pre-wrap;'}">${esc(fullMsg)}</pre></td>
                            <td style="vertical-align:top;">${c.timestamp ? esc(new Date(c.timestamp).toLocaleString()) : '—'}</td>
                        </tr>`;
                    }).join('')}</tbody>
                </table>
            </div>`;
    } else {
        el.innerHTML = '<div class="error-state">Failed to load health details</div>';
    }
}

async function fetchLiveness() {
    let data;
    try { data = await (await fetch(baseURL + '/ext/health/liveness')).json(); } catch (e) { data = null; }
    const el = document.getElementById('livenessCard');
    el.innerHTML = data ? `
        <div class="info-list">
            <div class="info-row"><span class="info-label">Liveness</span><span class="info-value ${data.healthy ? 'success' : 'error'}">${data.healthy ? 'ALIVE' : 'UNHEALTHY'}</span></div>
        </div>
    ` : '<div class="error-state">Failed</div>';
}

async function fetchReadiness() {
    let data;
    try { data = await (await fetch(baseURL + '/ext/health/readiness')).json(); } catch (e) { data = null; }
    const el = document.getElementById('readinessCard');
    el.innerHTML = data ? `
        <div class="info-list">
            <div class="info-row"><span class="info-label">Readiness</span><span class="info-value ${data.healthy ? 'success' : 'warning'}">${data.healthy ? 'READY' : 'NOT READY'}</span></div>
        </div>
    ` : '<div class="error-state">Failed</div>';
}

// Chains
async function fetchChains() {
    const chains = [
        { id: 'P', name: 'Platform Chain', desc: 'Validators, subnets, staking' },
        { id: 'X', name: 'Exchange Chain', desc: 'Asset transfers' },
        { id: 'C', name: 'Contract Chain', desc: 'EVM smart contracts' }
    ];
    const results = await Promise.all(chains.map(async c => {
        const r = await rpcCall('/ext/info', 'info.isBootstrapped', { chain: c.id });
        return { ...c, synced: r?.result?.isBootstrapped };
    }));
    document.getElementById('statChains').textContent = results.filter(r => r.synced).length + '/3';
    document.getElementById('chainsCard').innerHTML = '<div class="chain-grid">' + results.map(c => `
        <div class="chain-item">
            <div class="chain-header">
                <span class="chain-name">${c.name} <span class="chain-badge">${c.id}</span></span>
                <span class="chain-status ${c.synced ? 'synced' : 'syncing'}">${c.synced ? 'Synced' : 'Syncing'}</span>
            </div>
            <div class="chain-desc">${c.desc}</div>
        </div>
    `).join('') + '</div>';
}

// Subnets - show tracked subnets with health, and all other subnets separately
async function fetchSubnets() {
    const PRIMARY_NETWORK_ID = '11111111111111111111111111111111LpoYY';
    const el = document.getElementById('subnetsCard');
    
    // Get tracked subnets from config and all blockchains
    const [configData, blockchainsData] = await Promise.all([
        rpcCall('/ext/admin', 'admin.getConfig'),
        rpcCall('/ext/P', 'platform.getBlockchains')
    ]);
    
    // Parse tracked subnets from config
    let trackedSubnetIds = new Set([PRIMARY_NETWORK_ID]);
    if (configData?.result?.config) {
        const config = configData.result.config;
        const trackSubnets = config['track-subnets'] || config.trackSubnets || '';
        if (trackSubnets) {
            trackSubnets.split(',').forEach(id => {
                const trimmed = id.trim();
                if (trimmed) trackedSubnetIds.add(trimmed);
            });
        }
    }
    
    if (blockchainsData?.result?.blockchains) {
        const bcs = blockchainsData.result.blockchains;
        const trackedSubnets = new Map();
        const otherSubnets = new Map();
        
        // Always add P-Chain to Primary Network (tracked)
        trackedSubnets.set(PRIMARY_NETWORK_ID, [{
            id: 'P',
            name: 'P-Chain',
            subnetID: PRIMARY_NETWORK_ID,
            vmID: 'platformvm'
        }]);
        
        // Group blockchains by subnet, separating tracked from others
        bcs.forEach(bc => {
            const sid = bc.subnetID || PRIMARY_NETWORK_ID;
            const isTracked = trackedSubnetIds.has(sid);
            const targetMap = isTracked ? trackedSubnets : otherSubnets;
            
            if (!targetMap.has(sid)) targetMap.set(sid, []);
            targetMap.get(sid).push(bc);
        });
        
        // Count other subnets/L1s (excluding Primary Network and tracked)
        const otherSubnetCount = otherSubnets.size;
        document.getElementById('statSubnets').textContent = otherSubnetCount;
        
        // Only check health for tracked subnets
        const trackedChains = [];
        for (const [sid, chains] of trackedSubnets) {
            chains.forEach(c => trackedChains.push({ ...c, subnetID: sid }));
        }
        
        const healthChecks = await Promise.all(
            trackedChains.map(async c => {
                const result = await rpcCall('/ext/info', 'info.isBootstrapped', { chain: c.id });
                return { 
                    chainId: c.id, 
                    subnetID: c.subnetID,
                    isBootstrapped: result?.result?.isBootstrapped || false 
                };
            })
        );
        
        const chainHealth = new Map();
        healthChecks.forEach(h => chainHealth.set(h.chainId, h.isBootstrapped));
        
        let html = '';
        
        // Section 1: Tracked/Syncing subnets (with health status)
        if (trackedSubnets.size > 0) {
            html += '<div style="margin-bottom:1rem;"><span style="font-size:0.7rem;color:var(--text-muted);text-transform:uppercase;letter-spacing:0.05em;">Syncing</span></div>';
            html += '<div class="subnet-grid" style="margin-bottom:1.5rem;">';
            for (const [sid, chains] of trackedSubnets) {
                const isPrimary = sid === PRIMARY_NETWORK_ID;
                const healthyCount = chains.filter(c => chainHealth.get(c.id)).length;
                const allHealthy = healthyCount === chains.length;
                
                html += `
                    <div class="subnet-item">
                        <div class="subnet-header">
                            <span class="subnet-name">${isPrimary ? 'Primary Network' : 'Tracked Subnet/L1'}</span>
                            <span class="chain-status ${allHealthy ? 'synced' : 'syncing'}">${allHealthy ? 'Healthy' : `${healthyCount}/${chains.length} Synced`}</span>
                        </div>
                        ${!isPrimary ? `<div class="subnet-id">${esc(sid)}</div>` : ''}
                        <div class="subnet-chains">
                            ${chains.map(c => {
                                const isHealthy = chainHealth.get(c.id);
                                return `
                                    <div class="subnet-chain">
                                        <span>${esc(c.name || 'Unnamed')}</span>
                                        <span class="chain-status ${isHealthy ? 'synced' : 'syncing'}" style="font-size:0.6rem;">${isHealthy ? 'Synced' : 'Syncing'}</span>
                                    </div>`;
                            }).join('')}
                        </div>
                    </div>`;
            }
            html += '</div>';
        }
        
        // Section 2: Other subnets (no health status)
        if (otherSubnets.size > 0) {
            html += '<div style="margin-bottom:1rem;"><span style="font-size:0.7rem;color:var(--text-muted);text-transform:uppercase;letter-spacing:0.05em;">All Subnets/L1s</span></div>';
            html += '<div class="subnet-grid">';
            for (const [sid, chains] of otherSubnets) {
                html += `
                    <div class="subnet-item">
                        <div class="subnet-header">
                            <span class="subnet-name">Subnet/L1</span>
                            <span style="font-size:0.7rem;color:var(--text-muted);">${chains.length} chain${chains.length > 1 ? 's' : ''}</span>
                        </div>
                        <div class="subnet-id">${esc(sid)}</div>
                        <div class="subnet-chains">
                            ${chains.map(c => `
                                <div class="subnet-chain">
                                    <span>${esc(c.name || 'Unnamed')}</span>
                                    <span style="font-size:0.6rem;color:var(--text-muted);">${esc(c.vmID?.substring(0, 8) || '')}...</span>
                                </div>`).join('')}
                        </div>
                    </div>`;
            }
            html += '</div>';
        }
        
        el.innerHTML = html || '<div class="empty-state">No subnets found</div>';
    } else {
        document.getElementById('statSubnets').textContent = '—';
        el.innerHTML = '<div class="empty-state">Subnet data not available yet</div>';
    }
}

// VMs
async function fetchVMs() {
    const data = await rpcCall('/ext/info', 'info.getVMs');
    const el = document.getElementById('vmsCard');
    if (data?.result?.vms) {
        const vms = Object.entries(data.result.vms);
        document.getElementById('statVMs').textContent = vms.length;
        el.innerHTML = '<div class="vm-grid">' + vms.map(([id, aliases]) => `
            <div class="vm-item">
                <span class="vm-name">${esc(aliases[0] || 'Unknown')}</span>
                <span class="vm-id">${esc(id)}</span>
            </div>
        `).join('') + '</div>';
    } else {
        document.getElementById('statVMs').textContent = '—';
        el.innerHTML = '<div class="error-state">Failed to load VMs</div>';
    }
}

// ACPs
async function fetchACPs() {
    const data = await rpcCall('/ext/info', 'info.acps');
    const el = document.getElementById('acpsCard');
    if (data?.result?.acps) {
        const acps = Object.entries(data.result.acps);
        if (acps.length === 0) {
            el.innerHTML = '<div class="empty-state">No active ACPs</div>';
        } else {
            el.innerHTML = `
                <div class="table-container">
                    <table class="data-table">
                        <thead>
                            <tr>
                                <th>ACP</th>
                                <th style="text-align:right;">Support Weight</th>
                                <th style="text-align:right;">Object Weight</th>
                                <th style="text-align:right;">Abstain Weight</th>
                            </tr>
                        </thead>
                        <tbody>${acps.map(([num, acp]) => `
                            <tr>
                                <td><strong>ACP-${esc(num)}</strong></td>
                                <td style="text-align:right;font-variant-numeric:tabular-nums;">${esc(String(acp.supportWeight || 0))}</td>
                                <td style="text-align:right;font-variant-numeric:tabular-nums;">${esc(String(acp.objectWeight || 0))}</td>
                                <td style="text-align:right;font-variant-numeric:tabular-nums;">${esc(String(acp.abstainWeight || 0))}</td>
                            </tr>
                        `).join('')}</tbody>
                    </table>
                </div>`;
        }
    } else {
        el.innerHTML = '<div class="empty-state">No ACP data available</div>';
    }
}

// Upgrades
async function fetchUpgrades() {
    const data = await rpcCall('/ext/info', 'info.upgrades');
    const el = document.getElementById('upgradesCard');
    if (data?.result) {
        const u = data.result;
        el.innerHTML = '<div class="info-list">' + Object.entries(u).filter(([k]) => k.endsWith('Time')).map(([k,v]) => `
            <div class="info-row"><span class="info-label">${esc(k)}</span><span class="info-value">${v ? esc(new Date(v).toLocaleString()) : 'Not scheduled'}</span></div>
        `).join('') + '</div>';
    } else {
        el.innerHTML = '<div class="error-state">Failed to load upgrades</div>';
    }
}

// TX Fees
async function fetchTxFees() {
    const data = await rpcCall('/ext/info', 'info.getTxFee');
    const el = document.getElementById('txFeesCard');
    if (data?.result) {
        const f = data.result;
        el.innerHTML = '<div class="info-list">' + Object.entries(f).map(([k,v]) => `
            <div class="info-row"><span class="info-label">${esc(k)}</span><span class="info-value">${esc(nanoToAvax(v))} AVAX</span></div>
        `).join('') + '</div>';
    } else {
        el.innerHTML = '<div class="error-state">Failed to load tx fees</div>';
    }
}

// P-Chain Height
async function fetchPChainHeight() {
    const data = await rpcCall('/ext/P', 'platform.getHeight');
    const el = document.getElementById('pchainHeightCard');
    if (data?.result?.height) {
        el.innerHTML = `
            <div class="info-list">
                <div class="info-row"><span class="info-label">P-Chain Height</span><span class="info-value">${esc(data.result.height)}</span></div>
            </div>
        `;
    } else {
        el.innerHTML = '<div class="empty-state">P-Chain height not available</div>';
    }
}

// C-Chain Info
async function fetchCChainInfo() {
    const [blockNum, chainId] = await Promise.all([
        fetchJSON('/ext/bc/C/rpc', 'POST', { jsonrpc: '2.0', id: 1, method: 'eth_blockNumber', params: [] }),
        fetchJSON('/ext/bc/C/rpc', 'POST', { jsonrpc: '2.0', id: 1, method: 'eth_chainId', params: [] })
    ]);
    const el = document.getElementById('cchainInfoCard');
    el.innerHTML = `
        <div class="info-list">
            <div class="info-row"><span class="info-label">C-Chain Block Height</span><span class="info-value">${blockNum?.result ? parseInt(blockNum.result, 16) : 'N/A'}</span></div>
            <div class="info-row"><span class="info-label">Chain ID</span><span class="info-value">${chainId?.result ? parseInt(chainId.result, 16) : 'N/A'}</span></div>
        </div>
    `;
}

// Helper to format uptime percentage correctly
function formatUptime(uptime) {
    if (uptime === undefined || uptime === null) return 'N/A';
    const val = parseFloat(uptime);
    // If value > 1, it's already a percentage; if <= 1, it's a decimal (0-1 range)
    const pct = val > 1 ? val : val * 100;
    return pct.toFixed(2) + '%';
}

// Validators
async function fetchValidators() {
    const data = await rpcCall('/ext/P', 'platform.getCurrentValidators', { subnetID: '11111111111111111111111111111111LpoYY' });
    const el = document.getElementById('validatorsCard');
    if (data?.result?.validators?.length) {
        const vals = data.result.validators.slice(0, 10);
        el.innerHTML = `<table class="data-table">
            <thead><tr><th>Node ID</th><th>Stake</th><th>Start Time</th><th>End Time</th><th>Uptime</th></tr></thead>
            <tbody>${vals.map(v => `
                <tr>
                    <td>${esc(v.nodeID?.substring(0, 20))}...</td>
                    <td>${esc(nanoToAvax(v.stakeAmount || v.weight || 0))} AVAX</td>
                    <td>${esc(new Date(parseInt(v.startTime) * 1000).toLocaleDateString())}</td>
                    <td>${esc(new Date(parseInt(v.endTime) * 1000).toLocaleDateString())}</td>
                    <td>${esc(formatUptime(v.uptime))}</td>
                </tr>
            `).join('')}</tbody>
        </table><div class="empty-state">Showing first 10 of ${data.result.validators.length} validators</div>`;
    } else {
        el.innerHTML = '<div class="empty-state">Validator data not available</div>';
    }
}

function updateTimestamp() {
    document.getElementById('lastUpdated').textContent = 'Last updated: ' + new Date().toLocaleTimeString();
}

async function refreshAll() {
    const btn = document.getElementById('refreshAllBtn');
    btn.classList.add('spinning');
    await Promise.all([
        fetchNodeInfo(), fetchNetworkInfo(), fetchUptime(), fetchPeers(),
        fetchHealth(), fetchChains(), fetchSubnets(), fetchVMs()
    ]);
    updateTimestamp();
    btn.classList.remove('spinning');
}

refreshAll();

