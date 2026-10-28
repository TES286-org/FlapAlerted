import {
    Chart, LineController, LineElement, PointElement, LinearScale, TimeScale, Tooltip, Legend, Filler, CategoryScale
} from "chart.js";

import 'chartjs-adapter-date-fns';
import JustGage from "justgage";
import {formatSessionRate, getRelativeTime, toTimeElapsed, truncateRouteChanges} from "./util";

Chart.register(
    CategoryScale,
    LineController,
    LineElement,
    PointElement,
    LinearScale,
    TimeScale,
    Tooltip,
    Legend,
    Filler
);

function getFetchOptions() {
    return {
        headers: {
            'X-AS': Math.floor(Date.now() / 1000).toString()
        }
    };
}

let gageMaxValue = 400;
let gageMaxValueModifier = 1;
let gageDisableDynamic = false;
const gauge = new JustGage({
    id: "justgage",
    value: 0,
    min: 0,
    max: gageMaxValue,
    label: "Average Route Changes",
    decimals: 2,
    gaugeWidthScale: 0.2,
    pointer: true,
    relativeGaugeSize: true,
    customSectors: {
        // lo and hi values are in %
        percents: true,
        ranges: [
            {
                color: "#43bf58",
                lo: 0,
                hi: 20
            },
            {
                color: "#f7bc08",
                lo: 21,
                hi: 70
            },
            {
                color: "#ff3b30",
                lo: 71,
                hi: 100
            }
        ]
    }
});

const dataFlapCount = {
    labels: [],
    datasets: [
        {
            label: "Count of active prefixes",
            fill: false,
            backgroundColor: "rgba(255,47,5,0.4)",
            borderColor: "rgba(255,47,5,1)",
            pointBorderWidth: 1,
            pointBackgroundColor: "#fff",
            pointRadius: 4,
            pointHitRadius: 10,
            data: []
        }
    ]
};

const dataRouteChange = {
    labels: [],
    datasets: [
        {
            label: "Path updates",
            fill: "origin",
            backgroundColor: "rgba(153,102,255,0.35)",
            borderColor: "rgba(153,102,255,1)",
            pointBorderWidth: 1,
            pointBackgroundColor: "#fff",
            pointRadius: 4,
            pointHitRadius: 10,
            data: []
        },
        {
            label: "Same-path updates",
            fill: "-1",
            backgroundColor: "rgba(255,159,64,0.35)",
            borderColor: "rgba(255,159,64,1)",
            pointBorderWidth: 1,
            pointBackgroundColor: "#fff",
            pointRadius: 4,
            pointHitRadius: 10,
            data: []
        },
        {
            label: "Path Withdrawals",
            fill: "-1",
            backgroundColor: "rgba(255,99,132,0.35)",
            borderColor: "rgba(255,99,132,1)",
            pointBorderWidth: 1,
            pointBackgroundColor: "#fff",
            pointRadius: 4,
            pointHitRadius: 10,
            data: []
        },
        {
            label: "Total Changes (listed prefixes)",
            stack: "listed",
            fill: false,
            borderDash: [6, 4],
            borderWidth: 2,
            backgroundColor: "rgb(50,168,5)",
            borderColor: "rgb(50,168,5)",
            pointBorderWidth: 1,
            pointBackgroundColor: "#fff",
            pointRadius: 4,
            pointHitRadius: 10,
            data: []
        }
    ]
};

const ctxFlapCount = document.getElementById("chartFlapCount").getContext("2d");
const ctxRoute = document.getElementById("chartRoute").getContext("2d");

const liveFlapChart = new Chart(
    ctxFlapCount,
    {
        type: "line",
        data: dataFlapCount,
        options: {
            scales: {
                x: {
                    type: "time",
                    time: {
                        unit: "minute",
                        displayFormats: {
                            minute: "HH:mm"
                        },
                        tooltipFormat: "HH:mm:ss"
                    }
                },
                y: {
                    suggestedMin: 0,
                    suggestedMax: 10
                }
            },
            maintainAspectRatio: false
        }
    }
);


const liveRouteChart = new Chart(
    ctxRoute,
    {
        type: "line",
        data: dataRouteChange,
        options: {
            interaction: {
                mode: "index",
                intersect: true
            },
            scales: {
                x: {
                    type: "time",
                    time: {
                        unit: "minute",
                        displayFormats: {
                            minute: "HH:mm"
                        },
                        tooltipFormat: "HH:mm:ss"
                    }
                },
                y: {
                    stacked: true,
                    suggestedMin: 0,
                    suggestedMax: 15
                }
            },
            maintainAspectRatio: false,
            plugins: {
                tooltip: {
                    callbacks: {
                        label: (context) => `${context.dataset.label}: ${context.parsed.y}/sec`,
                        footer: (items) => {
                            const stacked = items.filter(i => i.dataset.stack !== "listed");
                            return `Total: ${stacked.reduce((s, i) => s + i.parsed.y, 0).toFixed(2)}/sec`;
                        }
                    }
                }
            }
        }
    }
);

