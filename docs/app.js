const componentsEl = document.getElementById("components");
const cyclePanelEl = document.getElementById("cycle-panel");
const cycleClearEl = document.getElementById("cycle-clear");
const cycleDetailEl = document.getElementById("cycle-detail");
const recentEventsEl = document.getElementById("recent-events");
const recentEventsPanel = document.getElementById("recent-events-panel");
const statusTooltipEl = document.getElementById("status-tooltip");
const bannerEl = document.getElementById("banner");
const bannerIconEl = document.getElementById("banner-icon");
const bannerTitleEl = document.getElementById("banner-title");
const bannerTextEl = document.getElementById("banner-text");
const updatedAtEl = document.getElementById("updated-at");
const bucketRangeEl = document.getElementById("bucket-range");

const TIMELINE_WINDOW_HOURS = 24;
const TIMELINE_BUCKET_MINUTES = 15;
const TIMELINE_BUCKET_MS = TIMELINE_BUCKET_MINUTES * 60 * 1000;
const TIMELINE_WINDOW_MS = TIMELINE_WINDOW_HOURS * 60 * 60 * 1000;
const TIMELINE_BUCKET_COUNT = TIMELINE_WINDOW_MS / TIMELINE_BUCKET_MS;
const RECENT_EVENTS_LIMIT = 10;
const UI_STATE_KEY = "ebs-netwatch-ui-state-v1";
const API_STATUS_URL = "api/status";
const RAW_MANIFEST_URL = "data/manifest.json";
const COMPONENT_IDS = ["web", "network", "dns", "gateway"];
const initialUiState = loadUiState();

function defaultUiState() {
  return {
    expandedComponentIds: Object.fromEntries(COMPONENT_IDS.map((id) => [id, false])),
    recentEventsOpen: false,
  };
}

function loadUiState() {
  const fallback = defaultUiState();
  try {
    const raw = localStorage.getItem(UI_STATE_KEY);
    if (!raw) return fallback;
    const parsed = JSON.parse(raw);
    return {
      expandedComponentIds: {
        ...fallback.expandedComponentIds,
        ...(parsed.expandedComponentIds || {}),
      },
      recentEventsOpen: Boolean(parsed.recentEventsOpen),
    };
  } catch {
    return fallback;
  }
}

function persistUiState() {
  try {
    localStorage.setItem(UI_STATE_KEY, JSON.stringify({
      expandedComponentIds: state.expandedComponentIds,
      recentEventsOpen: state.recentEventsOpen,
    }));
  } catch {
    // Ignore storage failures.
  }
}

const state = {
  selectedCycleAt: null,
  payload: null,
  expandedComponentIds: initialUiState.expandedComponentIds,
  recentEventsOpen: initialUiState.recentEventsOpen,
};

const tooltipState = {
  anchor: null,
};

function formatTime(value) {
  if (!value) return "n/a";
  return new Date(value).toLocaleString();
}

function formatShortTime(value) {
  if (!value) return "n/a";
  return new Date(value).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
}

function escapeHtml(value) {
  return String(value)
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#39;");
}

function humanStatus(status) {
  switch (status) {
    case "operational":
      return "Fully operational";
    case "degraded":
      return "Degraded performance";
    case "major_outage_candidate":
      return "Major outage detected";
    default:
      return "No recent data";
  }
}

function statusClass(status) {
  switch (status) {
    case "operational":
      return "status-operational";
    case "degraded":
      return "status-degraded";
    case "major_outage_candidate":
      return "status-major";
    case "local_issue":
      return "status-local";
    default:
      return "status-unknown";
  }
}

function iconForStatus(status) {
  switch (status) {
    case "operational":
      return "✓";
    case "degraded":
      return "!";
    case "major_outage_candidate":
      return "×";
    case "local_issue":
      return "●";
    default:
      return "?";
  }
}

function formatEndpointSummary(cycle) {
  if (!cycle) return "No data";
  const healthyCount = cycle.endpoints.filter((endpoint) => endpoint.ok).length;
  return `${healthyCount}/${cycle.endpoints.length} healthy`;
}

function bucketLabel(cycles, index) {
  const cycle = cycles[index];
  if (!cycle) return "No data";
  const current = new Date(cycle.checkedAt);
  const next = cycles[index + 1] ? new Date(cycles[index + 1].checkedAt) : null;
  if (next) {
    return `${current.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })} - ${next.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })}`;
  }
  return `Checked at ${formatTime(cycle.checkedAt)}`;
}

function formatBucketTime(value) {
  return new Date(value).toLocaleString([], {
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  });
}

function formatBucketRange(start, end) {
  return `${formatBucketTime(start)} - ${formatBucketTime(end)}`;
}

function alignTimeToBucket(value) {
  return Math.ceil(value.getTime() / TIMELINE_BUCKET_MS) * TIMELINE_BUCKET_MS;
}

