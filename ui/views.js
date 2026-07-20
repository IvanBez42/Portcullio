"use strict";

function escapeHtml(s) {
  return String(s).replace(
    /[&<>"']/g,
    (c) =>
      ({
        "&": "&amp;",
        "<": "&lt;",
        ">": "&gt;",
        '"': "&quot;",
        "'": "&#39;",
      })[c],
  );
}

function layout(title, body) {
  return `<!doctype html><html><head><meta charset="utf-8"><title>${escapeHtml(title)}</title>
<link rel="icon" type="image/svg+xml" href="/favicon.svg">
<link rel="stylesheet" href="/style.css"></head>
<body>${body}</body></html>`;
}

const gearIcon = `<svg viewBox="0 0 24 24" xmlns="http://www.w3.org/2000/svg" aria-hidden="true"><path d="M19.14 12.94c.04-.3.06-.61.06-.94s-.02-.64-.07-.94l2.03-1.58a.5.5 0 0 0 .12-.64l-1.92-3.32a.5.5 0 0 0-.61-.22l-2.39.96a7.03 7.03 0 0 0-1.62-.94l-.36-2.54a.5.5 0 0 0-.5-.42h-3.84a.5.5 0 0 0-.5.42l-.36 2.54c-.59.24-1.13.56-1.62.94l-2.39-.96a.5.5 0 0 0-.61.22L2.64 8.84a.5.5 0 0 0 .12.64l2.03 1.58c-.05.3-.07.63-.07.94s.02.64.07.94l-2.03 1.58a.5.5 0 0 0-.12.64l1.92 3.32c.14.24.42.34.68.22l2.39-.96c.5.38 1.03.7 1.62.94l.36 2.54c.04.28.26.42.5.42h3.84c.24 0 .46-.14.5-.42l.36-2.54c.59-.24 1.13-.56 1.62-.94l2.39.96c.24.11.54 0 .68-.22l1.92-3.32a.5.5 0 0 0-.12-.64l-2.03-1.58zM12 15.5A3.5 3.5 0 1 1 12 8.5a3.5 3.5 0 0 1 0 7z"/></svg>`;
const backIcon = `<svg viewBox="0 0 24 24" xmlns="http://www.w3.org/2000/svg" aria-hidden="true"><path d="M20 11H7.83l5.59-5.59L12 4l-8 8 8 8 1.41-1.41L7.83 13H20v-2z"/></svg>`;

function backLink(href, label) {
  return `<a class="back-link" href="${href}">${backIcon}${escapeHtml(label)}</a>`;
}

const githubIcon = `<svg viewBox="0 0 16 16" xmlns="http://www.w3.org/2000/svg" aria-hidden="true"><path d="M8 0C3.58 0 0 3.58 0 8c0 3.54 2.29 6.53 5.47 7.59.4.07.55-.17.55-.38 0-.19-.01-.82-.01-1.49-2.01.37-2.53-.49-2.69-.94-.09-.23-.48-.94-.82-1.13-.28-.15-.68-.52-.01-.53.63-.01 1.08.58 1.23.82.72 1.21 1.87.87 2.33.66.07-.52.28-.87.51-1.07-1.78-.2-3.64-.89-3.64-3.95 0-.87.31-1.59.82-2.15-.08-.2-.36-1.02.08-2.12 0 0 .67-.21 2.2.82.64-.18 1.32-.27 2-.27.68 0 1.36.09 2 .27 1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82 1.27.82 2.15 0 3.07-1.87 3.75-3.65 3.95.29.25.54.73.54 1.48 0 1.07-.01 1.93-.01 2.2 0 .21.15.46.55.38A8.01 8.01 0 0 0 16 8c0-4.42-3.58-8-8-8z"/></svg>`;

const STATE_LABELS = {
  sealed: "Locked",
  unsealed: "Running",
  unsealing: "Unsealing",
  degraded: "Degraded",
};

function statusBadge(state) {
  return `<span class="status-badge status-${escapeHtml(state)}">${escapeHtml(STATE_LABELS[state] || state)}</span>`;
}

function serviceChips(services) {
  if (!services || services.length === 0) {
    return `<div class="linked-services"><span class="service-chip service-chip-empty">No services linked</span></div>`;
  }
  return `<div class="linked-services">${services.map((s) => `<span class="service-chip">${escapeHtml(s)}</span>`).join("")}</div>`;
}

function setupPage(error) {
  return layout(
    "Portcullio - initial setup",
    `
    <h1>Portcullio - initial setup</h1>
    ${error ? `<p class="error-text">${escapeHtml(error)}</p>` : ""}
    <form method="post" action="/setup" class="settings-form">
      <label>Admin password <input type="password" name="password" required minlength="8"></label>
      <label>Confirm <input type="password" name="confirm" required minlength="8"></label>
      <button type="submit">Set admin password</button>
    </form>
  `,
  );
}

function loginPage(error) {
  return layout(
    "Portcullio - log in",
    `
    <h1>Portcullio - log in</h1>
    ${error ? `<p class="error-text">${escapeHtml(error)}</p>` : ""}
    <form method="post" action="/login" class="settings-form stacked-form">
      <label for="login-password">Password</label>
      <input type="password" id="login-password" name="password" required>
      <button type="submit">Log in</button>
    </form>
    <p><a href="/recover">Forgot your password?</a></p>
  `,
  );
}

function recoverPage(error, generated) {
  return layout(
    "Portcullio - recover admin password",
    `
    <div class="page-header">
      <h1>Recover admin password</h1>
      ${backLink("/login", "Log in")}
    </div>
    ${generated ? `<p class="success-text">New code generated -- check this container's log (<code>docker compose logs ui</code>).</p>` : ""}
    ${error ? `<p class="error-text">${escapeHtml(error)}</p>` : ""}

    <h2>1. Get a code</h2>
    <form method="post" action="/recover/generate" class="settings-form">
      <p class="vault-detail">Receive a one-time code in the container's log by:</p>
      <div class="command-box" role="button" tabindex="0" aria-label="Copy command">
        <code>docker compose exec ui node generate-recovery-code.js</code>
        <span class="copy-hint">Copy</span>
      </div>
      <p class="vault-detail">or</p>
      <button type="submit">Generate a recovery code</button>
    </form>

    <h2>2. Reset Password</h2>
    <form method="post" action="/recover" class="settings-form stacked-form">
      <label for="recover-code">Recovery code</label>
      <input type="text" id="recover-code" name="code" required autocomplete="off">
      <hr class="field-divider">
      <label for="recover-password">New admin password</label>
      <input type="password" id="recover-password" name="password" required minlength="8">
      <label for="recover-confirm">Confirm</label>
      <input type="password" id="recover-confirm" name="confirm" required minlength="8">
      <button type="submit">Reset password</button>
    </form>
    <script src="/recover.js" defer></script>
  `,
  );
}

function notFoundPage() {
  return layout("Not found", `<h1>Not found</h1>`);
}

function vaultRow(vault) {
  const id = escapeHtml(vault.vault_id);
  const state = escapeHtml(vault.state);
  const gear = `<a class="gear-link" href="/vaults/${id}/settings" title="Settings" aria-label="${id} settings">${gearIcon}</a>`;
  const header = `<div class="vault-card-header"><h2 class="vault-id">${id}</h2>${gear}</div>`;

  let body;
  if (vault.state === "sealed") {
    body = `
      ${statusBadge(vault.state)}
      <form method="post" action="/vaults/${id}/unseal" class="unlock-form">
        <input type="password" name="passphrase" placeholder="Passphrase" required>
        <button type="submit">Unlock</button>
      </form>`;
  } else if (vault.state === "unsealed") {
    body = `
      ${statusBadge(vault.state)}
      ${usageDetail(vault.used_mb, vault.total_mb)}
      ${serviceChips(vault.services)}
      <form method="post" action="/vaults/${id}/seal" class="lock-form">
        <button type="submit">Lock</button>
      </form>`;
  } else {
    const detail = vault.detail ? escapeHtml(vault.detail) : "Needs attention.";
    body = `${statusBadge(vault.state)}<p class="vault-detail">${detail}</p>`;
  }

  return `<div class="vault-card" data-state="${state}">${header}${body}</div>`;
}

function formatMB(mb) {
  if (mb >= 1024) return (mb / 1024).toFixed(1) + " GB";
  return mb + " MB";
}

// Container Usage Bar //
function usageDetail(usedMB, totalMB) {
  if (!totalMB) return "";
  const pct = Math.min(100, Math.round(((usedMB || 0) / totalMB) * 100));
  const level =
    pct >= 90 ? "usage-danger" : pct >= 75 ? "usage-warn" : "usage-ok";
  return `
    <div class="usage-bar"><div class="usage-fill ${level}" style="width:${pct}%"></div></div>
    <p class="vault-detail">${formatMB(usedMB || 0)} of ${formatMB(totalMB)} used</p>`;
}

// Range slides with textbox //
function sizeControl(availableMB) {
  const known = typeof availableMB === "number" && availableMB > 0;
  const max = known ? availableMB : 1048576; // 1 TB fallback, unconstrained in practice
  const value = Math.min(64, max);
  const hint = known
    ? `${formatMB(availableMB)} free on the vault storage disk`
    : "Free space unavailable -- size isn't checked against it here.";
  return `
    <label for="size_mb">Size (MB)</label>
    <div class="size-control">
      <input type="range" id="size_mb_slider" min="32" max="${max}" value="${value}" step="1">
      <input type="number" id="size_mb" name="size_mb" min="32" max="${max}" value="${value}" required>
    </div>
    <p class="vault-detail">${hint}</p>`;
}

function dashboardPage({ vaults, error }) {
  const list = vaults.length
    ? `<div class="vault-grid">${vaults.map(vaultRow).join("")}</div>`
    : `<p>No vaults yet.</p>`;
  return layout(
    "Portcullio",
    `
    <div class="page-header">
      <div class="page-title">
        <h1>Portcullio</h1>
        <a class="github-link" href="https://github.com/IvanBez42/Portcullio" target="_blank" rel="noopener noreferrer" title="View on GitHub" aria-label="View on GitHub">${githubIcon}</a>
      </div>
      <div class="page-header-actions">
        <a class="new-vault-btn" href="/vaults/new">+ Vault</a>
        <form method="post" action="/logout"><button type="submit" class="logout-btn">Log out</button></form>
      </div>
    </div>
    ${error ? `<p class="error-text">${escapeHtml(error)}</p>` : ""}
    ${list}
  `,
  );
}

// New vault page //
function newVaultPage({ error, availableMB }) {
  return layout(
    "Portcullio - new vault",
    `
    <div class="page-header">
      <h1>New vault</h1>
      ${backLink("/dashboard", "Dashboard")}
    </div>
    ${error ? `<p class="error-text">${escapeHtml(error)}</p>` : ""}
    <form method="post" action="/vaults" class="new-vault-form">
      <label>Vault ID <input type="text" name="vault_id" pattern="[a-zA-Z0-9_][a-zA-Z0-9_-]{0,63}" required></label>
      ${sizeControl(availableMB)}
      <label>Passphrase <input type="password" name="passphrase" required minlength="8"></label>
      <label>Confirm <input type="password" name="confirm" required minlength="8"></label>
      <button type="submit">Create</button>
    </form>
    <script src="/dashboard.js" defer></script>
  `,
  );
}

// Vault Settings //
function settingsPage({
  vaultId,
  availableServices,
  linkedServices,
  error,
  saved,
}) {
  const id = escapeHtml(vaultId);
  const linkedSet = new Set(linkedServices);
  const checkboxes = availableServices.length
    ? availableServices
        .map((svc) => {
          const escSvc = escapeHtml(svc);
          const checked = linkedSet.has(svc) ? "checked" : "";
          return `<label><input type="checkbox" name="services" value="${escSvc}" ${checked}> ${escSvc}</label><br>`;
        })
        .join("")
    : "<p>No docker services declared in the compose file.</p>";

  return layout(
    `Portcullio - ${vaultId} settings`,
    `
    <div class="page-header">
      <h1>${id} - settings</h1>
      ${backLink("/dashboard", "Dashboard")}
    </div>
    ${error ? `<p class="error-text">${escapeHtml(error)}</p>` : ""}
    ${saved ? `<p class="success-text">Saved.</p>` : ""}

    <h2>Linked services</h2>
    <form method="post" action="/vaults/${id}/settings" class="settings-form">
      ${checkboxes}
      <button type="submit">Save</button>
    </form>

    <h2 class="danger-heading">Delete this vault</h2>
    <p>Permanently deletes the vault's backing file. Cannot be undone.</p>
    <form method="post" action="/vaults/${id}/destroy" class="settings-form">
      <label>Retype vault ID to confirm <input type="text" name="confirm_id" required></label>
      <label>Admin password <input type="password" name="admin_password" required></label>
      <button type="submit" class="danger-btn">Delete vault</button>
    </form>
  `,
  );
}

module.exports = {
  setupPage,
  loginPage,
  recoverPage,
  notFoundPage,
  dashboardPage,
  newVaultPage,
  settingsPage,
  escapeHtml,
};
