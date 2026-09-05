/**
 * Relay Campaign Events Real-Time Dashboard Client
 */

let activeCampaign = "";
let currentEventType = "";
let currentOffset = 0;
const pageLimit = 15;
let pollingTimer = null;

// Preset Payloads for Webhook Simulator
const PRESETS = {
  valid: [
    {
      "event_id": "evt_live_" + Math.floor(Math.random() * 90000 + 10000),
      "campaign_id": "cmp_summer_sale",
      "contact_id": "ct_042",
      "type": "sent",
      "timestamp": new Date().toISOString()
    },
    {
      "event_id": "evt_live_" + Math.floor(Math.random() * 90000 + 10000),
      "campaign_id": "cmp_summer_sale",
      "contact_id": "ct_042",
      "type": "delivered",
      "timestamp": new Date(Date.now() + 5000).toISOString()
    },
    {
      "event_id": "evt_live_" + Math.floor(Math.random() * 90000 + 10000),
      "campaign_id": "cmp_summer_sale",
      "contact_id": "ct_042",
      "type": "opened",
      "timestamp": new Date(Date.now() + 15000).toISOString()
    }
  ],
  duplicates: [
    {
      "event_id": "evt_dup_99999",
      "campaign_id": "cmp_summer_sale",
      "contact_id": "ct_100",
      "type": "sent",
      "timestamp": new Date().toISOString()
    },
    {
      "event_id": "evt_dup_99999",
      "campaign_id": "cmp_summer_sale",
      "contact_id": "ct_100",
      "type": "sent",
      "timestamp": new Date().toISOString()
    }
  ],
  malformed: [
    {
      "event_id": "evt_good_01",
      "campaign_id": "cmp_summer_sale",
      "contact_id": "ct_099",
      "type": "opened",
      "timestamp": new Date().toISOString()
    },
    {
      "event_id": "",
      "campaign_id": "cmp_summer_sale",
      "type": "invalid_type",
      "timestamp": "bad_time"
    }
  ],
  seedSample: [
    {"event_id":"evt_seed_01","campaign_id":"cmp_summer_sale","contact_id":"ct_001","type":"sent","timestamp":"2026-08-10T06:00:00Z"},
    {"event_id":"evt_seed_02","campaign_id":"cmp_summer_sale","contact_id":"ct_001","type":"delivered","timestamp":"2026-08-10T06:05:00Z"},
    {"event_id":"evt_seed_03","campaign_id":"cmp_summer_sale","contact_id":"ct_001","type":"opened","timestamp":"2026-08-10T07:15:00Z"},
    {"event_id":"evt_seed_04","campaign_id":"cmp_summer_sale","contact_id":"ct_001","type":"clicked","timestamp":"2026-08-10T07:20:00Z","metadata":{"url":"https://example.com/promo"}},
    {"event_id":"evt_seed_05","campaign_id":"cmp_summer_sale","contact_id":"ct_002","type":"sent","timestamp":"2026-08-10T06:00:00Z"},
    {"event_id":"evt_seed_06","campaign_id":"cmp_summer_sale","contact_id":"ct_002","type":"delivered","timestamp":"2026-08-10T06:06:00Z"},
    {"event_id":"evt_seed_07","campaign_id":"cmp_summer_sale","contact_id":"ct_002","type":"opened","timestamp":"2026-08-10T08:30:00Z"},
    {"event_id":"evt_seed_08","campaign_id":"cmp_product_launch","contact_id":"ct_002","type":"sent","timestamp":"2026-08-10T09:00:00Z"},
    {"event_id":"evt_seed_09","campaign_id":"cmp_product_launch","contact_id":"ct_002","type":"delivered","timestamp":"2026-08-10T09:02:00Z"},
    {"event_id":"evt_seed_10","campaign_id":"cmp_product_launch","contact_id":"ct_002","type":"opened","timestamp":"2026-08-10T09:10:00Z"}
  ]
};

document.addEventListener("DOMContentLoaded", () => {
  initEventListeners();
  loadCampaigns();
  startAutoRefresh();
});