function sortCycles(cycles) {
  return (Array.isArray(cycles) ? cycles : [])
    .filter((cycle) => cycle && cycle.checkedAt)
    .slice()
    .sort((a, b) => new Date(a.checkedAt) - new Date(b.checkedAt));
}

function mergeCycles(...cycleGroups) {
  const byCheckedAt = new Map();
  for (const group of cycleGroups) {
    for (const cycle of group || []) {
      if (!cycle || !cycle.checkedAt) continue;
      byCheckedAt.set(cycle.checkedAt, cycle);
    }
  }
  return sortCycles(Array.from(byCheckedAt.values()));
}

function componentUptimeText(cycles, kind) {
  let healthyChecks = 0;
  let totalChecks = 0;

  for (const cycle of cycles) {
    const state = rowStateForCycle(cycle, kind);
    totalChecks++;
    if (state.status === "operational") {
      healthyChecks++;
    }
  }

  if (totalChecks === 0) {
    return "n/a uptime";
  }

  const uptime = (healthyChecks / totalChecks) * 100;
  return `${uptime.toFixed(2)}% uptime`;
}

function bucketStatusFromCounts(healthyChecks, totalChecks) {
  if (totalChecks === 0) {
    return "unknown";
  }
  if (healthyChecks === totalChecks) {
    return "operational";
  }
  if ((healthyChecks / totalChecks) >= 0.5) {
    return "degraded";
  }
  return "major_outage_candidate";
}

function collectFailedEndpointNames(cycles) {
  const failed = [];
  const seen = new Set();

  for (const cycle of cycles) {
    for (const endpoint of cycle.endpoints || []) {
      if (endpoint.ok || seen.has(endpoint.name)) continue;
      seen.add(endpoint.name);
      failed.push(endpoint.name);
    }
  }

  return failed;
}

function bucketSummaryMessage(status) {
  switch (status) {
    case "operational":
      return "All checks were healthy.";
    case "degraded":
      return "Some checks reported degraded behavior.";
    case "major_outage_candidate":
      return "Configured endpoints were unavailable.";
    default:
      return "No check data available for this period.";
  }
}

function buildTimeline(cycles, kind) {
  const windowEnd = new Date(alignTimeToBucket(new Date()));
  const windowStart = new Date(windowEnd.getTime() - TIMELINE_WINDOW_MS);
  const buckets = [];
  const sorted = sortCycles(cycles);
  const windowCycles = [];

  for (let index = 0; index < TIMELINE_BUCKET_COUNT; index++) {
    const start = new Date(windowStart.getTime() + (index * TIMELINE_BUCKET_MS));
    const end = new Date(start.getTime() + TIMELINE_BUCKET_MS);
    buckets.push({
      key: start.toISOString(),
      start,
      end,
      cycles: [],
      healthyChecks: 0,
      totalChecks: 0,
      status: "unknown",
      latestCycle: null,
      failedEndpoints: [],
    });
  }

  for (const cycle of sorted) {
    const checkedAt = new Date(cycle.checkedAt);
    if (Number.isNaN(checkedAt.getTime()) || checkedAt < windowStart || checkedAt >= windowEnd) {
      continue;
    }

    const index = Math.floor((checkedAt.getTime() - windowStart.getTime()) / TIMELINE_BUCKET_MS);
    const bucket = buckets[index];
    if (!bucket) {
      continue;
    }

    bucket.cycles.push(cycle);
    windowCycles.push(cycle);
  }

  for (const bucket of buckets) {
    const states = bucket.cycles.map((cycle) => rowStateForCycle(cycle, kind));
    bucket.totalChecks = bucket.cycles.length;
    bucket.healthyChecks = states.filter((state) => state.status === "operational").length;
    bucket.status = bucketStatusFromCounts(bucket.healthyChecks, bucket.totalChecks);
    bucket.latestCycle = bucket.cycles[bucket.cycles.length - 1] || null;
    bucket.failedEndpoints = collectFailedEndpointNames(bucket.cycles.filter((cycle, index) => states[index]?.status !== "operational"));
  }

  return {
    windowStart,
    windowEnd,
    windowCycles,
    buckets,
  };
}

function bannerCopy(snapshot, recent) {
  if (!recent.length) {
    return { kind: "unknown", title: "No recent data", text: "No checks have completed yet." };
  }

  switch (snapshot.status) {
    case "operational":
      return { kind: "operational", title: "Fully operational", text: "All configured checks are currently healthy." };
    case "degraded":
      return { kind: "degraded", title: "Degraded performance", text: "One endpoint is unhealthy, but the service is still mostly available." };
    case "major_outage_candidate":
      return { kind: "major", title: "Major outage detected", text: "Two or more checks are unhealthy, which suggests a broader issue." };
    default:
      return { kind: "unknown", title: "No recent data", text: "No checks have completed yet." };
  }
}

