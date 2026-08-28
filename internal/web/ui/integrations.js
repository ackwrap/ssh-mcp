/* SSH-MCP AI 工具检测与一键接入 */
"use strict";

async function loadIntegrations(preserveResults = false) {
  if (state.integrationsLoading) return;
  state.integrationsLoading = true;
  if (!preserveResults) state.integrationResults = [];
  setIntegrationBusy(true);
  $("#integration-exe").textContent = "检测中…";
  $("#integration-list").innerHTML = `<div class="panel integration-empty">正在检测本机 AI 工具…</div>`;
  if (!preserveResults) renderIntegrationResults();
  try {
    const data = await api("/api/integrations");
    state.integrationTools = Array.isArray(data.tools) ? data.tools : [];
    $("#integration-exe").textContent = data.executable || "未提供";
    renderIntegrations();
  } catch (ex) {
    state.integrationTools = [];
    $("#integration-exe").textContent = "检测失败";
    $("#integration-list").innerHTML = `<div class="panel integration-empty fail">检测失败：${esc(ex.message)}</div>`;
    toast(ex.message, "err");
  } finally {
    state.integrationsLoading = false;
    setIntegrationBusy(false);
    syncIntegrationSelection();
  }
}

function integrationResult(id) {
  return state.integrationResults.find((result) => result.id === id);
}

function renderIntegrations() {
  const box = $("#integration-list");
  if (!state.integrationTools.length) {
    box.innerHTML = `<div class="panel integration-empty">未检测到可接入的 AI 工具。</div>`;
    return;
  }
  box.innerHTML = state.integrationTools.map((tool) => {
    const installed = Boolean(tool.installed);
    const configured = Boolean(tool.configured);
    const result = integrationResult(tool.id);
    return `<label class="integration-card ${installed ? "" : "unavailable"}">
      <div class="integration-card-head">
        <input class="integration-check" type="checkbox" value="${esc(tool.id)}"
          ${installed && !configured ? "checked" : ""} ${installed ? "" : "disabled"}>
        <span class="integration-name">${esc(tool.name || tool.id)}</span>
      </div>
      <div class="tags">
        <span class="tag ${installed ? "green" : ""}">${installed ? "已安装" : "未安装"}</span>
        ${configured ? '<span class="tag green">已接入</span>' : '<span class="tag">未接入</span>'}
        ${tool.method ? `<span class="tag">${esc(tool.method)}</span>` : ""}
      </div>
      <dl class="integration-details">
        <div><dt>命令路径</dt><dd><code>${esc(tool.command_path || "—")}</code></dd></div>
        <div><dt>配置路径</dt><dd><code>${esc(tool.config_path || "—")}</code></dd></div>
      </dl>
      ${tool.note ? `<p class="integration-note">${esc(tool.note)}</p>` : ""}
      ${tool.restart_hint ? `<p class="integration-restart">接入后：${esc(tool.restart_hint)}</p>` : ""}
      ${result ? `<div class="integration-inline-result ${result.ok ? "ok" : "fail"}">
        ${result.ok ? "✓" : "✗"} ${esc(result.message || (result.ok ? "接入成功" : "接入失败"))}
      </div>` : ""}
    </label>`;
  }).join("");
  $$("#integration-list .integration-check").forEach((checkbox) =>
    checkbox.addEventListener("change", syncIntegrationSelection)
  );
}

function selectedIntegrationIDs() {
  return $$("#integration-list .integration-check:checked").map((checkbox) => checkbox.value);
}

function syncIntegrationSelection() {
  const selected = selectedIntegrationIDs();
  $("#integration-selection").textContent = selected.length ? `已选择 ${selected.length} 个工具` : "尚未选择工具";
  $("#btn-configure-integrations").disabled = state.integrationsLoading || selected.length === 0;
  $("#btn-select-installed").disabled = state.integrationsLoading || !state.integrationTools.some((tool) => tool.installed);
}

function setIntegrationBusy(busy) {
  $("#btn-refresh-integrations").disabled = busy;
  if (busy) {
    $("#btn-select-installed").disabled = true;
    $("#btn-configure-integrations").disabled = true;
  }
  $$("#integration-list .integration-check").forEach((checkbox) => {
    checkbox.disabled = busy || checkbox.closest(".integration-card").classList.contains("unavailable");
  });
}

function renderIntegrationResults() {
  const box = $("#integration-results");
  if (!state.integrationResults.length) {
    box.classList.add("hidden");
    box.innerHTML = "";
    return;
  }
  box.classList.remove("hidden");
  box.innerHTML = `<h3>接入结果</h3>${state.integrationResults.map((result) => `
    <div class="integration-result ${result.ok ? "success" : "failure"}">
      <strong>${result.ok ? "✓" : "✗"} ${esc(result.name || result.id)}</strong>
      <span>${esc(result.message || (result.ok ? "接入成功" : "接入失败"))}</span>
      ${result.config_path ? `<small>配置：<code>${esc(result.config_path)}</code></small>` : ""}
      ${result.backup_path ? `<small>备份：<code>${esc(result.backup_path)}</code></small>` : ""}
    </div>`).join("")}`;
}

$("#btn-refresh-integrations").addEventListener("click", () => loadIntegrations());
$("#btn-select-installed").addEventListener("click", () => {
  $$("#integration-list .integration-check:not(:disabled)").forEach((checkbox) => { checkbox.checked = true; });
  syncIntegrationSelection();
});
$("#btn-configure-integrations").addEventListener("click", async () => {
  const tools = selectedIntegrationIDs();
  if (!tools.length) return toast("请先选择要接入的工具", "err");
  const names = state.integrationTools
    .filter((tool) => tools.includes(String(tool.id)))
    .map((tool) => tool.name || tool.id);
  if (!confirm(`将自动写入以下工具的配置：\n${names.join("、")}\n\n确认继续？`)) return;
  state.integrationsLoading = true;
  setIntegrationBusy(true);
  syncIntegrationSelection();
  try {
    const data = await api("/api/integrations/configure", { method: "POST", body: { tools } });
    state.integrationResults = Array.isArray(data.results) ? data.results : [];
    renderIntegrationResults();
    renderIntegrations();
    const succeeded = state.integrationResults.filter((result) => result.ok).length;
    const failed = state.integrationResults.length - succeeded;
    toast(`接入完成：成功 ${succeeded} 项，失败 ${failed} 项`, failed ? "err" : "ok");
    state.integrationsLoading = false;
    if (succeeded > 0) await loadIntegrations(true);
  } catch (ex) {
    toast(ex.message, "err");
  } finally {
    state.integrationsLoading = false;
    setIntegrationBusy(false);
    syncIntegrationSelection();
  }
});