function initEventListeners() {
  const select = document.getElementById("campaign-select");
  select.addEventListener("change", (e) => {
    activeCampaign = e.target.value;
    currentOffset = 0;
    refreshDashboard();
  });

  // Seed data buttons
  document.getElementById("btn-seed-data").addEventListener("click", handleIngestSeedData);
  document.getElementById("btn-seed-empty").addEventListener("click", handleIngestSeedData);

  // Webhook Simulator Modal triggers
  const modal = document.getElementById("sim-modal");
  const openModal = () => {
    modal.style.display = "flex";
    setPreset("valid");
  };
  const closeModal = () => {
    modal.style.display = "none";
    document.getElementById("sim-result-box").style.display = "none";
  };

  document.getElementById("btn-open-simulator").addEventListener("click", openModal);
  document.getElementById("btn-sim-empty").addEventListener("click", openModal);
  document.getElementById("btn-close-modal").addEventListener("click", closeModal);
  document.getElementById("btn-cancel-modal").addEventListener("click", closeModal);

  // Preset buttons
  document.getElementById("preset-valid").addEventListener("click", () => setPreset("valid"));
  document.getElementById("preset-duplicates").addEventListener("click", () => setPreset("duplicates"));
  document.getElementById("preset-malformed").addEventListener("click", () => setPreset("malformed"));
  document.getElementById("preset-seed").addEventListener("click", () => setPreset("seedSample"));

  // Submit Simulator Request
  document.getElementById("btn-submit-sim").addEventListener("click", handleSimulatorSubmit);

  // Event Type Filter Buttons
  document.querySelectorAll(".filter-btn").forEach(btn => {
    btn.addEventListener("click", (e) => {
      document.querySelectorAll(".filter-btn").forEach(b => b.classList.remove("active"));
      e.target.classList.add("active");
      currentEventType = e.target.dataset.type;
      currentOffset = 0;
      loadEvents();
    });
  });

  // Pagination
  document.getElementById("btn-prev-page").addEventListener("click", () => {
    if (currentOffset >= pageLimit) {
      currentOffset -= pageLimit;
      loadEvents();
    }
  });

  document.getElementById("btn-next-page").addEventListener("click", () => {
    currentOffset += pageLimit;
    loadEvents();
  });
}

function setPreset(name) {
  const payload = PRESETS[name] || PRESETS.valid;
  document.getElementById("sim-json-input").value = JSON.stringify(payload, null, 2);
}

// Fetch Campaigns
async function loadCampaigns() {
  try {
    const res = await fetch("/campaigns");
    if (!res.ok) throw new Error("Failed to load campaigns");
    const data = await res.json();
    const select = document.getElementById("campaign-select");
    
    select.innerHTML = "";
    if (data.campaigns && data.campaigns.length > 0) {
      data.campaigns.forEach(c => {
        const opt = document.createElement("option");
        opt.value = c;
        opt.textContent = c;
        select.appendChild(opt);
      });

      if (!activeCampaign || !data.campaigns.includes(activeCampaign)) {
        activeCampaign = data.campaigns[0];
      }
      select.value = activeCampaign;
      showDashboard(true);
      refreshDashboard();
    } else {
      showDashboard(false);
    }
  } catch (err) {
    console.error("loadCampaigns error:", err);
    showDashboard(false);
  }
}

function showDashboard(hasData) {
  document.getElementById("empty-state").style.display = hasData ? "none" : "block";
  document.getElementById("dashboard-view").style.display = hasData ? "block" : "none";
}

async function refreshDashboard() {
  if (!activeCampaign) return;
  document.getElementById("active-campaign-title").textContent = activeCampaign;
  await Promise.all([loadStats(), loadEvents()]);
}

// Load Campaign Statistics
async function loadStats() {
  if (!activeCampaign) return;
  try {
    const res = await fetch(`/campaigns/${encodeURIComponent(activeCampaign)}/stats`);
    if (!res.ok) return;
    const stats = await res.json();
    renderStats(stats);
  } catch (err) {
    console.error("loadStats error:", err);
  }
}

