/* SSH-MCP WebUI 前端逻辑（原生 JS，无构建步骤） */
"use strict";

const $ = (sel) => document.querySelector(sel);
const $$ = (sel) => [...document.querySelectorAll(sel)];
const MASK = "******";
const state = {
  configured: false,
  authenticated: false,
  integrationTools: [],
  integrationResults: [],
  integrationsLoading: false,
};

// ---------------- 基础工具 ----------------
async function api(path, opts = {}) {
  const init = { method: opts.method || "GET", headers: {} };
  if (opts.body !== undefined) {
    init.headers["Content-Type"] = "application/json";
    init.body = JSON.stringify(opts.body);
  }
  const res = await fetch(path, init);
  if (res.status === 401) {
    state.authenticated = false;
    showAuth();
    throw new Error("登录已失效，请重新登录");
  }
  const data = await res.json().catch(() => ({}));
  if (!res.ok) throw new Error(data.detail || `请求失败 (${res.status})`);
  return data;
}

let toastTimer = null;
function toast(msg, type = "ok") {
  const el = $("#toast");
  el.textContent = msg;
  el.className = type;
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => el.classList.add("hidden"), 3200);
}

async function copyText(text) {
  try {
    await navigator.clipboard.writeText(text);
  } catch {
    const ta = document.createElement("textarea");
    ta.value = text;
    document.body.appendChild(ta);
    ta.select();
    document.execCommand("copy");
    ta.remove();
  }
  toast("已复制到剪贴板");
}

function esc(s) {
  return String(s ?? "").replace(/[&<>"']/g, (c) => ({
    "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;",
  }[c]));
}

function proxyLabel(p) {
  return p ? `<span class="tag orange">${p.type.toUpperCase()} 代理 ${esc(p.host)}:${p.port}</span>` : "";
}
function jumpLabel(jv) {
  return jv ? `<span class="tag">跳板 ${esc(jv)}</span>` : "";
}
function authLabel(a) {
  const map = { password: "密码", key_data: "私钥", key_path: "私钥文件" };
  return `<span class="tag">${map[a.type] || a.type}</span>`;
}

// ---------------- 认证 ----------------
async function checkAuth() {
  try {
    const s = await api("/api/auth/status");
    state.configured = s.configured;
    state.authenticated = s.authenticated;
  } catch { /* 后端未启动 */ }
  if (state.authenticated) {
    $("#auth-overlay").classList.add("hidden");
    $("#app").classList.remove("hidden");
    refreshAll();
  } else {
    showAuth();
  }
}

function showAuth() {
  $("#app").classList.add("hidden");
  const ov = $("#auth-overlay");
  ov.classList.remove("hidden");
  const setup = !state.configured;
  $("#auth-title").textContent = setup ? "设置主密码" : "登录解锁";
  $("#auth-sub").textContent = setup
    ? "首次使用需要设置主密码，用于加密所有服务器凭据（不可找回，请牢记）"
    : "输入主密码解锁，MCP 工具与配置管理均可使用";
  $("#auth-pass2-wrap").classList.toggle("hidden", !setup);
  $("#auth-btn").textContent = setup ? "设置并进入" : "登录";
  $("#auth-err").classList.add("hidden");
}

$("#auth-form").addEventListener("submit", async (e) => {
  e.preventDefault();
  const err = $("#auth-err");
  err.classList.add("hidden");
  const pass = $("#auth-pass").value;
  const pass2 = $("#auth-pass2").value;
  if (pass.length < 6) return showErr("主密码至少 6 位");
  if (!state.configured && pass !== pass2) return showErr("两次输入的密码不一致");
  try {
    if (!state.configured) {
      await api("/api/auth/setup", { method: "POST", body: { password: pass } });
      toast("主密码已设置");
    } else {
      await api("/api/auth/login", { method: "POST", body: { password: pass } });
      toast("已解锁");
    }
    $("#auth-pass").value = "";
    $("#auth-pass2").value = "";
    state.authenticated = true;
    checkAuth();
  } catch (ex) {
    showErr(ex.message);
  }
  function showErr(m) {
    err.textContent = m;
    err.classList.remove("hidden");
  }
});

