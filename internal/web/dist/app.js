const fmt = (n) => {
  if (n < 1024) return n + " B";
  const u = ["KB", "MB", "GB", "TB"];
  let v = n / 1024, i = 0;
  while (v >= 1024 && i < u.length - 1) { v /= 1024; i++; }
  return v.toFixed(v >= 100 ? 0 : v >= 10 ? 1 : 2) + " " + u[i];
};

const state = {
  tab: "process",
  rangeMin: 60,
  refreshTimer: null,
};

const view = document.getElementById("view");
const banner = document.getElementById("banner");

function setTab(tab) {
  state.tab = tab;
  document.querySelectorAll(".tab").forEach(b => b.classList.toggle("active", b.dataset.tab === tab));
  refresh();
}

document.querySelectorAll(".tab").forEach(b => b.addEventListener("click", () => setTab(b.dataset.tab)));
document.getElementById("range").addEventListener("change", e => {
  state.rangeMin = parseInt(e.target.value, 10);
  refresh();
});

const dlg = document.getElementById("settings-dlg");
document.getElementById("settings-btn").addEventListener("click", async () => {
  const r = await fetch("/api/settings/mihomo");
  const s = await r.json();
  document.getElementById("mihomo-url").value = s.url || "";
  document.getElementById("mihomo-secret").value = s.secret || "";
  dlg.showModal();
});
document.getElementById("settings-form").addEventListener("submit", async (e) => {
  // Triggered by either button. Only persist on save.
  const submitter = e.submitter && e.submitter.value;
  if (submitter !== "save") return;
  e.preventDefault();
  await fetch("/api/settings/mihomo", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      url: document.getElementById("mihomo-url").value,
      secret: document.getElementById("mihomo-secret").value,
    }),
  });
  dlg.close();
  refresh();
});

async function refreshBanner() {
  try {
    const d = await (await fetch("/api/diagnostics")).json();
    if (d.missingProcessRatio >= 0.8) {
      banner.textContent = `检测到最近 5 分钟内 ${(d.missingProcessRatio * 100).toFixed(0)}% 流量缺少进程信息。${d.hint}`;
      banner.classList.remove("hidden");
    } else {
      banner.classList.add("hidden");
    }
  } catch { /* ignore */ }
}

function rangeParams() {
  const now = Math.floor(Date.now() / 60000);
  return `from=${now - state.rangeMin}&to=${now}`;
}

async function refresh() {
  if (state.refreshTimer) { clearInterval(state.refreshTimer); state.refreshTimer = null; }
  await refreshBanner();
  if (state.tab === "live") {
    await renderLive();
    state.refreshTimer = setInterval(renderLive, 3000);
    return;
  }
  await renderSummary(state.tab);
  state.refreshTimer = setInterval(() => renderSummary(state.tab), 10000);
}

async function renderSummary(group) {
  const r = await fetch(`/api/summary?group=${group}&top=50&${rangeParams()}`);
  const data = await r.json();
  const rows = data.rows || [];
  if (rows.length === 0) {
    view.innerHTML = `<div class="empty">暂无数据。请确认 mihomo 已运行，且 <code>find-process-mode</code> 已开启。</div>`;
    return;
  }
  const max = rows.reduce((m, r) => Math.max(m, r.upload + r.download), 0) || 1;
  const labelHeader = { process: "进程", host: "域名", ip: "目标 IP", outbound: "出口" }[group];
  const rowsHtml = rows.map((r) => {
    const total = r.upload + r.download;
    const pct = (total / max) * 100;
    const label = r.label === "" ? `<span class="muted">(unknown)</span>` : escapeHTML(r.label);
    return `<tr>
      <td>${label}</td>
      <td class="num">${fmt(r.upload)}</td>
      <td class="num">${fmt(r.download)}</td>
      <td class="num">${fmt(total)}</td>
      <td class="num">${r.count}</td>
      <td class="bar-cell"><div class="bar" style="width:${pct.toFixed(1)}%"></div></td>
    </tr>`;
  }).join("");
  view.innerHTML = `<table>
    <thead><tr>
      <th>${labelHeader}</th>
      <th class="num">上行</th><th class="num">下行</th>
      <th class="num">合计</th><th class="num">连接数</th>
      <th>占比</th>
    </tr></thead>
    <tbody>${rowsHtml}</tbody>
  </table>`;
}

async function renderLive() {
  const r = await fetch("/api/connections/live");
  const data = await r.json();
  const items = data.items || [];
  if (items.length === 0) {
    view.innerHTML = `<div class="empty">当前无活跃连接。</div>`;
    return;
  }
  const rowsHtml = items.slice(0, 200).map((c) => {
    const proc = c.processName || `<span class="muted">(unknown)</span>`;
    return `<tr>
      <td>${escapeHTML(proc)}</td>
      <td>${escapeHTML(c.host || c.destIP)}</td>
      <td>${escapeHTML(c.network || "")}</td>
      <td>${escapeHTML(c.outbound || "")}</td>
      <td class="num">${fmt(c.upload)}</td>
      <td class="num">${fmt(c.download)}</td>
    </tr>`;
  }).join("");
  view.innerHTML = `<table>
    <thead><tr>
      <th>进程</th><th>目标</th><th>协议</th><th>出口</th>
      <th class="num">累计上行</th><th class="num">累计下行</th>
    </tr></thead>
    <tbody>${rowsHtml}</tbody>
  </table>`;
}

function escapeHTML(s) {
  return String(s).replace(/[&<>"']/g, c => ({
    "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;",
  }[c]));
}

refresh();