const ctxImport = document.getElementById("chartImportCount").getContext("2d");

const dataImportCount = {
    labels: [],
    datasets: [
        {
            label: "Total Imported Routes",
            fill: true,
            backgroundColor: "rgb(37 99 235 / 10%)",
            borderColor: "rgb(37 99 235)",
            pointBorderWidth: 1,
            pointBackgroundColor: "#fff",
            pointRadius: 4,
            pointHitRadius: 10,
            data: []
        }
    ]
};


const liveImportChart = new Chart(
    ctxImport,
    {
        type: "line",
        data: dataImportCount,
        options: {
            scales: {
                x: {
                    type: "time",
                    time: {
                        unit: "minute",
                        displayFormats: { minute: "HH:mm" },
                        tooltipFormat: "HH:mm:ss"
                    }
                },
                y: {
                    beginAtZero: false,
                }
            },
            maintainAspectRatio: false
        }
    }
);

// --- Per-session BGP update rate chart (100% stacked area) ---
// Each established BGP session contributes one stacked area. The y-axis is
// normalized to 0-100% so the chart shows each session's *share* of the
// total update activity rather than the absolute rate. The raw per-second
// rate is preserved in a parallel _rawData array on each dataset so the
// tooltip can display both the percentage and the absolute value.
const SESSION_CHART_MAX_POINTS = 51;
// Deterministic color palette so the same label keeps the same color across
// snapshots. Sized to cover the 20-session cap enforced by the backend.
// "others" uses a neutral slate so it is visually distinct from any single
// session color and reads as an aggregate rather than a real peer.
const SESSION_CHART_PALETTE = [
    "rgba(37, 99, 235, 0.55)",   // blue
    "rgba(13, 148, 136, 0.55)",  // teal
    "rgba(5, 150, 105, 0.55)",   // green
    "rgba(132, 204, 22, 0.55)",  // lime
    "rgba(234, 179, 8, 0.55)",   // amber
    "rgba(249, 115, 22, 0.55)",  // orange
    "rgba(239, 68, 68, 0.55)",   // red
    "rgba(219, 39, 119, 0.55)",  // pink
    "rgba(168, 85, 247, 0.55)",  // purple
    "rgba(79, 70, 229, 0.55)",  // indigo
    "rgba(14, 165, 233, 0.55)",  // sky
    "rgba(20, 184, 166, 0.55)",  // cyan
    "rgba(101, 163, 13, 0.55)",  // olive
    "rgba(245, 158, 11, 0.55)",  // yellow
    "rgba(220, 38, 38, 0.55)",   // crimson
    "rgba(190, 24, 93, 0.55)",   // rose
    "rgba(124, 58, 237, 0.55)",  // violet
    "rgba(49, 46, 129, 0.55)",   // navy
    "rgba(8, 145, 178, 0.55)",   // deep-teal
    "rgba(77, 124, 15, 0.55)"    // forest
];
const SESSION_OTHERS_COLOR_BG = "rgba(100, 116, 139, 0.55)";  // slate-500
const SESSION_OTHERS_COLOR_BORDER = "rgba(100, 116, 139, 1)";

function colorForSessionLabel(label, index) {
    if (label === "others") {
        return SESSION_OTHERS_COLOR_BG;
    }
    return SESSION_CHART_PALETTE[index % SESSION_CHART_PALETTE.length];
}

function borderForSessionLabel(label, index) {
    if (label === "others") {
        return SESSION_OTHERS_COLOR_BORDER;
    }
    const bg = colorForSessionLabel(label, index);
    // Promote alpha to ~1.0 for the line border by replacing the alpha group
    return bg.replace(/rgba\(([^)]+),\s*[\d.]+\)/, "rgba($1, 1)");
}

const ctxSessionRate = document.getElementById("chartSessionRate").getContext("2d");