function cycleSummary(cycle) {
  const healthyCount = cycle.endpoints.filter((endpoint) => endpoint.ok).length;
  const failedNames = cycle.endpoints.filter((endpoint) => !endpoint.ok).map((endpoint) => endpoint.name);
  return {
    healthyCount,
    failedText: failedNames.length ? failedNames.join(", ") : "none",
  };
}

function rowStateForCycle(cycle, kind) {
  if (!cycle) return { status: "unknown", label: "No data" };

  const diag = cycle.diagnostics || {};
  const gatewayReachable = diag.gatewayReachable;
  const dnsLookups = Array.isArray(diag.dnsLookups) ? diag.dnsLookups : [];
  const dnsFailed = dnsLookups.some((lookup) => lookup.error);
  const okCount = cycle.endpoints.filter((endpoint) => endpoint.ok).length;
  const endpointCount = cycle.endpoints.length;

  if (kind === "web") {
    if (endpointCount === 0) return { status: "unknown", label: "No data" };
    if (okCount === endpointCount) return { status: "operational", label: `${okCount}/${endpointCount} healthy` };
    if (okCount >= endpointCount - 1) return { status: "degraded", label: `${okCount}/${endpointCount} healthy` };
    return { status: "major_outage_candidate", label: `${okCount}/${endpointCount} healthy` };
  }

  if (kind === "network") {
    if (!diag || (!diag.defaultInterface && !diag.gateway)) {
      return cycle.status === "operational" ? { status: "operational", label: "Route healthy" } : { status: "unknown", label: "No diagnostics" };
    }
    if (gatewayReachable === false) return { status: "local_issue", label: "Gateway unreachable" };
    return { status: "operational", label: diag.connectionType ? `${diag.connectionType} path` : "Path checked" };
  }

  if (kind === "dns") {
    if (!diag || !dnsLookups.length) {
      return cycle.status === "operational" ? { status: "operational", label: "No issues observed" } : { status: "unknown", label: "No diagnostics" };
    }
    if (dnsFailed) return { status: "local_issue", label: "DNS lookup failure" };
    return { status: "operational", label: `${dnsLookups.length} host${dnsLookups.length === 1 ? "" : "s"} resolved` };
  }

  if (kind === "gateway") {
    if (!diag || !diag.gateway) {
      return cycle.status === "operational" ? { status: "operational", label: "No issues observed" } : { status: "unknown", label: "No diagnostics" };
    }
    if (gatewayReachable === false) return { status: "local_issue", label: "Gateway unreachable" };
    return { status: "operational", label: "Gateway reachable" };
  }

  return { status: cycle.status, label: "Checked" };
}

function bucketTooltipData(bucket) {
  const countsText = bucket.totalChecks > 0
    ? `Healthy checks: ${bucket.healthyChecks}/${bucket.totalChecks}`
    : "Healthy checks: 0/0";

  return {
    status: bucket.status,
    timeRange: formatBucketRange(bucket.start, bucket.end),
    icon: iconForStatus(bucket.status),
    message: bucketSummaryMessage(bucket.status),
    countsText,
    extra: bucket.failedEndpoints.length ? `Failed endpoints: ${bucket.failedEndpoints.join(", ")}` : "",
  };
}

function renderStatusBlocks(timeline, kind) {
  if (!timeline.buckets.length) return `<div class="history-empty">No recent data</div>`;

  return timeline.buckets.map((bucket) => {
    const tooltip = bucketTooltipData(bucket);
    const latestCycle = bucket.latestCycle;
    const selected = latestCycle && state.selectedCycleAt === latestCycle.checkedAt;
    const cycleAtAttr = latestCycle ? `data-cycle-at="${escapeHtml(latestCycle.checkedAt)}"` : "";
    const cycleLabel = latestCycle
      ? `${kind} ${formatBucketRange(bucket.start, bucket.end)} ${bucket.healthyChecks}/${bucket.totalChecks} checks healthy`
      : `${kind} ${formatBucketRange(bucket.start, bucket.end)} No check data available`;

    return `
      <button
        type="button"
        class="history-block ${statusClass(bucket.status)} ${selected ? "is-selected" : ""} ${bucket.totalChecks === 0 ? "is-empty" : ""}"
        data-bucket-key="${escapeHtml(bucket.key)}"
        ${cycleAtAttr}
        data-tooltip-status="${escapeHtml(tooltip.status)}"
        data-tooltip-time-range="${escapeHtml(tooltip.timeRange)}"
        data-tooltip-message="${escapeHtml(tooltip.message)}"
        data-tooltip-counts="${escapeHtml(tooltip.countsText)}"
        data-tooltip-extra="${escapeHtml(tooltip.extra)}"
        data-tooltip-icon="${escapeHtml(tooltip.icon)}"
        aria-label="${escapeHtml(cycleLabel)}"
      ></button>
    `;
  }).join("");
}

