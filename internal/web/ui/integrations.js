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
    const present = Boolean(tool.present);
    const configured = Boolean(tool.configured);
    const selectable = installed || present;
    const result = integrationResult(tool.id);
    const status = configured
      ? '<span class="tag green">已接入</span>'
      : present
        ? '<span class="tag">待迁移</span>'
        : '<span class="tag">未接入</span>';
    return `<label class="integration-card ${selectable ? "" : "unavailable"}">
      <div class="integration-card-head">
        <input class="integration-check" type="checkbox" value="${esc(tool.id)}"
          ${installed && !configured ? "checked" : ""} ${selectable ? "" : "disabled"}>
        <span class="integration-name">${esc(tool.name || tool.id)}</span>
      </div>
      <div class="tags">
        <span class="tag ${installed ? "green" : ""}">${installed ? "已安装" : "未安装"}</span>
        ${status}
        ${tool.method ? `<span class="tag">${esc(tool.method)}</span>` : ""}
        ${tool.transport ? `<span class="tag">${esc(tool.transport)}</span>` : ""}
      </div>
      <dl class="integration-details">
        <div><dt>命令路径</dt><dd><code>${esc(tool.command_path || "—")}</code></dd></div>
        <div><dt>配置路径</dt><dd><code>${esc(tool.config_path || "—")}</code></dd></div>
      </dl>
      ${tool.note ? `<p class="integration-note">${esc(tool.note)}</p>` : ""}
      ${tool.restart_hint ? `<p class="integration-restart">操作后：${esc(tool.restart_hint)}</p>` : ""}
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
  const selectedTools = state.integrationTools.filter((tool) => selected.includes(String(tool.id)));
  const configurable = selectedTools.filter((tool) => tool.installed && !tool.configured).length;
  const removable = selectedTools.filter((tool) => tool.present).length;
  $("#integration-selection").textContent = selected.length
    ? `已选择 ${selected.length} 个；可接入 ${configurable} 个，可取消 ${removable} 个`
    : "尚未选择工具";
  $("#btn-configure-integrations").disabled = state.integrationsLoading || configurable === 0;
  $("#btn-remove-integrations").disabled = state.integrationsLoading || removable === 0;
  $("#btn-select-installed").disabled = state.integrationsLoading || !state.integrationTools.some((tool) => tool.installed && !tool.configured);
  $("#btn-select-removable").disabled = state.integrationsLoading || !state.integrationTools.some((tool) => tool.present);
  const hint = $("#integration-action-hint");
  hint.classList.toggle("warn", selected.length > 0 && removable === 0);
  hint.textContent = selected.length > 0 && removable === 0
    ? "不能取消接入：当前选中的工具都没有 ssh-mcp 配置。请选择标有“已接入”或“待迁移”的工具。"
    : removable > 0
      ? `可取消 ${removable} 个工具的 ssh-mcp 配置；不会卸载工具本体。`
      : "取消接入前，请选择标有“已接入”或“待迁移”的工具。";
}

function setIntegrationBusy(busy) {
  $("#btn-refresh-integrations").disabled = busy;
  if (busy) {
    $("#btn-select-installed").disabled = true;
    $("#btn-select-removable").disabled = true;
    $("#btn-configure-integrations").disabled = true;
    $("#btn-remove-integrations").disabled = true;
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
  box.innerHTML = `<h3>${esc(state.integrationResultTitle)}</h3>${state.integrationResults.map((result) => `
    <div class="integration-result ${result.ok ? "success" : "failure"}">
      <strong>${result.ok ? "✓" : "✗"} ${esc(result.name || result.id)}</strong>
      <span>${esc(result.message || (result.ok ? "接入成功" : "接入失败"))}</span>
      ${result.config_path ? `<small>配置：<code>${esc(result.config_path)}</code></small>` : ""}
      ${result.backup_path ? `<small>备份：<code>${esc(result.backup_path)}</code></small>` : ""}
    </div>`).join("")}`;
}

$("#btn-refresh-integrations").addEventListener("click", () => loadIntegrations());
$("#btn-select-installed").addEventListener("click", () => {
  $$("#integration-list .integration-check:not(:disabled)").forEach((checkbox) => {
    const tool = state.integrationTools.find((item) => String(item.id) === checkbox.value);
    checkbox.checked = Boolean(tool && tool.installed && !tool.configured);
  });
  syncIntegrationSelection();
});
$("#btn-select-removable").addEventListener("click", () => {
  $$("#integration-list .integration-check:not(:disabled)").forEach((checkbox) => {
    const tool = state.integrationTools.find((item) => String(item.id) === checkbox.value);
    checkbox.checked = Boolean(tool && tool.present);
  });
  syncIntegrationSelection();
});
async function runIntegrationAction(action) {
  const selected = selectedIntegrationIDs();
  const tools = state.integrationTools
    .filter((tool) => selected.includes(String(tool.id)) && (action === "configure" ? tool.installed && !tool.configured : tool.present))
    .map((tool) => String(tool.id));
  const verb = action === "configure" ? "接入" : "取消接入";
  if (!tools.length) return toast(`请先选择可${verb}的工具`, "err");
  const names = state.integrationTools
    .filter((tool) => tools.includes(String(tool.id)))
    .map((tool) => tool.name || tool.id);
  const prompt = action === "configure"
    ? `将为以下工具写入无参数 stdio 配置：\n${names.join("、")}\n\n每个工具会启动独立 SSH-MCP 子进程。\n\n确认继续？`
    : `将从以下工具中移除 ssh-mcp 配置：\n${names.join("、")}\n\n写入前会创建备份，其他 MCP 配置不会删除。\n\n确认继续？`;
  if (!confirm(prompt)) return;
  state.integrationsLoading = true;
  setIntegrationBusy(true);
  syncIntegrationSelection();
  try {
    const path = action === "configure" ? "/api/integrations/configure" : "/api/integrations/remove";
    const data = await api(path, { method: "POST", body: { tools } });
    state.integrationResults = Array.isArray(data.results) ? data.results : [];
    state.integrationResultTitle = `${verb}结果`;
    renderIntegrationResults();
    renderIntegrations();
    const succeeded = state.integrationResults.filter((result) => result.ok).length;
    const failed = state.integrationResults.length - succeeded;
    toast(`${verb}完成：成功 ${succeeded} 项，失败 ${failed} 项`, failed ? "err" : "ok");
    state.integrationsLoading = false;
    if (succeeded > 0) await loadIntegrations(true);
  } catch (ex) {
    toast(ex.message, "err");
  } finally {
    state.integrationsLoading = false;
    setIntegrationBusy(false);
    syncIntegrationSelection();
  }
}

$("#btn-configure-integrations").addEventListener("click", () => runIntegrationAction("configure"));
$("#btn-remove-integrations").addEventListener("click", () => runIntegrationAction("remove"));