const liveSessionRateChart = new Chart(
    ctxSessionRate,
    {
        type: "line",
        data: {
            labels: [],
            datasets: []
        },
        options: {
            interaction: {
                mode: "index",
                axis: "x",
                intersect: false
            },
            scales: {
                x: {
                    type: "time",
                    time: {
                        unit: "minute",
                        displayFormats: { minute: "HH:mm" },
                        tooltipFormat: "HH:mm:ss"
                    }
                },
                y: {
                    stacked: true,
                    min: 0,
                    max: 100,
                    ticks: {
                        // Render the y-axis as percentages so the chart reads
                        // as a 100% stacked area, not a 0-100 count.
                        callback: (value) => `${value}%`
                    }
                }
            },
            maintainAspectRatio: false,
            plugins: {
                legend: {
                    display: true,
                    // Match the other charts on the dashboard, which use the
                    // Chart.js default legend position (top).
                    position: "top",
                    labels: {
                        boxWidth: 12,
                        boxHeight: 12,
                        font: { size: 10 },
                        // Hide the legend entry for datasets that have left
                        // (all-null data) so the legend only shows active
                        // sessions rather than historical ghosts.
                        filter: (item, data) => {
                            const ds = data.datasets[item.datasetIndex];
                            if (!ds || !ds.data) return true;
                            return ds.data.some(v => v !== null && v !== undefined);
                        }
                    }
                },
                tooltip: {
                    callbacks: {
                        // Show both the percentage (from the normalized y value)
                        // and the raw per-second rate (from _rawData) so the
                        // viewer can read share and magnitude at once.
                        label: (ctx) => {
                            const pct = ctx.parsed.y.toFixed(1);
                            const raw = ctx.dataset._rawData?.[ctx.dataIndex];
                            const rateStr = (raw !== null && raw !== undefined)
                                ? ` (${raw.toFixed(2)}/sec)`
                                : "";
                            return `${ctx.dataset.label}: ${pct}%${rateStr}`;
                        },
                        footer: (items) => {
                            // Sum the raw rates for the hovered timestamp.
                            let total = 0;
                            for (const i of items) {
                                const raw = i.dataset._rawData?.[i.dataIndex];
                                if (raw !== null && raw !== undefined) {
                                    total += raw;
                                }
                            }
                            return `Total: ${total.toFixed(2)}/sec`;
                        }
                    }
                }
            }
        }
    }
);

// Map from session label to its dataset index in the chart. Allows O(1)
// lookup when updating values across stats ticks.
const sessionDatasetIndex = new Map();

// updateSessionRateChart ingests one ListSessions snapshot per stats tick.
//
// The chart is a 100% stacked area: each session's per-second rate is
// normalized to its share of the total update activity at that tick, so
// the stacked areas always sum to 100%. The raw per-second rate is kept
// in a parallel _rawData array (one entry per label) so the tooltip can
// show both the percentage and the absolute value.
//
// Design notes:
// - New sessions are back-filled with null so Chart.js renders a gap rather
//   than a fake zero-rate history (which would otherwise drag the stack
//   baseline down for past ticks).
// - Sessions that have left (not in this tick) get a null point so their
//   stacked contribution disappears at the current timestamp.
// - When the total rate for a tick is zero (no activity anywhere), every
//   session gets 0% rather than NaN; this avoids a flat 100% spike on a
//   single session from a 0/0 division.
// - Departed datasets are pruned once they are entirely null (every point
//   in the window is null) so the internal arrays don't grow unbounded.
function updateSessionRateChart(sessions, unixTime, dataIntervalSec, update) {
    const { labels, datasets } = liveSessionRateChart.data;

    // Slide window when full. Both data (percentages) and _rawData
    // (absolute rates) must shift in lockstep so indices stay aligned.
    if (labels.length >= SESSION_CHART_MAX_POINTS) {
        labels.shift();
        for (const ds of datasets) {
            ds.data.shift();
            if (ds._rawData) {
                ds._rawData.shift();
            }
        }
    }

    labels.push(unixTime * 1000);

    const seenThisTick = new Set();
    const targetLength = labels.length;

    // First pass: collect raw rates for this tick and compute the total so
    // we can normalize to percentages. We can't normalize inline because we
    // need the total across ALL sessions before writing any percentage.
    const tickRates = new Map();
    let tickTotal = 0;
    for (const session of sessions) {
        const rate = session.RateSec / dataIntervalSec;
        tickRates.set(session.Label, rate);
        tickTotal += rate;
        seenThisTick.add(session.Label);
    }

    // Second pass: append a percentage point (or null) to each dataset.
    for (const [label, rate] of tickRates) {
        let idx = sessionDatasetIndex.get(label);
        if (idx === undefined) {
            idx = datasets.length;
            const dsIdx = idx;
            datasets.push({
                label,
                fill: true,
                stack: "sessions",
                backgroundColor: colorForSessionLabel(label, dsIdx),
                borderColor: borderForSessionLabel(label, dsIdx),
                borderWidth: 1,
                pointRadius: 4,
                pointHoverRadius: 4,
                pointHitRadius: 10,
                pointBackgroundColor: "#fff",
                pointBorderWidth: 1,
                // Pad with nulls for past ticks where this session did not
                // exist yet, so Chart.js renders gaps instead of flat zeros.
                data: new Array(targetLength - 1).fill(null),
                // _rawData mirrors data length 1:1 and holds the absolute
                // per-second rate; consumed by the tooltip callback.
                _rawData: new Array(targetLength - 1).fill(null)
            });
            sessionDatasetIndex.set(label, idx);
        }

        const ds = datasets[idx];
        // Pad to the new length if the dataset was created mid-window or
        // if previous ticks were skipped (e.g. the backend "others" bucket
        // only appears once sessions exceed the cap).
        while (ds.data.length < targetLength - 1) {
            ds.data.push(null);
            ds._rawData.push(null);
        }
        const pct = tickTotal > 0 ? (rate / tickTotal) * 100 : 0;
        ds.data.push(pct);
        ds._rawData.push(rate);
    }

    // Push null (skipped) for datasets not present in this tick so their
    // stacked contribution disappears for the current point.
    for (const ds of datasets) {
        if (!seenThisTick.has(ds.label)) {
            while (ds.data.length < targetLength - 1) {
                ds.data.push(null);
                ds._rawData.push(null);
            }
            ds.data.push(null);
            ds._rawData.push(null);
        }
    }

    // Prune datasets that are entirely null across the whole window: the
    // session has been gone long enough that no point in the visible range
    // carries data. This keeps the internal arrays bounded on instances
    // where sessions churn frequently. Indices are rebuilt from scratch.
    pruneDepartedSessionDatasets();

    if (update) {
        liveSessionRateChart.update();
    }
}

