package deepseekcpa

// keysResourcePath serves the browser page for managing DeepSeek credentials.
// The host mounts it at /v0/resource/plugins/deepseek-cpa/keys, which is not
// management-authenticated, so the page itself carries no data: it asks for the
// management key once, stores it in this origin's localStorage, and calls the
// authenticated /v0/management routes from there.
// The page lives at its own path: the host strips the mount prefix before the
// plugin sees it, so sharing "/keys" with the management route would make the
// two indistinguishable.
const keysResourcePath = "/credentials"

const keysPage = `<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>DeepSeek Keys</title>
<style>
  :root { color-scheme: light dark; }
  body { font: 14px/1.5 system-ui, -apple-system, "Segoe UI", sans-serif; margin: 0; padding: 24px; max-width: 780px; }
  h1 { font-size: 18px; margin: 0 0 4px; }
  p.hint { margin: 0 0 20px; opacity: .7; }
  section { border: 1px solid rgba(127,127,127,.35); border-radius: 8px; padding: 16px; margin-bottom: 16px; }
  h2 { font-size: 14px; margin: 0 0 12px; font-weight: 600; }
  label { display: block; font-size: 12px; opacity: .8; margin-bottom: 4px; }
  input { font: inherit; padding: 7px 9px; border-radius: 6px; border: 1px solid rgba(127,127,127,.5); background: transparent; width: 100%; box-sizing: border-box; }
  .row { display: flex; gap: 8px; align-items: flex-end; }
  .row > div { flex: 1; }
  button { font: inherit; padding: 7px 14px; border-radius: 6px; border: 1px solid rgba(127,127,127,.5); background: rgba(127,127,127,.12); cursor: pointer; }
  button:hover:not(:disabled) { background: rgba(127,127,127,.22); }
  button:disabled { opacity: .5; cursor: default; }
  button.danger { border-color: rgba(220,80,80,.6); }
  table { width: 100%; border-collapse: collapse; font-size: 13px; }
  th, td { text-align: left; padding: 7px 8px; border-bottom: 1px solid rgba(127,127,127,.25); }
  th { font-weight: 600; font-size: 12px; opacity: .75; }
  td.mono { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-size: 12px; }
  #status { min-height: 20px; font-size: 13px; }
  #status.error { color: #d9534f; }
  #status.ok { color: #3c9d5d; }
  .empty { opacity: .6; font-size: 13px; }
</style>
</head>
<body>
<h1>DeepSeek Keys</h1>
<p class="hint">密钥由 CLIProxyAPI 托管，保存在宿主的凭证目录里。本页只是调用管理接口的界面，密钥本身不会出现在页面里。</p>

<section>
  <h2>管理密钥</h2>
  <div class="row">
    <div>
      <label for="mgmt">CLIProxyAPI 管理密钥（只需填一次，存在本机浏览器）</label>
      <input id="mgmt" type="password" autocomplete="off" placeholder="management secret-key">
    </div>
    <button id="save-mgmt">保存</button>
  </div>
</section>

<section>
  <h2>现有密钥</h2>
  <div id="keys" class="empty">尚未加载</div>
</section>

<section>
  <h2>添加密钥</h2>
  <div class="row">
    <div>
      <label for="apikey">DeepSeek API key</label>
      <input id="apikey" type="password" autocomplete="off" placeholder="sk-...">
    </div>
    <button id="add">添加</button>
  </div>
</section>

<div id="status"></div>

<script>
(function () {
  var PLUGIN = "deepseek-cpa";
  var STORE = "deepseek-cpa:management-key";
  var BASE = "/v0/management/plugins/" + PLUGIN + "/keys";

  var mgmtInput = document.getElementById("mgmt");
  var apiInput = document.getElementById("apikey");
  var keysBox = document.getElementById("keys");
  var status = document.getElementById("status");

  function setStatus(text, kind) {
    status.textContent = text;
    status.className = kind || "";
  }

  function managementKey() {
    return mgmtInput.value || localStorage.getItem(STORE) || "";
  }

  function call(method, path, body) {
    var key = managementKey();
    if (!key) {
      return Promise.reject(new Error("请先填写管理密钥"));
    }
    var options = { method: method, headers: { "X-Management-Key": key } };
    if (body !== undefined) {
      options.headers["Content-Type"] = "application/json";
      options.body = JSON.stringify(body);
    }
    return fetch(path, options).then(function (response) {
      return response.text().then(function (text) {
        var payload = null;
        try { payload = text ? JSON.parse(text) : null; } catch (e) { payload = null; }
        if (!response.ok) {
          var message = (payload && (payload.error || payload.message)) || text || ("HTTP " + response.status);
          if (typeof message !== "string") { message = JSON.stringify(message); }
          throw new Error(message);
        }
        return payload;
      });
    });
  }

  function renderKeys(keys) {
    keysBox.textContent = "";
    keysBox.className = "";
    if (!keys || !keys.length) {
      keysBox.className = "empty";
      keysBox.textContent = "还没有添加任何密钥。";
      return;
    }
    var table = document.createElement("table");
    var head = document.createElement("tr");
    ["名称", "状态", "可用", ""].forEach(function (title) {
      var th = document.createElement("th");
      th.textContent = title;
      head.appendChild(th);
    });
    table.appendChild(head);
    keys.forEach(function (item) {
      var row = document.createElement("tr");
      var name = document.createElement("td");
      name.className = "mono";
      name.textContent = item.name || "";
      row.appendChild(name);
      var st = document.createElement("td");
      st.textContent = item.status || (item.disabled ? "disabled" : "active");
      row.appendChild(st);
      var avail = document.createElement("td");
      avail.textContent = item.available ? "是" : "否";
      row.appendChild(avail);
      var action = document.createElement("td");
      var remove = document.createElement("button");
      remove.className = "danger";
      remove.textContent = "删除";
      remove.addEventListener("click", function () {
        if (!confirm("删除 " + (item.name || "该密钥") + "？")) { return; }
        remove.disabled = true;
        call("DELETE", BASE + "?name=" + encodeURIComponent(item.name))
          .then(function () { setStatus("已删除 " + item.name, "ok"); return load(); })
          .catch(function (error) { remove.disabled = false; setStatus(error.message, "error"); });
      });
      action.appendChild(remove);
      row.appendChild(action);
      table.appendChild(row);
    });
    keysBox.appendChild(table);
  }

  function load() {
    if (!managementKey()) {
      keysBox.className = "empty";
      keysBox.textContent = "填写管理密钥后加载。";
      return Promise.resolve();
    }
    return call("GET", BASE)
      .then(function (payload) { renderKeys(payload && payload.keys); })
      .catch(function (error) {
        keysBox.className = "empty";
        keysBox.textContent = "加载失败。";
        setStatus(error.message, "error");
      });
  }

  document.getElementById("save-mgmt").addEventListener("click", function () {
    var value = mgmtInput.value.trim();
    if (!value) { setStatus("管理密钥不能为空", "error"); return; }
    localStorage.setItem(STORE, value);
    setStatus("管理密钥已保存在本机浏览器", "ok");
    load();
  });

  document.getElementById("add").addEventListener("click", function () {
    var apiKey = apiInput.value.trim();
    if (!apiKey) { setStatus("请填写 DeepSeek API key", "error"); return; }
    var button = document.getElementById("add");
    button.disabled = true;
    call("POST", BASE, { api_key: apiKey })
      .then(function (payload) {
        apiInput.value = "";
        setStatus("已添加 " + (payload && payload.name ? payload.name : "密钥"), "ok");
        return load();
      })
      .catch(function (error) { setStatus(error.message, "error"); })
      .then(function () { button.disabled = false; });
  });

  var saved = localStorage.getItem(STORE);
  if (saved) { mgmtInput.value = saved; }
  load();
})();
</script>
</body>
</html>
`
