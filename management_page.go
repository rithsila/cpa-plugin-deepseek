package deepseekcpa

// keysResourcePath serves the browser page for managing DeepSeek credentials.
// The host mounts it at /v0/resource/plugins/deepseek-cpa/credentials, which is not
// management-authenticated, so the page itself carries no data: it asks for the
// management key once, stores it in this origin's localStorage, and calls the
// authenticated /v0/management routes from there.
// The page lives at its own path: the host strips the mount prefix before the
// plugin sees it, so sharing "/keys" with the management route would make the
// two indistinguishable.
const keysResourcePath = "/credentials"

const keysPage = `<!doctype html>
<html lang="en">
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
<p class="hint">API keys are stored and managed by CLIProxyAPI in the host credentials directory. This page provides a management UI; API keys are never exposed in this interface.</p>

<section id="mgmt-section">
  <h2>Management Key</h2>
  <p class="hint" id="mgmt-note" style="margin:0 0 10px">CLIProxyAPI management key. If "Remember Password" was enabled during panel login, it is read automatically.</p>
  <div class="row" id="mgmt-form">
    <div>
      <label for="mgmt">Management Key</label>
      <input id="mgmt" type="password" autocomplete="off" placeholder="management secret-key">
    </div>
    <button id="save-mgmt">Save</button>
  </div>
  <button id="change-mgmt" style="display:none">Change Management Key</button>
</section>

<section>
  <h2>Existing Keys</h2>
  <div id="keys" class="empty">Not loaded yet</div>
</section>

<section>
  <h2>Add Key</h2>
  <div class="row">
    <div>
      <label for="apikey">DeepSeek API Key</label>
      <input id="apikey" type="password" autocomplete="off" placeholder="sk-...">
    </div>
    <button id="add">Add</button>
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

  // The management panel stores the key under localStorage["managementKey"],
  // obfuscated as "enc::v1::" + base64(XOR(utf8(JSON), key)) where key is derived
  // from the page host, the user agent and a fixed salt. Reading it here means a
  // user who ticked "remember password" in the panel never retypes it.
  var PANEL_STORE_KEY = "managementKey";
  var OBFUSCATION_PREFIX = "enc::v1::";
  var OBFUSCATION_SALT = "cli-proxy-api-webui::secure-storage";

  function panelManagementKey() {
    var raw;
    try {
      raw = localStorage.getItem(PANEL_STORE_KEY);
    } catch (e) {
      return "";
    }
    if (!raw || raw.indexOf(OBFUSCATION_PREFIX) !== 0) {
      return "";
    }
    try {
      var key = new TextEncoder().encode(
        OBFUSCATION_SALT + "|" + window.location.host + "|" + navigator.userAgent
      );
      var binary = atob(raw.slice(OBFUSCATION_PREFIX.length));
      var bytes = new Uint8Array(binary.length);
      for (var i = 0; i < binary.length; i++) {
        bytes[i] = binary.charCodeAt(i) ^ key[i % key.length];
      }
      var decoded = JSON.parse(new TextDecoder().decode(bytes));
      if (typeof decoded !== "string") {
        return "";
      }
      var value = decoded.trim();
      // A mismatched host or user agent turns the tail into garbage that can
      // still parse; only accept something shaped like a management key, because
      // a wrong guess costs one of the host's five failed attempts.
      if (!/^[\x21-\x7e]{4,256}$/.test(value)) {
        return "";
      }
      return value;
    } catch (e) {
      // A different panel version, a changed user agent, or a rotated key: fall
      // back to asking instead of guessing.
      return "";
    }
  }

  function managementKey() {
    return mgmtInput.value || panelManagementKey() || localStorage.getItem(STORE) || "";
  }

  function call(method, path, body) {
    var key = managementKey();
    if (!key) {
      return Promise.reject(new Error("Please enter the management key first"));
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
      keysBox.textContent = "No keys added yet.";
      return;
    }
    var table = document.createElement("table");
    var head = document.createElement("tr");
    ["Name", "Status", "Available", ""].forEach(function (title) {
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
      avail.textContent = item.available ? "Yes" : "No";
      row.appendChild(avail);
      var action = document.createElement("td");
      var remove = document.createElement("button");
      remove.className = "danger";
      remove.textContent = "Delete";
      remove.addEventListener("click", function () {
        if (!confirm("Delete " + (item.name || "this key") + "?")) { return; }
        remove.disabled = true;
        call("DELETE", BASE + "?name=" + encodeURIComponent(item.name))
          .then(function () { setStatus("Deleted " + item.name, "ok"); return load(); })
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
      keysBox.textContent = "Enter management key to load.";
      showKeyForm();
      return Promise.resolve();
    }
    return call("GET", BASE)
      .then(function (payload) {
        renderKeys(payload && payload.keys);
        hideKeyForm();
      })
      .catch(function (error) {
        keysBox.className = "empty";
        keysBox.textContent = "Failed to load.";
        setStatus(error.message, "error");
        showKeyForm("Invalid or changed management key. Please re-enter.");
      });
  }

  var mgmtForm = document.getElementById("mgmt-form");
  var mgmtNote = document.getElementById("mgmt-note");
  var changeButton = document.getElementById("change-mgmt");

  function showKeyForm(note) {
    mgmtForm.style.display = "";
    changeButton.style.display = "none";
    mgmtNote.textContent = note || "CLIProxyAPI management key. Saved locally in this browser.";
  }

  function hideKeyForm() {
    mgmtForm.style.display = "none";
    changeButton.style.display = "";
    mgmtNote.textContent = panelManagementKey()
      ? "Using remembered management key from the panel."
      : "Using locally saved management key.";
  }

  document.getElementById("save-mgmt").addEventListener("click", function () {
    var value = mgmtInput.value.trim();
    if (!value) { setStatus("Management key cannot be empty", "error"); return; }
    localStorage.setItem(STORE, value);
    setStatus("Management key saved in local browser", "ok");
    load().then(function () {
      // Only hide the form once the stored key actually worked: a wrong key
      // counts toward the host's failed-attempt ban, so it must stay visible.
      if (status.className === "ok") { hideKeyForm(); } else { showKeyForm("Invalid key. Please re-enter."); }
    });
  });

  changeButton.addEventListener("click", function () {
    mgmtInput.value = "";
    localStorage.removeItem(STORE);
    showKeyForm("Enter a new management key.");
    mgmtInput.focus();
  });

  document.getElementById("add").addEventListener("click", function () {
    var apiKey = apiInput.value.trim();
    if (!apiKey) { setStatus("Please enter a DeepSeek API key", "error"); return; }
    var button = document.getElementById("add");
    button.disabled = true;
    call("POST", BASE, { api_key: apiKey })
      .then(function (payload) {
        apiInput.value = "";
        setStatus("Added " + (payload && payload.name ? payload.name : "key"), "ok");
        return load();
      })
      .catch(function (error) { setStatus(error.message, "error"); })
      .then(function () { button.disabled = false; });
  });

  var fromPanel = panelManagementKey();
  var saved = localStorage.getItem(STORE);
  if (fromPanel) {
    mgmtForm.style.display = "none";
    mgmtNote.textContent = "Using remembered management key from the panel.";
    load();
  } else if (saved) {
    mgmtInput.value = saved;
    mgmtForm.style.display = "none";
    mgmtNote.textContent = "Using locally saved management key...";
    load();
  } else {
    showKeyForm();
    load();
  }
})();
</script>
</body>
</html>
`