// ---------------- 路由 ----------------
function route() {
  const view = (location.hash || "#servers").slice(1).split("?")[0] || "servers";
  $$(".view").forEach((v) => v.classList.add("hidden"));
  $$("nav a").forEach((a) => a.classList.toggle("active", a.dataset.view === view));
  const el = $(`#view-${view}`);
  if (el) el.classList.remove("hidden");
  if (view === "servers") loadServers();
  if (view === "jumps") loadJumps();
  if (view === "integrations") loadIntegrations();
}
window.addEventListener("hashchange", route);

function refreshAll() {
  route();
}

// ---------------- 服务器 ----------------
async function loadServers() {
  let data;
  try {
    data = await api("/api/servers");
    const jumps = await api("/api/jumphosts");
    state.jumps = jumps.jumphosts;
  } catch (ex) { toast(ex.message, "err"); return; }
  state.servers = data.servers;
  const box = $("#server-list");
  if (!data.servers.length) {
    box.innerHTML = `<p class="hint">还没有服务器。点击右上角「+ 新增服务器」添加，或在 AI 工具里直接让我帮你添加。</p>`;
    return;
  }
  box.innerHTML = data.servers.map(serverCard).join("");
  $$("#server-list .test-btn").forEach((b) =>
    b.addEventListener("click", () => testServer(b.dataset.name, b))
  );
  $$("#server-list .edit-btn").forEach((b) =>
    b.addEventListener("click", () => serverModal(b.dataset.name))
  );
  $$("#server-list .copy-btn").forEach((b) =>
    b.addEventListener("click", () => serverModal(b.dataset.name, true))
  );
  $$("#server-list .del-btn").forEach((b) =>
    b.addEventListener("click", () => delServer(b.dataset.name))
  );
}

function serverCard(s) {
  return `<div class="card">
    <div class="name">${esc(s.name)}</div>
    <div class="meta">${esc(s.username)}@${esc(s.host)}:${s.port}</div>
    <div class="tags">${authLabel(s.auth)}${jumpLabel(s.jump_via)}${proxyLabel(s.proxy)}</div>
    <div class="status" id="st-${esc(s.name)}"></div>
    <div class="actions">
      <button class="btn small test-btn" data-name="${esc(s.name)}">测试连接</button>
      <button class="btn small edit-btn" data-name="${esc(s.name)}">编辑</button>
      <button class="btn small copy-btn" data-name="${esc(s.name)}">复制</button>
      <button class="btn small danger del-btn" data-name="${esc(s.name)}">删除</button>
    </div>
  </div>`;
}

async function testServer(name, btn) {
  btn.disabled = true;
  const st = $(`#st-${CSS.escape(name)}`);
  st.innerHTML = `<span class="hint">测试中…</span>`;
  try {
    const r = await api(`/api/servers/${encodeURIComponent(name)}/test`, { method: "POST" });
    st.innerHTML = r.ok
      ? `<span class="ok">✓ 连接成功（${r.latency_ms}ms）</span>`
      : `<span class="fail">✗ ${esc(r.message)}</span>`;
  } catch (ex) {
    st.innerHTML = `<span class="fail">✗ ${esc(ex.message)}</span>`;
  }
  btn.disabled = false;
}

async function delServer(name) {
  if (!confirm(`确认删除服务器「${name}」？`)) return;
  try {
    await api(`/api/servers/${encodeURIComponent(name)}`, { method: "DELETE" });
    toast("已删除");
    loadServers();
  } catch (ex) { toast(ex.message, "err"); }
}