function tooltipMarkup(data) {
  const extra = data.extra ? `<div class="tooltip-extra">${escapeHtml(data.extra)}</div>` : "";
  return `
    <div class="tooltip-date">
      <span>${escapeHtml(data.timeRange)}</span>
      <span class="tooltip-status ${statusClass(data.status)}">${escapeHtml(humanStatus(data.status))}</span>
    </div>
    <div class="tooltip-body">
      <span class="tooltip-icon ${statusClass(data.status)}" aria-hidden="true">${escapeHtml(data.icon)}</span>
      <div class="tooltip-text">
        <div class="tooltip-message">${escapeHtml(data.message)}</div>
        <div class="tooltip-counts">${escapeHtml(data.countsText)}</div>
        ${extra}
      </div>
    </div>
  `;
}

function clamp(value, min, max) {
  return Math.min(Math.max(value, min), max);
}

function hideTooltip() {
  if (!statusTooltipEl) {
    return;
  }
  statusTooltipEl.classList.remove("visible");
  statusTooltipEl.setAttribute("aria-hidden", "true");
  statusTooltipEl.innerHTML = "";
  if (tooltipState.anchor) {
    tooltipState.anchor.removeAttribute("aria-describedby");
  }
  tooltipState.anchor = null;
}

function positionTooltip(anchor) {
  if (!statusTooltipEl || !anchor) {
    return;
  }

  const margin = 12;
  const gap = 10;
  const anchorRect = anchor.getBoundingClientRect();
  const tooltipRect = statusTooltipEl.getBoundingClientRect();
  const viewportWidth = window.innerWidth;
  const viewportHeight = window.innerHeight;

  let left = anchorRect.left + (anchorRect.width / 2) - (tooltipRect.width / 2);
  left = clamp(left, margin, Math.max(margin, viewportWidth - tooltipRect.width - margin));

  let top = anchorRect.bottom + gap;
  let placement = "bottom";
  if (top + tooltipRect.height > viewportHeight - margin) {
    const above = anchorRect.top - tooltipRect.height - gap;
    if (above >= margin) {
      top = above;
      placement = "top";
    } else {
      top = clamp(top, margin, Math.max(margin, viewportHeight - tooltipRect.height - margin));
    }
  }

  statusTooltipEl.style.left = `${left}px`;
  statusTooltipEl.style.top = `${top}px`;
  statusTooltipEl.dataset.placement = placement;
}

function showTooltip(anchor) {
  if (!statusTooltipEl || !anchor) {
    return;
  }

  const status = anchor.getAttribute("data-tooltip-status") || "unknown";
  const timeRange = anchor.getAttribute("data-tooltip-time-range") || "No data";
  const message = anchor.getAttribute("data-tooltip-message") || "No check data available for this period.";
  const countsText = anchor.getAttribute("data-tooltip-counts") || "Healthy checks: 0/0";
  const extra = anchor.getAttribute("data-tooltip-extra") || "";
  const icon = anchor.getAttribute("data-tooltip-icon") || "?";

  tooltipState.anchor = anchor;
  statusTooltipEl.innerHTML = tooltipMarkup({
    status,
    timeRange,
    message,
    countsText,
    extra,
    icon,
  });
  statusTooltipEl.className = `status-tooltip visible ${statusClass(status)}`;
  statusTooltipEl.setAttribute("aria-hidden", "false");
  anchor.setAttribute("aria-describedby", "status-tooltip");
  statusTooltipEl.style.left = "0px";
  statusTooltipEl.style.top = "0px";
  requestAnimationFrame(() => {
    if (tooltipState.anchor === anchor) {
      positionTooltip(anchor);
    }
  });
}

function restoreTooltipAfterRender() {
  if (!tooltipState.anchor) {
    return;
  }

  const bucketKey = tooltipState.anchor.getAttribute("data-bucket-key");
  if (!bucketKey) {
    hideTooltip();
    return;
  }

  const nextAnchor = Array.from(document.querySelectorAll("[data-bucket-key]")).find((button) => button.getAttribute("data-bucket-key") === bucketKey);
  if (!nextAnchor) {
    hideTooltip();
    return;
  }

  showTooltip(nextAnchor);
}