function renderStats(s) {
  // Volume Numbers
  document.getElementById("val-sent").textContent = s.sent.toLocaleString();
  document.getElementById("val-delivered").textContent = s.delivered.toLocaleString();
  document.getElementById("val-opened").textContent = s.opened.toLocaleString();
  document.getElementById("val-unique-opens").textContent = `(${s.unique_opens.toLocaleString()} Unique)`;
  document.getElementById("val-clicked").textContent = s.clicked.toLocaleString();
  document.getElementById("val-unique-clicks").textContent = `(${s.unique_clicks.toLocaleString()} Unique)`;

  // Rates in KPI Badges
  const delRatePct = (s.delivery_rate * 100).toFixed(1);
  const openRatePct = (s.open_rate * 100).toFixed(1);
  const ctrPct = (s.click_through_rate * 100).toFixed(1);

  document.getElementById("rate-delivered-badge").textContent = `${delRatePct}% Delivery Rate`;
  document.getElementById("rate-open-badge").textContent = `${openRatePct}% Open Rate`;
  document.getElementById("rate-ctr-badge").textContent = `${ctrPct}% CTR`;

  // Rates in Progress Bars
  renderRateBar("rate-delivery", s.delivery_rate);
  renderRateBar("rate-open", s.open_rate);
  renderRateBar("rate-unique-open", s.unique_open_rate);
  renderRateBar("rate-ctr", s.click_through_rate);
  renderRateBar("rate-ctor", s.click_to_open_rate);

  // Render Daily UTC Table
  renderDailyTable(s.daily_stats);
}

function renderRateBar(idPrefix, val) {
  const pct = Math.min(100, Math.max(0, val * 100));
  const textEl = document.getElementById(`${idPrefix}-text`);
  const barEl = document.getElementById(`${idPrefix}-bar`);
  if (textEl) textEl.textContent = `${(val * 100).toFixed(2)}%`;
  if (barEl) barEl.style.width = `${pct}%`;
}

function renderDailyTable(dailyStats) {
  const tbody = document.getElementById("daily-tbody");
  tbody.innerHTML = "";

  if (!dailyStats || Object.keys(dailyStats).length === 0) {
    tbody.innerHTML = `<tr><td colspan="5" class="table-empty">No daily data recorded yet</td></tr>`;
    return;
  }

  const sortedDates = Object.keys(dailyStats).sort().reverse();
  const maxVol = Math.max(...sortedDates.map(d => Math.max(dailyStats[d].delivered, dailyStats[d].opened, dailyStats[d].clicked, 1)));

  sortedDates.forEach(d => {
    const row = dailyStats[d];
    const delBarPct = Math.round((row.delivered / maxVol) * 100);
    const openBarPct = Math.round((row.opened / maxVol) * 100);
    const clickBarPct = Math.round((row.clicked / maxVol) * 100);

    const tr = document.createElement("tr");
    tr.innerHTML = `
      <td><strong>${row.date}</strong></td>
      <td><span class="type-badge type-delivered">${row.delivered.toLocaleString()}</span></td>
      <td><span class="type-badge type-opened">${row.opened.toLocaleString()}</span></td>
      <td><span class="type-badge type-clicked">${row.clicked.toLocaleString()}</span></td>
      <td>
        <div style="display: flex; gap: 4px; align-items: center; width: 140px; height: 10px; background: rgba(255,255,255,0.05); border-radius: 4px; padding: 2px;">
          <div style="height: 100%; width: ${delBarPct}%; background: #10b981; border-radius: 2px;"></div>
          <div style="height: 100%; width: ${openBarPct}%; background: #06b6d4; border-radius: 2px;"></div>
          <div style="height: 100%; width: ${clickBarPct}%; background: #8b5cf6; border-radius: 2px;"></div>
        </div>
      </td>
    `;
    tbody.appendChild(tr);
  });
}

// Load Paginated Events
async function loadEvents() {
  if (!activeCampaign) return;
  try {
    let url = `/campaigns/${encodeURIComponent(activeCampaign)}/events?limit=${pageLimit}&offset=${currentOffset}`;
    if (currentEventType) {
      url += `&type=${encodeURIComponent(currentEventType)}`;
    }

    const res = await fetch(url);
    if (!res.ok) return;
    const data = await res.json();
    renderEvents(data);
  } catch (err) {
    console.error("loadEvents error:", err);
  }
}