function serverModal(name, copying = false) {
  const s = name ? state.servers?.find((x) => x.name === name) : null;
  const sourceName = copying ? name : null;
  openModal(`
    <h2>${copying ? `复制服务器 ${esc(s.name)}` : (s ? `编辑服务器 ${esc(s.name)}` : "新增服务器")}</h2>
    <div class="form-grid">
      <label>名称 *<input id="m-name" value="${esc(s?.name || "")}" placeholder="如 prod-web-01"></label>
      <label>主机 *<input id="m-host" value="${esc(s?.host || "")}" placeholder="IP 或域名"></label>
      <label>端口<input id="m-port" type="number" value="${s?.port || 22}"></label>
      <label>用户名 *<input id="m-user" value="${esc(s?.username || "")}"></label>
      <label class="full">认证方式
        <select id="m-authtype">
          <option value="password" ${s?.auth.type === "password" ? "selected" : ""}>密码</option>
          <option value="key_data" ${s?.auth.type === "key_data" ? "selected" : ""}>私钥（粘贴内容）</option>
          <option value="key_path" ${s?.auth.type === "key_path" ? "selected" : ""}>私钥（文件路径）</option>
        </select>
      </label>
      <label class="full" id="m-pw-wrap"><span>密码</span><input id="m-pw" type="password" value="${esc(s?.auth.password || "")}" placeholder="留空保持原值，填新值覆盖"></label>
      <label class="full hidden" id="m-keydata-wrap"><span>私钥内容</span><textarea id="m-keydata" rows="5" placeholder="-----BEGIN OPENSSH PRIVATE KEY-----…">${esc(s?.auth.key_data || "")}</textarea></label>
      <label class="full hidden" id="m-keypath-wrap"><span>私钥文件路径</span><input id="m-keypath" value="${esc(s?.auth.key_path || "")}" placeholder="C:\\Users\\you\\.ssh\\id_ed25519"></label>
      <label class="full hidden" id="m-pass-wrap"><span>私钥口令（可选）</span><input id="m-passphrase" type="password" value="${esc(s?.auth.passphrase || "")}"></label>
      <label class="full">连接方式
        <select id="m-jump">
          <option value="">直连</option>
          ${(state.jumps || []).map((j) => `<option value="${esc(j.name)}" ${s?.jump_via === j.name ? "selected" : ""}>跳板 ${esc(j.name)}</option>`).join("")}
        </select>
      </label>
      <label>代理类型
        <select id="m-proxytype">
          <option value="">无</option>
          <option value="socks5" ${s?.proxy?.type === "socks5" ? "selected" : ""}>SOCKS5</option>
          <option value="http" ${s?.proxy?.type === "http" ? "selected" : ""}>HTTP CONNECT</option>
        </select>
      </label>
      <div class="full" id="m-proxy-wrap" ${s?.proxy ? "" : 'style="display:none"'}>
        <div class="form-grid">
          <label>代理主机<input id="m-phost" value="${esc(s?.proxy?.host || "")}"></label>
          <label>代理端口<input id="m-pport" type="number" value="${s?.proxy?.port || 1080}"></label>
          <label>代理用户名<input id="m-puser" value="${esc(s?.proxy?.username || "")}"></label>
          <label>代理密码<input id="m-ppass" type="password" value="${esc(s?.proxy?.password || "")}"></label>
        </div>
      </div>
    </div>
    <div class="modal-actions">
      <button class="btn" id="m-cancel">取消</button>
      <button class="btn primary" id="m-save">保存</button>
    </div>
  `);
  const syncAuth = () => {
    const t = $("#m-authtype").value;
    $("#m-pw-wrap").classList.toggle("hidden", t !== "password");
    $("#m-keydata-wrap").classList.toggle("hidden", t !== "key_data");
    $("#m-keypath-wrap").classList.toggle("hidden", t !== "key_path");
    $("#m-pass-wrap").classList.toggle("hidden", t === "password");
  };
  $("#m-authtype").addEventListener("change", syncAuth);
  $("#m-proxytype").addEventListener("change", () => {
    $("#m-proxy-wrap").style.display = $("#m-proxytype").value ? "" : "none";
  });
  syncAuth();
  if (copying) {
    $("#m-name").focus();
    $("#m-name").select();
  }
  $("#m-cancel").addEventListener("click", closeModal);
  $("#m-save").addEventListener("click", async () => {
    const t = $("#m-authtype").value;
    const proxyType = $("#m-proxytype").value;
    const body = {
      name: $("#m-name").value.trim(),
      host: $("#m-host").value.trim(),
      port: parseInt($("#m-port").value) || 22,
      username: $("#m-user").value.trim(),
      auth: { type: t },
      jump_via: $("#m-jump").value || null,
      proxy: proxyType
        ? { type: proxyType, host: $("#m-phost").value.trim(), port: parseInt($("#m-pport").value) || 1080,
            username: $("#m-puser").value.trim() || null, password: $("#m-ppass").value || null }
        : null,
    };
    if (t === "password") body.auth.password = $("#m-pw").value || (s ? MASK : "");
    if (t === "key_data") {
      body.auth.key_data = $("#m-keydata").value || (s ? MASK : "");
      body.auth.passphrase = $("#m-passphrase").value || (s?.auth.passphrase === MASK ? MASK : null);
    }
    if (t === "key_path") {
      body.auth.key_path = $("#m-keypath").value.trim();
      body.auth.passphrase = $("#m-passphrase").value || (s?.auth.passphrase === MASK ? MASK : null);
    }
    if (body.proxy && s?.proxy && !body.proxy.password) body.proxy.password = MASK;
    try {
      if (copying) {
        await api(`/api/servers/${encodeURIComponent(sourceName)}/copy`, { method: "POST", body });
      } else if (s) {
        await api(`/api/servers/${encodeURIComponent(name)}`, { method: "PUT", body });
      } else {
        await api("/api/servers", { method: "POST", body });
      }
      closeModal();
      toast(copying ? "已复制并新增服务器" : "已保存");
      loadServers();
    } catch (ex) { toast(ex.message, "err"); }
  });
}