// pruneDepartedSessionDatasets removes datasets whose _rawData array contains
// only null/undefined values (meaning the session had no activity at any
// visible point), and rebuilds sessionDatasetIndex to match the new
// compacted indices. Called once per tick by updateSessionRateChart.
function pruneDepartedSessionDatasets() {
    const { datasets } = liveSessionRateChart.data;
    let writeIdx = 0;
    for (let readIdx = 0; readIdx < datasets.length; readIdx++) {
        const ds = datasets[readIdx];
        const rawData = ds._rawData || ds.data;
        const hasData = rawData.some(v => v !== null && v !== undefined);
        if (!hasData) {
            continue;
        }
        if (writeIdx !== readIdx) {
            datasets[writeIdx] = ds;
        }
        writeIdx++;
    }
    if (writeIdx < datasets.length) {
        datasets.length = writeIdx;
        // Rebuild the index map so future lookups point at the compacted
        // positions instead of the stale (pre-pruning) indices.
        sessionDatasetIndex.clear();
        for (let i = 0; i < datasets.length; i++) {
            sessionDatasetIndex.set(datasets[i].label, i);
        }
    }
}

function resetSessionRateChart() {
    liveSessionRateChart.data.labels = [];
    liveSessionRateChart.data.datasets = [];
    sessionDatasetIndex.clear();
    liveSessionRateChart.update("none");
}

const compactChartMedia = window.matchMedia("(max-width: 640px)");

function updateChartPresentation() {
    const compact = compactChartMedia.matches;

    for (const chart of [liveRouteChart, liveFlapChart, liveImportChart, liveSessionRateChart]) {
        for (const dataset of chart.data.datasets) {
            dataset.pointRadius = compact ? 0 : 4;
            dataset.pointHoverRadius = compact ? 3 : 4;
            dataset.pointHitRadius = 10;
        }

        chart.options.interaction = {
            mode: "index",
            axis: "x",
            intersect: false
        };

        // Avoid crowded time labels on narrow screens
        chart.options.scales.x.ticks.maxTicksLimit = compact ? 4 : 10;

        chart.update("none");
    }
}

updateChartPresentation();
compactChartMedia.addEventListener("change", updateChartPresentation);

let peerHistoryChart = null;
const peerHistoryChartContainer = document.getElementById("chartPeerHistoryContainer");
const ctxPeerHistory = document.getElementById("chartPeerHistory").getContext("2d");

function initPeerChart() {
    if (peerHistoryChart) return;
    peerHistoryChart = new Chart(ctxPeerHistory, {
        type: "line",
        data: {
            labels: [],
            datasets: [{
                label: "Changes/sec",
                data: [],
                borderColor: "rgba(37, 99, 235, 1)",
                backgroundColor: "rgba(37, 99, 235, 0.1)",
                fill: true,
                tension: 0.3
            }]
        },
        options: {
            responsive: true,
            maintainAspectRatio: false,
            scales: {
                x: { display: true, title: { display: true, text: 'Minutes Ago' } },
                y: { beginAtZero: true }
            }
        }
    });
}

const peerHistoryDialog = document.getElementById("peerHistoryDialog");
const peerHistoryASNLabel = document.getElementById("peerHistoryASN");
const peerHistoryDetails = document.getElementById("peerHistoryDetails");

// Variable to store the currently viewed ASN so the refresh button knows what to fetch
let currentHistoryASN = null;

async function showPeerHistory(asn) {
    currentHistoryASN = asn;
    peerHistoryASNLabel.innerText = asn;

    peerHistoryDetails.innerText = "";

    const statusElem = document.getElementById("peerHistoryStatus");
    statusElem.innerText = "";

    peerHistoryDialog.showModal();
    initPeerChart();

    // Explicitly reset chart data so old data doesn't flicker
    peerHistoryChart.data.labels = [];
    peerHistoryChart.data.datasets[0].data = [];
    peerHistoryChart.update();

    await fetchPeerHistory(asn);
}

// Add the Refresh Button event listener
document.getElementById("refreshPeerHistory").addEventListener("click", () => {
    if (currentHistoryASN) {
        fetchPeerHistory(currentHistoryASN);
    }
});
document.getElementById("closePeerHistoryDialog").onclick = () => peerHistoryDialog.close();