function endpointRows(cycle) {
  if (!cycle || !cycle.endpoints.length) {
    return `<div class="empty-state small"><strong>No endpoint data</strong><p>No endpoint checks were available for this cycle.</p></div>`;
  }

  return cycle.endpoints.map((endpoint) => `
    <div class="endpoint-row">
      <div class="endpoint-left">
        <div class="endpoint-name">${escapeHtml(endpoint.name)}</div>
        <div class="endpoint-url" title="${escapeHtml(endpoint.url)}">${escapeHtml(endpoint.url)}</div>
      </div>
      <div class="endpoint-right">
        <span class="endpoint-ok ${endpoint.ok ? "is-ok" : "is-fail"}">${endpoint.ok ? "true" : "false"}</span>
        <span class="endpoint-meta">HTTP ${endpoint.statusCode == null ? "n/a" : escapeHtml(endpoint.statusCode)}</span>
        <span class="endpoint-meta">Phase ${escapeHtml(endpoint.failurePhase || "n/a")}</span>
        <span class="endpoint-meta">Duration ${escapeHtml(endpoint.durationMs)} ms</span>
        ${endpoint.error ? `<span class="endpoint-error">${escapeHtml(endpoint.error)}</span>` : ""}
      </div>
    </div>
  `).join("");
}

function networkTiles(cycle) {
  const diag = cycle?.diagnostics;
  if (!diag) {
    return `<div class="empty-state small"><strong>No diagnostics</strong><p>Network diagnostics only appear after an abnormal cycle.</p></div>`;
  }

  return `
    <div class="mini-grid">
      <div class="mini-tile"><span class="detail-label">Interface</span><strong>${escapeHtml(diag.defaultInterface || "n/a")}</strong></div>
      <div class="mini-tile"><span class="detail-label">Connection type</span><strong>${escapeHtml(diag.connectionType || "n/a")}</strong></div>
      <div class="mini-tile"><span class="detail-label">Gateway</span><strong>${escapeHtml(diag.gateway || "n/a")}</strong></div>
      <div class="mini-tile"><span class="detail-label">Gateway probe</span><strong>${escapeHtml(diag.gatewayProbe || "n/a")}</strong></div>
      <div class="mini-tile"><span class="detail-label">Gateway reachable</span><strong>${diag.gatewayReachable ? "true" : "false"}</strong></div>
    </div>
  `;
}

function dnsList(cycle) {
  const diag = cycle?.diagnostics;
  const lookups = Array.isArray(diag?.dnsLookups) ? diag.dnsLookups : [];
  if (!lookups.length) {
    return `<div class="empty-state small"><strong>No DNS diagnostics</strong><p>No DNS data was collected for this cycle.</p></div>`;
  }

  return `
    <ul class="dns-list">
      ${lookups.map((lookup) => `
        <li>
          <strong>${escapeHtml(lookup.host)}</strong>
          <span>${lookup.error ? escapeHtml(lookup.error) : `${lookup.addresses.length} addresses`}</span>
        </li>
      `).join("")}
    </ul>
  `;
}

function componentDetails(kind, cycle) {
  if (kind === "web") {
    return `
      <div class="expand-section">
        <div class="expand-head">
          <h3>Endpoint details</h3>
          <p>URLs stay on one line and reveal the full value on hover.</p>
        </div>
        <div class="endpoint-list">
          ${endpointRows(cycle)}
        </div>
      </div>
    `;
  }

  if (kind === "network") {
    return `
      <div class="expand-section">
        <div class="expand-head">
          <h3>Local route details</h3>
          <p>Interface and gateway data from the latest abnormal cycle.</p>
        </div>
        ${networkTiles(cycle)}
      </div>
    `;
  }

  if (kind === "dns") {
    return `
      <div class="expand-section">
        <div class="expand-head">
          <h3>DNS results</h3>
          <p>Lookup results for hosts touched during abnormal checks.</p>
        </div>
        ${dnsList(cycle)}
      </div>
    `;
  }

  if (kind === "gateway") {
    return `
      <div class="expand-section">
        <div class="expand-head">
          <h3>Gateway check</h3>
          <p>Probe result from the latest abnormal cycle.</p>
        </div>
        ${networkTiles(cycle)}
      </div>
    `;
  }

  return "";
}