$("#btn-add-server").addEventListener("click", async () => {
  await ensureJumps();
  serverModal(null);
});
async function ensureJumps() {
  try {
    const d = await api("/api/jumphosts");
    state.jumps = d.jumphosts;
  } catch { state.jumps = []; }
}

// ---------------- 跳板机 ----------------
async function loadJumps() {
  let data;
  try {
    data = await api("/api/jumphosts");
    state.jumps = data.jumphosts;
  } catch (ex) { toast(ex.message, "err"); return; }
  const box = $("#jump-list");
  if (!data.jumphosts.length) {
    box.innerHTML = `<p class="hint">还没有跳板机。跳板机用于统一入口访问内网服务器，支持链式多跳。</p>`;
    return;
  }
  box.innerHTML = data.jumphosts.map(jumpCard).join("");
  $$("#jump-list .edit-btn").forEach((b) =>
    b.addEventListener("click", () => jumpModal(b.dataset.name))
  );
  $$("#jump-list .del-btn").forEach((b) =>
    b.addEventListener("click", () => delJump(b.dataset.name))
  );
}

function jumpCard(j) {
  return `<div class="card">
    <div class="name">${esc(j.name)}</div>
    <div class="meta">${esc(j.username)}@${esc(j.host)}:${j.port}</div>
    <div class="tags">${authLabel(j.auth)}${jumpLabel(j.jump_via)}</div>
    <div class="actions">
      <button class="btn small edit-btn" data-name="${esc(j.name)}">编辑</button>
      <button class="btn small danger del-btn" data-name="${esc(j.name)}">删除</button>
    </div>
  </div>`;
}

async function delJump(name) {
  if (!confirm(`确认删除跳板机「${name}」？`)) return;
  try {
    await api(`/api/jumphosts/${encodeURIComponent(name)}`, { method: "DELETE" });
    toast("已删除");
    loadJumps();
  } catch (ex) { toast(ex.message, "err"); }
}