let asnExplorerURL = null;
function updateCapabilities(response) {
    const data = JSON.parse(response);
    const versionBox = document.getElementById("version");
    const infoBox = document.getElementById("info");

    versionBox.innerText = ` ${data.Version}`;
    if (data.UserParameters.RouteChangeCounter === 0) {
        infoBox.innerText = "Displaying every BGP update received. Removing entries after 1 minute of inactivity.";
    } else {
        infoBox.innerText = `Table listing criteria: > ${data.UserParameters.RouteChangeCounter} route changes/min for ${data.UserParameters.OverThresholdTarget}min to list; ${data.UserParameters.UnderThresholdTarget}min below ${data.UserParameters.ExpiryRouteChangeCounter}/min to expire.`;
    }

    historicalDialogOpenBtn.disabled = data.HistoryProviderAvailable === false;
    asnExplorerURL = data.modHttp.asnExplorerURL;
    if (asnExplorerURL) {
        try {
            sessionStorage.setItem("fa_asnExplorerUrl", asnExplorerURL);
        } catch (_) {}
    }

    gageDisableDynamic = data.modHttp.gageDisableDynamic;
    gageMaxValue = data.modHttp.gageMaxValue;
    gauge.refresh(lastGageValue, gageMaxValue * gageMaxValueModifier);

    if (data.modHttp.maxUserDefined === 0) {
        const userDefinedTrackingForm = document.querySelector("#userDefinedTracking form");
        const submit = userDefinedTrackingForm.querySelector("button[type='submit']");
        submit.disabled = true;
        userDefinedTrackingForm?.addEventListener("submit", (e) => {
            e.preventDefault();
            alert("This feature is disabled on this instance");
        });
    }
}

const lookupPeerForm = document.querySelector("#peerLookup form");
lookupPeerForm?.addEventListener("submit", (e) => {
    e.preventDefault();
    const value = new FormData(lookupPeerForm).get("asn");
    showPeerHistory(value).then();
});

let hideZeroRateEvents = false;
if (localStorage.getItem("fa_hideZeroRate") === "true") {
    hideZeroRateEvents = true;
    document.getElementById("hideZeroRateEventsCheckbox").checked = true;
}
document.getElementById("hideZeroRateEventsCheckbox").addEventListener("click", (e) => {
    hideZeroRateEvents = e.target.checked;
    localStorage.setItem("fa_hideZeroRate", hideZeroRateEvents.toString());
});


function addToChart(liveChart, points, unixTime, dataInterval, update) {
    const { labels, datasets } = liveChart.data;
    const maxPoints = 51;

    // Remove first, so Chart.js processes removals before insertions
    if (labels.length >= maxPoints) {
        labels.shift();

        datasets.forEach(dataset => {
            dataset.data.shift();
        });
    }

    // Make the new label available before appending dataset values
    labels.push(unixTime * 1000);

    datasets.forEach((dataset, i) => {
        if (i >= points.length) return;

        dataset.data.push(points[i] / dataInterval);
    });

    if (update) {
        liveChart.update();
    }
}

const prefixTable = document.getElementById("prefixTableBody");
const peersTableBody = document.getElementById("peersTableBody");
const tablePrefixes = document.getElementById("tablePrefixes");
const tablePeers = document.getElementById("tablePeers");

let activeTableView = 'prefixes';
document.querySelectorAll(".toggle-view-btn").forEach(btn => {
    btn.addEventListener("click", () => {
        if (activeTableView === 'prefixes') {
            activeTableView = 'peers';
            tablePrefixes.classList.add('d-none');
            tablePeers.classList.remove('d-none');
        } else {
            activeTableView = 'prefixes';
            tablePrefixes.classList.remove('d-none');
            tablePeers.classList.add('d-none');
        }
    });
});

function updatePeers(peerList) {
    if (!peerList) {
        peersTableBody.innerHTML = '<tr><td colspan="3" class="text-center"><b>Please wait</b></td></tr>';
        return;
    }

    if (peerList.length === 0) {
        peersTableBody.innerHTML = '<tr><td colspan="3" class="text-center">No active peers detected</td></tr>';
        return;
    }

    // Sort array by highest RateSec
    const sorted = [...peerList].sort((a, b) => b.RateSecAvg - a.RateSecAvg);
    const limit = Math.min(101, sorted.length);

    peersTableBody.innerHTML = "";
    for (let i = 0; i < limit; i++) {
        const item = sorted[i];
        if (hideZeroRateEvents && item.RateSec < 1) continue;

        const rowClass = item.RateSec < 1 ? ' class="inactive"' : "";
        const rateDisplayAvg = item.RateSecAvg !== -1 ? `${item.RateSecAvg.toFixed(2)}/s` : "..";
        const rateDisplay = item.RateSec !== -1 ? `${item.RateSec}/s` : "..";

        const tr = document.createElement("tr");
        if (rowClass) tr.className = "inactive";

        const asnExplorerLink = (typeof asnExplorerURL !== 'undefined' && asnExplorerURL) ?
            `<span>&nbsp;|&nbsp;</span><a href="${asnExplorerURL.replace("{asn}", item.ASN)}" target="_blank" rel="noopener noreferrer" title="Lookup ASN" class="asnExplorerLink">
                <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round">
                    <path d="M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6"></path>
                    <polyline points="15 3 21 3 21 9"></polyline>
                    <line x1="10" y1="14" x2="21" y2="3"></line>
                </svg>
                </a>
            `
            : "";

        tr.innerHTML = `
        <td><a href="#" class="asn-link" data-asn="${item.ASN}">${item.ASN}</a>${asnExplorerLink}</td>
        <td>${rateDisplayAvg}</td>
        <td>${rateDisplay}</td>
    `;
        const asnLink = tr.querySelector(".asn-link");
        asnLink.addEventListener("click", (e) => {
            e.preventDefault();
            showPeerHistory(item.ASN).then();
        });

        asnLink.addEventListener("auxclick", (e) => e.preventDefault());

        peersTableBody.appendChild(tr);
    }
}