function renderComponentRows(snapshot, recent) {
  const rows = [
    {
      kind: "web",
      name: "Web availability",
      meta: `${snapshot.endpoints.length} endpoints`,
      summary: (cycle) => formatEndpointSummary(cycle),
      note: () => "Configured endpoint checks",
    },
    {
      kind: "network",
      name: "Network path",
      meta: snapshot.interface.connectionType ? `${snapshot.interface.connectionType} · ${snapshot.interface.defaultInterface || "unknown interface"}` : "Local path",
      summary: (cycle) => {
        if (!cycle) return "No data";
        const diag = cycle.diagnostics;
        if (!diag) return cycle.status === "operational" ? "Healthy" : "No diagnostics";
        return diag.gatewayReachable ? "Gateway reachable" : "Gateway issue";
      },
      note: (cycle) => (!cycle ? "No data yet" : cycle.status === "operational" ? "Healthy on the latest cycle" : "Checked on abnormal cycles"),
    },
    {
      kind: "dns",
      name: "DNS diagnostics",
      meta: "Checked on failure",
      summary: (cycle) => {
        if (!cycle) return "No data";
        const diag = cycle.diagnostics;
        if (!diag || !Array.isArray(diag.dnsLookups) || !diag.dnsLookups.length) return cycle.status === "operational" ? "Healthy" : "No diagnostics";
        return diag.dnsLookups.some((lookup) => lookup.error) ? "DNS issue" : "DNS ok";
      },
      note: (cycle) => (!cycle ? "No data yet" : cycle.status === "operational" ? "Healthy on the latest cycle" : "Available on abnormal cycles"),
    },
    {
      kind: "gateway",
      name: "Local gateway",
      meta: "Gateway reachability",
      summary: (cycle) => {
        if (!cycle) return "No data";
        const diag = cycle.diagnostics;
        if (!diag || !diag.gateway) return cycle.status === "operational" ? "Healthy" : "No diagnostics";
        return diag.gatewayReachable ? "Reachable" : "Unreachable";
      },
      note: (cycle) => (!cycle ? "No data yet" : cycle.status === "operational" ? "Healthy on the latest cycle" : "Available on abnormal cycles"),
    },
  ];

  return rows.map((row) => {
    const timeline = buildTimeline(recent, row.kind);
    const latest = recent[recent.length - 1];
    const rowState = rowStateForCycle(latest, row.kind);
    const open = state.expandedComponentIds[row.kind];
    const detailHtml = componentDetails(row.kind, latest);
    return `
      <details class="component-group" data-component-id="${escapeHtml(row.kind)}" ${open ? "open" : ""}>
        <summary class="component-row">
          <div class="component-row-top">
            <div class="component-main">
              <div class="component-icon ${statusClass(rowState.status)}">${iconForStatus(rowState.status)}</div>
              <div class="component-namewrap">
                <div class="component-name">${escapeHtml(row.name)}</div>
                <div class="component-meta">${escapeHtml(row.meta)}</div>
              </div>
            </div>
            <div class="component-summary">
              <div class="component-summary-value">${escapeHtml(componentUptimeText(timeline.windowCycles, row.kind))}</div>
              <div class="component-summary-note">Last 24 hours</div>
            </div>
            <div class="component-toggle" aria-hidden="true">⌄</div>
          </div>
          <div class="component-history" aria-label="${escapeHtml(row.name)} recent status blocks">
            ${renderStatusBlocks(timeline, row.kind)}
          </div>
        </summary>
        <div class="component-expanded">
          ${detailHtml}
        </div>
      </details>
    `;
  }).join("");
}

function renderCycleDetail(cycle, recent) {
  if (!cycle) {
    return "";
  }

  const summary = cycleSummary(cycle);
  const diag = cycle.diagnostics;
  const dnsLookups = Array.isArray(diag?.dnsLookups) ? diag.dnsLookups : [];
  const historyIndex = recent.findIndex((item) => item.checkedAt === cycle.checkedAt);
  const historyLabel = bucketLabel(recent, historyIndex);

  return `
    <div class="detail-grid">
      <div class="detail-tile">
        <span class="detail-label">Time range</span>
        <strong>${escapeHtml(historyLabel)}</strong>
      </div>
      <div class="detail-tile">
        <span class="detail-label">Overall status</span>
        <strong>${escapeHtml(humanStatus(cycle.status))}</strong>
      </div>
      <div class="detail-tile">
        <span class="detail-label">Healthy endpoints</span>
        <strong>${escapeHtml(summary.healthyCount)}/${escapeHtml(cycle.endpoints.length)}</strong>
      </div>
      <div class="detail-tile">
        <span class="detail-label">Failed endpoint names</span>
        <strong>${escapeHtml(summary.failedText)}</strong>
      </div>
    </div>

    <div class="detail-section">
      <div class="detail-section-head">
        <h3>Endpoint details</h3>
        <p>URLs are truncated visually but remain available via hover.</p>
      </div>
      <div class="endpoint-list">
        ${endpointRows(cycle)}
      </div>
    </div>

    <div class="detail-section detail-secondary">
      <div class="detail-section-head">
        <h3>Gateway and DNS</h3>
        <p>Local diagnostics collected on abnormal cycles.</p>
      </div>
      <div class="diagnostic-grid">
        <div class="diagnostic-block">
          <span class="detail-label">Gateway result</span>
          <strong>${escapeHtml(diag && diag.gateway ? `${diag.gateway} · ${diag.gatewayReachable ? "reachable" : "unreachable"}` : "No diagnostics")}</strong>
          <p>${escapeHtml(diag && diag.gatewayProbe ? diag.gatewayProbe : "n/a")}</p>
        </div>
        <div class="diagnostic-block">
          <span class="detail-label">DNS result</span>
          <ul class="dns-list">
            ${dnsLookups.length
              ? dnsLookups.map((lookup) => `
                  <li>
                    <strong>${escapeHtml(lookup.host)}</strong>
                    <span>${lookup.error ? escapeHtml(lookup.error) : `${lookup.addresses.length} addresses`}</span>
                  </li>
                `).join("")
              : `<li><strong>No DNS diagnostics</strong><span>No DNS data was collected for this cycle.</span></li>`}
          </ul>
        </div>
      </div>
    </div>
  `;
}