function jumpModal(name) {
  const j = name ? state.jumps?.find((x) => x.name === name) : null;
  openModal(`
    <h2>${j ? `编辑跳板机 ${esc(j.name)}` : "新增跳板机"}</h2>
    <div class="form-grid">
      <label>名称 *<input id="j-name" value="${esc(j?.name || "")}" placeholder="如 bastion-prod"></label>
      <label>主机 *<input id="j-host" value="${esc(j?.host || "")}"></label>
      <label>端口<input id="j-port" type="number" value="${j?.port || 22}"></label>
      <label>用户名 *<input id="j-user" value="${esc(j?.username || "")}"></label>
      <label class="full">认证方式
        <select id="j-authtype">
          <option value="password" ${j?.auth.type === "password" ? "selected" : ""}>密码</option>
          <option value="key_data" ${j?.auth.type === "key_data" ? "selected" : ""}>私钥（粘贴内容）</option>
          <option value="key_path" ${j?.auth.type === "key_path" ? "selected" : ""}>私钥（文件路径）</option>
        </select>
      </label>
      <label class="full" id="j-pw-wrap"><span>密码</span><input id="j-pw" type="password" value="${esc(j?.auth.password || "")}" placeholder="留空保持原值，填新值覆盖"></label>
      <label class="full hidden" id="j-keydata-wrap"><span>私钥内容</span><textarea id="j-keydata" rows="5" placeholder="-----BEGIN OPENSSH PRIVATE KEY-----…">${esc(j?.auth.key_data || "")}</textarea></label>
      <label class="full hidden" id="j-keypath-wrap"><span>私钥文件路径</span><input id="j-keypath" value="${esc(j?.auth.key_path || "")}"></label>
      <label class="full hidden" id="j-pass-wrap"><span>私钥口令（可选）</span><input id="j-passphrase" type="password" value="${esc(j?.auth.passphrase || "")}"></label>
      <label class="full">上一级跳板（链式多跳）
        <select id="j-jump">
          <option value="">无（根跳板）</option>
          ${(state.jumps || []).filter((x) => x.name !== name).map((x) => `<option value="${esc(x.name)}" ${j?.jump_via === x.name ? "selected" : ""}>${esc(x.name)}</option>`).join("")}
        </select>
      </label>
    </div>
    <div class="modal-actions">
      <button class="btn" id="j-cancel">取消</button>
      <button class="btn primary" id="j-save">保存</button>
    </div>
  `);
  const syncAuth = () => {
    const t = $("#j-authtype").value;
    $("#j-pw-wrap").classList.toggle("hidden", t !== "password");
    $("#j-keydata-wrap").classList.toggle("hidden", t !== "key_data");
    $("#j-keypath-wrap").classList.toggle("hidden", t !== "key_path");
    $("#j-pass-wrap").classList.toggle("hidden", t === "password");
  };
  $("#j-authtype").addEventListener("change", syncAuth);
  syncAuth();
  $("#j-cancel").addEventListener("click", closeModal);
  $("#j-save").addEventListener("click", async () => {
    const t = $("#j-authtype").value;
    const body = {
      name: $("#j-name").value.trim(),
      host: $("#j-host").value.trim(),
      port: parseInt($("#j-port").value) || 22,
      username: $("#j-user").value.trim(),
      auth: { type: t },
      jump_via: $("#j-jump").value || null,
    };
    if (t === "password") body.auth.password = $("#j-pw").value || (j ? MASK : "");
    if (t === "key_data") {
      body.auth.key_data = $("#j-keydata").value || (j ? MASK : "");
      body.auth.passphrase = $("#j-passphrase").value || (j?.auth.passphrase === MASK ? MASK : null);
    }
    if (t === "key_path") {
      body.auth.key_path = $("#j-keypath").value.trim();
      body.auth.passphrase = $("#j-passphrase").value || (j?.auth.passphrase === MASK ? MASK : null);
    }
    try {
      if (j) await api(`/api/jumphosts/${encodeURIComponent(name)}`, { method: "PUT", body });
      else await api("/api/jumphosts", { method: "POST", body });
      closeModal();
      toast("已保存");
      loadJumps();
    } catch (ex) { toast(ex.message, "err"); }
  });
}