async function fetchPeerHistory(asn) {
    const refreshBtn = document.getElementById("refreshPeerHistory");
    const statusElem = document.getElementById("peerHistoryStatus");

    refreshBtn.disabled = true;
    refreshBtn.classList.add("loading-btn");
    statusElem.removeAttribute("data-result-type");
    statusElem.innerText = "Fetching...";

    try {
        const response = await fetch(`peers/asn?asn=${asn}`, getFetchOptions());
        if (!response.ok) throw new Error("Failed to fetch history");

        const data = await response.json();

        if (!data || data.RateSecHistory === null || data.RateSecHistory.length === 0) {
            // Clear chart data
            peerHistoryChart.data.labels = [];
            peerHistoryChart.data.datasets[0].data = [];
            peerHistoryChart.update('none');

            peerHistoryChartContainer.classList.add('d-none');

            statusElem.innerText = "No historical data available for this ASN";
            return;
        }

        peerHistoryChartContainer.classList.remove('d-none');
        const history = data.RateSecHistory;
        peerHistoryChart.data.labels = history.map((_, i) => `${history.length - 1 - i}m`);
        peerHistoryChart.data.datasets[0].data = history;
        peerHistoryChart.update('none');

        peerHistoryDetails.innerText = `Average update rate (60min): ${data.RateSecAvg.toFixed(2)}/sec`;

        statusElem.innerText = "Data updated just now.";
        statusElem.setAttribute("data-result-type", "success");

        // Fade out the success message after 3 seconds
        setTimeout(() => { if(statusElem.innerText.includes("just now")) statusElem.innerText = ""; }, 3000);

    } catch (err) {
        statusElem.innerText = `Error: ${err.message}`;
        statusElem.setAttribute("data-result-type", "error")
    } finally {
        refreshBtn.disabled = false;
        refreshBtn.classList.remove("loading-btn");
    }
}


function updateList(flapList) {
    if (flapList === null) {
        prefixTable.innerHTML = '<tr><td colspan="4" class="text-center"><b>Please wait</b></td></tr>';
        return;
    }

    if (flapList.length === 0) {
        prefixTable.innerHTML = '<tr><td colspan="4" class="text-center">No flapping prefixes detected</td></tr>';
        return;
    }

    const unixTime = Math.floor(Date.now() / 1000);
    const rows = [];

    flapList.sort((a, b) => b.TotalCount - a.TotalCount);
    const limit = Math.min(101, flapList.length);

    for (let i = 0; i < limit; i++) {
        const item = flapList[i];

        if (hideZeroRateEvents && item.RateSec < 1) continue;

        const rowClass = item.RateSec < 1 ? ' class="inactive"' : "";
        const rateDisplay = item.RateSec !== -1 ? `${item.RateSec}/s` : "..";

        rows.push(`<tr${rowClass}>
            <td><a target="_blank" href='analyze/?prefix=${encodeURIComponent(item.Prefix)}'>${item.Prefix}</a></td>
            <td>${toTimeElapsed(unixTime - item.FirstSeen)}</td>
            <td>${truncateRouteChanges(item.TotalCount)}</td>
            <td>${rateDisplay}</td>
        </tr>`);
    }

    prefixTable.innerHTML = rows.join('');
}

const loadingScreen = document.getElementById("loading-screen");