function renderEvents(data) {
  const tbody = document.getElementById("events-tbody");
  tbody.innerHTML = "";

  if (!data.events || data.events.length === 0) {
    tbody.innerHTML = `<tr><td colspan="5" class="table-empty">No events matching this filter</td></tr>`;
  } else {
    data.events.forEach(ev => {
      const meta = ev.metadata ? `<code class="code-badge">${JSON.stringify(ev.metadata)}</code>` : '<span style="color: var(--text-dim);">-</span>';
      const tr = document.createElement("tr");
      tr.innerHTML = `
        <td><span class="code-badge">${escapeHtml(ev.event_id)}</span></td>
        <td><span class="type-badge type-${ev.type}">${ev.type}</span></td>
        <td><span class="code-badge">${escapeHtml(ev.contact_id)}</span></td>
        <td style="font-family: 'JetBrains Mono', monospace; font-size: 0.78rem;">${new Date(ev.timestamp).toISOString()}</td>
        <td>${meta}</td>
      `;
      tbody.appendChild(tr);
    });
  }

  // Update pagination info
  const start = data.total > 0 ? data.offset + 1 : 0;
  const end = Math.min(data.offset + data.limit, data.total);
  document.getElementById("pagination-info").textContent = `Showing ${start}-${end} of ${data.total}`;

  document.getElementById("btn-prev-page").disabled = currentOffset <= 0;
  document.getElementById("btn-next-page").disabled = currentOffset + pageLimit >= data.total;
}

// Ingest Seed Data
async function handleIngestSeedData() {
  showToast("Ingesting sample seed dataset...");
  try {
    const res = await fetch("/events", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(PRESETS.seedSample)
    });
    const data = await res.json();
    showToast(`Ingested ${data.accepted} events (${data.duplicates} dups)`);
    await loadCampaigns();
    if (activeCampaign) refreshDashboard();
  } catch (err) {
    showToast("Ingestion failed: " + err.message);
  }
}

// Submit Simulator Form
async function handleSimulatorSubmit() {
  const rawJSON = document.getElementById("sim-json-input").value.trim();
  if (!rawJSON) {
    showToast("Please enter JSON array payload");
    return;
  }

  try {
    const res = await fetch("/events", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: rawJSON
    });

    const data = await res.json();
    const resultBox = document.getElementById("sim-result-box");
    resultBox.style.display = "block";

    const badge = document.getElementById("result-status-badge");
    badge.textContent = `${res.status} ${res.statusText || (res.status === 200 ? "OK" : "Accepted")}`;
    badge.className = "badge " + (res.status === 200 ? "badge-green" : res.status === 202 ? "badge-amber" : "badge-red");

    document.getElementById("res-total").textContent = data.total || 0;
    document.getElementById("res-accepted").textContent = data.accepted || 0;
    document.getElementById("res-duplicates").textContent = data.duplicates || 0;
    document.getElementById("res-rejected").textContent = data.rejected || 0;

    const errorDetails = document.getElementById("res-error-details");
    if (data.errors && data.errors.length > 0) {
      errorDetails.style.display = "block";
      errorDetails.innerHTML = "<strong>Validation Errors:</strong><br>" + 
        data.errors.map(e => `• Item [${e.index}] (${e.event_id || "no id"}): ${escapeHtml(e.reason)}`).join("<br>");
    } else {
      errorDetails.style.display = "none";
    }

    showToast("Webhook batch processed");
    await loadCampaigns();
    if (activeCampaign) refreshDashboard();
  } catch (err) {
    showToast("Submission error: " + err.message);
  }
}

function startAutoRefresh() {
  if (pollingTimer) clearInterval(pollingTimer);
  pollingTimer = setInterval(() => {
    if (activeCampaign && document.getElementById("sim-modal").style.display !== "flex") {
      loadStats();
    }
  }, 3000);
}

function showToast(msg) {
  const toast = document.getElementById("toast");
  toast.textContent = msg;
  toast.classList.add("show");
  setTimeout(() => toast.classList.remove("show"), 3500);
}

function escapeHtml(str) {
  if (!str) return "";
  return String(str).replace(/[&<>"']/g, m => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#039;' })[m]);
}