$("#btn-add-jump").addEventListener("click", async () => {
  await ensureJumps();
  jumpModal(null);
});

// ---------------- 导入导出 ----------------
$("#btn-export-full").addEventListener("click", async () => {
  try {
    const d = await api("/api/export?mode=full");
    await copyText(JSON.stringify(d, null, 2));
  } catch (ex) { toast(ex.message, "err"); }
});
$("#btn-export-full-file").addEventListener("click", async () => {
  try {
    const d = await api("/api/export?mode=full");
    const blob = new Blob([JSON.stringify(d, null, 2)], { type: "application/json" });
    const a = document.createElement("a");
    a.href = URL.createObjectURL(blob);
    a.download = `ssh_mcp_config_${new Date().toISOString().slice(0, 10)}.json`;
    a.click();
    URL.revokeObjectURL(a.href);
  } catch (ex) { toast(ex.message, "err"); }
});
$("#btn-export-template").addEventListener("click", async () => {
  try {
    const d = await api("/api/export?mode=template");
    await copyText(JSON.stringify(d, null, 2));
  } catch (ex) { toast(ex.message, "err"); }
});
$("#btn-import").addEventListener("click", async () => {
  const text = $("#import-text").value.trim();
  const pass = $("#import-pass").value;
  if (!text) return toast("请先粘贴配置 JSON", "err");
  let data;
  try { data = JSON.parse(text); } catch { return toast("JSON 格式不正确", "err"); }
  if (!confirm("导入将整体替换当前配置，确认继续？")) return;
  try {
    const r = await api("/api/import", { method: "POST", body: { data, source_password: pass } });
    toast(`导入成功：${r.servers} 台服务器，${r.jumphosts} 个跳板机`);
    $("#import-text").value = "";
    $("#import-pass").value = "";
  } catch (ex) { toast(ex.message, "err"); }
});

// ---------------- 设置 ----------------
$("#btn-change-pw").addEventListener("click", async () => {
  const old = $("#pw-old").value, n1 = $("#pw-new").value, n2 = $("#pw-new2").value;
  if (!old) return toast("请输入当前主密码", "err");
  if (n1.length < 6) return toast("新主密码至少 6 位", "err");
  if (n1 !== n2) return toast("两次输入的新主密码不一致", "err");
  try {
    await api("/api/auth/change_password", { method: "POST", body: { old_password: old, new_password: n1 } });
    toast("主密码已修改");
    $("#pw-old").value = $("#pw-new").value = $("#pw-new2").value = "";
  } catch (ex) { toast(ex.message, "err"); }
});

$("#btn-lock").addEventListener("click", async () => {
  try {
    await api("/api/auth/lock", { method: "POST" });
    toast("已锁定");
    state.authenticated = false;
    showAuth();
  } catch (ex) { toast(ex.message, "err"); }
});

$("#btn-forgot").addEventListener("click", async () => {
  const v = $("#forgot-confirm").value.trim();
  if (v !== "DELETE") return toast("请输入 DELETE 确认", "err");
  if (!confirm("这将清空全部服务器配置与凭据，且无法恢复！确认？")) return;
  try {
    await api("/api/auth/forgot", { method: "POST", body: { confirm: "DELETE" } });
    toast("已清空，可重新设置主密码");
    $("#forgot-confirm").value = "";
    state.configured = false;
    state.authenticated = false;
    checkAuth();
  } catch (ex) { toast(ex.message, "err"); }
});

// ---------------- modal ----------------
function openModal(html) {
  $("#modal-card").innerHTML = html;
  $("#modal").classList.remove("hidden");
}
function closeModal() {
  $("#modal").classList.add("hidden");
}
$("#modal").addEventListener("click", (e) => {
  if (e.target === $("#modal")) closeModal();
});

// ---------------- 启动 ----------------
checkAuth();