let lastGageValue = 0;
let lastSeenTime = 0;
function getStats() {
    const sessionCountElem = document.getElementById("sessionCount");
    const noBGPFeedsElem = document.getElementById("noBGPFeeds");
    const evtSource = new EventSource("flaps/statStream");
    evtSource.addEventListener("u", (event) => {
        dataUpdate(event, true);
    });
    evtSource.addEventListener("ready", (event) => {
        try {
            updateCapabilities(event.data);
        } catch (error) {
            console.error("updateCapabilities failed:", error);
        }
        loadingScreen.classList.add("d-none");
        liveRouteChart.update('none');
        liveFlapChart.update('none');
        liveImportChart.update('none');
        liveSessionRateChart.update('none');
    });
    evtSource.addEventListener("c", (event) => {
        dataUpdate(event, false);
    });

    const dataIntervalSec = 5;
    const avgArray = [];
    function dataUpdate(event, update) {
        try {
            const js = JSON.parse(event.data);

            const stats = js["Stats"];
            if (stats["Time"] <= lastSeenTime) {
                return; // already applied via snapshot replay
            }
            lastSeenTime = stats["Time"];

            const flapList = js["List"];
            const peerList = js["ListPeers"];
            const sessionList = js["ListSessions"];
            const sessionCount = js["Sessions"];
            if (sessionCount !== -1) {
                sessionCountElem.innerText = sessionCount;
                if (gageDisableDynamic) {
                    gageMaxValueModifier = 1;
                } else if (sessionCount > 0) {
                    gageMaxValueModifier = sessionCount;
                }
            }

            if (sessionCount === 0) {
                noBGPFeedsElem.classList.remove("d-none");
            } else {
                noBGPFeedsElem.classList.add("d-none");
            }

            updateList(flapList);
            updatePeers(peerList);
            updateSessionRateChart(sessionList || [], stats["Time"], dataIntervalSec, update);

            addToChart(
                liveRouteChart,
                [
                    stats["Changes"] - stats["SamePathChanges"] - stats["WithdrawalChanges"], // path updates
                    stats["SamePathChanges"],   // same-path updates
                    stats["WithdrawalChanges"],  // withdrawals
                    stats["ListedChanges"]       // listed prefixes total (overlay line)

                ],
                stats["Time"],
                dataIntervalSec,
                update
            );

            addToChart(liveFlapChart, [stats["Active"]], stats["Time"], 1, update);
            addToChart(liveImportChart, [stats["RouteCount"]], stats["Time"], 1, update);

            avgArray.push(stats["Changes"]);
            if (avgArray.length > 50) {
                avgArray.shift();
            }

            let percentile = [...avgArray].sort((a, b) => a - b);
            percentile = percentile.slice(0, Math.ceil(percentile.length * 0.90));
            const sum = percentile.reduce((s, a) => s + a, 0);
            const avg = sum / percentile.length;
            lastGageValue = avg / dataIntervalSec;
            gauge.refresh(avg / dataIntervalSec, gageMaxValue * gageMaxValueModifier);

        } catch (err) {
            console.log(err);
        }
    }

    evtSource.onerror = (err) => {
        loadingScreen.classList.add("d-none");
        handleConnectionLost(true);
        console.log(err);
    };
    evtSource.onopen = () => {
        // Reset before data replay arrives
        [dataFlapCount, dataRouteChange, dataImportCount].forEach(d => {
            d.labels = [];
            d.datasets.forEach(ds => ds.data = []);
        });
        resetSessionRateChart();
        avgArray.length = 0;
        lastSeenTime = 0;
        handleConnectionLost(false);
    };
}

function handleConnectionLost(lost) {
    const lostErrorElem = document.getElementById("connectionLost");
    if (lost) {
        lostErrorElem.classList.remove("d-none");
    } else {
        lostErrorElem.classList.add("d-none");
    }
}

getStats();

{
    const dialog = document.getElementById("sessionsDialog");
    const loading = document.getElementById("sessionsLoading");
    const table = document.getElementById("sessionsTable");
    const tbody = document.getElementById("sessionsTableBody");
    const error = document.getElementById("sessionsError");

    document.getElementById("closeSessionsDialog")?.addEventListener("click", () => {
        dialog.close();
    });

    document.getElementById("sessionCountLink").onclick = async (ev) => {
        ev.preventDefault();
        loading.classList.remove("d-none");
        table.classList.add("d-none");
        error.classList.add("d-none");
        tbody.innerHTML = "";

        dialog.showModal();

        try {
            const response = await fetch("sessions", getFetchOptions());
            if (!response.ok) throw new Error(`HTTP ${response.status}`);
            const data = await response.json();

            const now = Math.floor(Date.now() / 1000);
            const fragment = document.createDocumentFragment();

            if (data.length === 0) {
                const row = document.createElement("tr");
                const cell = document.createElement("td");
                cell.textContent = "No established sessions";
                cell.colSpan = 6;
                row.appendChild(cell);
                fragment.appendChild(row);
            } else {
                data.sort((a,b) => a.RouterID.localeCompare(b.RouterID));

                data.forEach((entry) => {
                    const row = document.createElement("tr");

                    const cells = [
                        entry.Remote,
                        entry.RouterID,
                        entry.Hostname || "--",
                        toTimeElapsed(now - entry.EstablishTime),
                        Number(entry.ImportCount).toLocaleString(),
                        formatSessionRate(entry.RateSec)
                    ];

                    cells.forEach((text) => {
                        const cell = document.createElement("td");
                        cell.textContent = text;
                        cell.classList.add("SessionsDialog-bodyCell");
                        row.appendChild(cell);
                    });

                    fragment.appendChild(row);
                });
            }
            const total = data.reduce((sum, entry) => sum + Number(entry.ImportCount || 0), 0);
            document.getElementById("totalImportCount").textContent = total.toLocaleString();
            const totalRate = data.reduce((sum, entry) => sum + Number(entry.RateSec || 0), 0);
            document.getElementById("totalSessionRate").textContent = formatSessionRate(totalRate);

            tbody.appendChild(fragment);
            loading.classList.add("d-none");
            table.classList.remove("d-none");
        } catch (err) {
            loading.classList.add("d-none");
            error.textContent = `Error loading sessions: ${err.message}`;
            error.classList.remove("d-none");
        }
    };
}