function renderRecentEvents(recent) {
  if (!recent.length) {
    return `<div class="empty-state"><strong>No recent events</strong><p>The first monitoring cycle has not completed yet.</p></div>`;
  }

  return recent.slice().reverse().map((cycle) => `
    <button type="button" class="event-item" data-cycle-select="${escapeHtml(cycle.checkedAt)}">
      <span class="event-status ${statusClass(cycle.status)}">${escapeHtml(humanStatus(cycle.status))}</span>
      <span class="event-time">${formatTime(cycle.checkedAt)}</span>
      <span class="event-summary">${escapeHtml(formatEndpointSummary(cycle))}</span>
    </button>
  `).join("");
}

async function fetchJson(url) {
  const response = await fetch(url, { cache: "no-store" });
  if (!response.ok) {
    throw new Error(`Request failed for ${url}: ${response.status}`);
  }
  return response.json();
}

async function loadStaticPayload() {
  const manifest = await fetchJson(RAW_MANIFEST_URL);
  const files = Array.isArray(manifest.files) ? manifest.files : [];
  const cycles = [];

  for (const file of files) {
    const path = typeof file === "string" ? file : file.path;
    if (!path) continue;
    const response = await fetch(path, { cache: "no-store" });
    if (!response.ok) {
      throw new Error(`Request failed for ${path}: ${response.status}`);
    }
    const text = await response.text();
    text.split(/\r?\n/).forEach((line) => {
      const trimmed = line.trim();
      if (!trimmed) return;
      try {
        cycles.push(JSON.parse(trimmed));
      } catch {
        // Ignore malformed raw log lines in static mode.
      }
    });
  }

  cycles.sort((a, b) => new Date(a.checkedAt) - new Date(b.checkedAt));
  const latest = cycles[cycles.length - 1] || null;
  return {
    generatedAt: manifest.generatedAt || latest?.checkedAt || new Date().toISOString(),
    snapshot: {
      updatedAt: latest?.checkedAt || manifest.generatedAt || null,
      status: latest?.status || "unknown",
      interface: latest?.interface || {},
      endpoints: latest?.endpoints || [],
      diagnostics: latest?.diagnostics || null,
      recent: cycles,
    },
  };
}

function mergePayloads(primary, secondary) {
  const primarySnapshot = primary?.snapshot || {};
  const secondarySnapshot = secondary?.snapshot || {};
  const recent = mergeCycles(primarySnapshot.recent, secondarySnapshot.recent);
  const latest = recent[recent.length - 1] || null;

  return {
    generatedAt: primary?.generatedAt || secondary?.generatedAt || latest?.checkedAt || new Date().toISOString(),
    snapshot: {
      ...secondarySnapshot,
      ...primarySnapshot,
      updatedAt: latest?.checkedAt || primarySnapshot.updatedAt || secondarySnapshot.updatedAt || null,
      status: latest?.status || primarySnapshot.status || secondarySnapshot.status || "unknown",
      interface: latest?.interface || primarySnapshot.interface || secondarySnapshot.interface || {},
      endpoints: latest?.endpoints || primarySnapshot.endpoints || secondarySnapshot.endpoints || [],
      diagnostics: latest?.diagnostics ?? primarySnapshot.diagnostics ?? secondarySnapshot.diagnostics ?? null,
      recent,
    },
  };
}

async function loadDashboardPayload() {
  const apiResult = await fetchJson(API_STATUS_URL)
    .then((payload) => ({ ok: true, payload }))
    .catch((error) => ({ ok: false, error }));
  const staticResult = await loadStaticPayload()
    .then((payload) => ({ ok: true, payload }))
    .catch((error) => ({ ok: false, error }));

  if (apiResult.ok && staticResult.ok) {
    return mergePayloads(apiResult.payload, staticResult.payload);
  }
  if (apiResult.ok) {
    return apiResult.payload;
  }
  if (staticResult.ok) {
    return staticResult.payload;
  }

  throw new Error(`Unable to load dashboard data: ${staticResult.error.message || apiResult.error.message}`);
}

function applyBanner(snapshot, recent) {
  const banner = bannerCopy(snapshot, recent);
  bannerEl.className = `banner banner-${banner.kind}`;
  bannerIconEl.className = `banner-icon ${statusClass(snapshot.status)}`;
  bannerIconEl.textContent = recent.length ? iconForStatus(snapshot.status) : "•";
  bannerTitleEl.textContent = banner.title;
  bannerTextEl.textContent = banner.text;
}