const historicalDialogOpenBtn = document.getElementById('viewHistoricalEvents');

{
    const historicalDialog = document.getElementById('historicalDialog');
    const closeBtn = document.getElementById('closeHistoricalDialog');
    const tableBody = document.getElementById('historicalTableBody');
    const loadingElem = document.getElementById('historicalLoading');
    const errorElem = document.getElementById('historicalError');
    const tableContainer = document.getElementById('historicalTableContainer');
    const toggleFullTimestamp = document.getElementById('historicalToggleFullTimestamp');

    let showFullTimestamps = false;
    let loading = false;

    historicalDialogOpenBtn.addEventListener('click', async () => {
        historicalDialog.showModal();

        if (!loading) {
            await loadHistoricalEvents();
        }
    });

    closeBtn.addEventListener('click', () => {
        historicalDialog.close();
    });

    function formatTimestamp(timestamp) {
        const date = new Date(timestamp * 1000);

        return showFullTimestamps
            ? date.toLocaleString()
            : getRelativeTime(date);
    }

    function updateVisibleTimestamps() {
        for (const cell of tableBody.querySelectorAll('[data-timestamp]')) {
            cell.textContent = formatTimestamp(Number(cell.dataset.timestamp));
        }
    }

    async function loadHistoricalEvents() {
        loading = true;

        tableBody.replaceChildren();
        errorElem.textContent = '';
        loadingElem.classList.remove('d-none');
        tableContainer.classList.add('d-none');

        try {
            const response = await fetch(
                'flaps/historical/list',
                getFetchOptions()
            );

            if (!response.ok) {
                throw new Error(
                    (await response.text()) ||
                    `Error ${response.status}: ${response.statusText}`
                );
            }

            const data = await response.json();

            if (data.length === 0) {
                const row = document.createElement('tr');
                const cell = document.createElement('td');

                cell.textContent = 'No historical events found';
                cell.colSpan = 4;

                row.appendChild(cell);
                tableBody.appendChild(row);

                tableContainer.classList.remove("d-none");
                return;
            }

            const fragment = document.createDocumentFragment();

            for (const { Prefix, Timestamp, AvgChangeRate } of data) {
                const row = document.createElement('tr');
                row.className = 'historicalDialog-row';

                const prefixTd = document.createElement('td');
                prefixTd.textContent = Prefix;

                const dateTd = document.createElement('td');
                dateTd.dataset.timestamp = String(Timestamp);
                dateTd.textContent = formatTimestamp(Timestamp);

                const avgChangeRateTd = document.createElement('td');
                avgChangeRateTd.textContent = AvgChangeRate.toFixed(2);

                const actionTd = document.createElement('td');

                const link = document.createElement('a');
                link.href =
                    `analyze/?prefix=${encodeURIComponent(Prefix)}` +
                    `&timestamp=${Timestamp}`;
                link.textContent = 'View';

                actionTd.appendChild(link);

                row.append(
                    prefixTd,
                    dateTd,
                    avgChangeRateTd,
                    actionTd
                );

                fragment.appendChild(row);
            }

            tableBody.appendChild(fragment);
            tableContainer.classList.remove('d-none');
        } catch (error) {
            errorElem.textContent =
                `Failed to load events: ${error.message}`;
        } finally {
            loading = false;
            loadingElem.classList.add('d-none');
        }
    }

    toggleFullTimestamp.addEventListener('change', () => {
        showFullTimestamps = toggleFullTimestamp.checked;
        updateVisibleTimestamps();
    });
}

// Gauge-only view toggle
const gaugeOnlyToggle = document.getElementById("gaugeOnlyToggle");
if (gaugeOnlyToggle) {
    // Load saved preference
    const isGaugeOnly = localStorage.getItem("fa_gaugeOnlyView") === "true";
    if (isGaugeOnly) {
        document.body.classList.add("gauge-only");
        gaugeOnlyToggle.textContent = "Full view";
    }

    gaugeOnlyToggle.addEventListener("click", () => {
        const currentlyGaugeOnly = document.body.classList.toggle("gauge-only");
        gaugeOnlyToggle.textContent = currentlyGaugeOnly ? "Full view" : "Gauge-only view";
        localStorage.setItem("fa_gaugeOnlyView", currentlyGaugeOnly.toString());
    });
}