function applyHeader(snapshot, recent) {
  if (!recent.length || !snapshot.updatedAt) {
    updatedAtEl.textContent = "Last updated n/a";
    return;
  }
  updatedAtEl.textContent = `Last updated ${formatTime(snapshot.updatedAt)}`;
}

function resolveSelectedCycle(recent) {
  if (!state.selectedCycleAt) {
    return null;
  }
  const existing = recent.find((cycle) => cycle.checkedAt === state.selectedCycleAt);
  if (!existing) {
    state.selectedCycleAt = null;
    return null;
  }
  return existing;
}

function wireInteractions() {
  document.querySelectorAll("[data-component-id]").forEach((group) => {
    group.addEventListener("toggle", () => {
      const id = group.getAttribute("data-component-id");
      if (!id) return;
      state.expandedComponentIds[id] = group.open;
      persistUiState();
    });
  });

  document.querySelectorAll("[data-bucket-key]").forEach((button) => {
    button.addEventListener("pointerenter", () => {
      showTooltip(button);
    });
    button.addEventListener("pointerleave", () => {
      hideTooltip();
    });
    button.addEventListener("focus", () => {
      showTooltip(button);
    });
    button.addEventListener("blur", () => {
      hideTooltip();
    });
    button.addEventListener("click", (event) => {
      event.preventDefault();
      event.stopPropagation();
      const cycleAt = button.getAttribute("data-cycle-at");
      if (!cycleAt) {
        return;
      }
      state.selectedCycleAt = cycleAt;
      render(state.payload);
    });
  });

  document.querySelectorAll("[data-cycle-select]").forEach((button) => {
    button.addEventListener("click", (event) => {
      event.preventDefault();
      state.selectedCycleAt = button.getAttribute("data-cycle-select");
      render(state.payload);
    });
  });
}

function render(payload) {
  const snapshot = payload?.snapshot || {};
  const allRecent = sortCycles(Array.isArray(snapshot.recent) ? snapshot.recent : []);
  const visibleForEvents = allRecent.slice(-RECENT_EVENTS_LIMIT);
  const selectedCycle = resolveSelectedCycle(allRecent);
  const timeline = buildTimeline(allRecent, "web");

  applyHeader(snapshot, allRecent);
  applyBanner(snapshot, allRecent);
  bucketRangeEl.textContent = `${formatBucketRange(timeline.windowStart, timeline.windowEnd)}`;
  componentsEl.innerHTML = renderComponentRows(snapshot, allRecent);

  if (selectedCycle) {
    cyclePanelEl.hidden = false;
    cycleDetailEl.innerHTML = renderCycleDetail(selectedCycle, allRecent);
  } else {
    cyclePanelEl.hidden = true;
    cycleDetailEl.innerHTML = "";
  }

  recentEventsPanel.open = state.recentEventsOpen;
  recentEventsEl.innerHTML = renderRecentEvents(visibleForEvents);
  wireInteractions();
  restoreTooltipAfterRender();
}

async function refresh() {
  const payload = await loadDashboardPayload();
  state.payload = payload;
  render(payload);
}

cycleClearEl.addEventListener("click", () => {
  state.selectedCycleAt = null;
  render(state.payload);
});

recentEventsPanel.addEventListener("toggle", () => {
  state.recentEventsOpen = recentEventsPanel.open;
  persistUiState();
});

document.addEventListener("keydown", (event) => {
  if (event.key === "Escape") {
    hideTooltip();
  }
});

window.addEventListener("scroll", () => {
  if (tooltipState.anchor) {
    positionTooltip(tooltipState.anchor);
  }
}, true);

window.addEventListener("resize", () => {
  if (tooltipState.anchor) {
    positionTooltip(tooltipState.anchor);
  }
});

refresh().catch((error) => {
  bannerEl.className = "banner banner-unknown";
  bannerIconEl.className = "banner-icon status-unknown";
  bannerIconEl.textContent = "?";
  bannerTitleEl.textContent = "No recent data";
  bannerTextEl.textContent = error.message;
  updatedAtEl.textContent = "Last updated n/a";
  bucketRangeEl.textContent = "No data yet";
  componentsEl.innerHTML = `<div class="empty-state"><strong>Unable to load dashboard</strong><p>${escapeHtml(error.message)}</p></div>`;
  cyclePanelEl.hidden = true;
  cycleDetailEl.innerHTML = "";
  recentEventsEl.innerHTML = `<div class="empty-state"><strong>No events available</strong><p>${escapeHtml(error.message)}</p></div>`;
});

setInterval(() => {
  refresh().catch(() => {});
}, 5000);
