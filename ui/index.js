"use strict";

const path = require("path");
const express = require("express");
const rateLimit = require("express-rate-limit");
const cookieParser = require("cookie-parser");
const auth = require("./auth");
const state = require("./state");
const views = require("./views");
const agentClient = require("./agentClient");
const loginThrottle = require("./loginThrottle");
const csrf = require("./csrf");

const PORT = process.env.PORT || 8080;

// Fixed socket path //
const AGENT_SOCKET_PATH = "/socket/agent.sock";

const VAULT_ID_PATTERN = /^[a-zA-Z0-9_][a-zA-Z0-9_-]{0,63}$/;

const app = express();

// Coarse per-IP cap on /login -- loginThrottle.js already locks out repeated bad passwords //
const loginRateLimit = rateLimit({
  windowMs: 15 * 60 * 1000,
  limit: 30,
  standardHeaders: true,
  legacyHeaders: false,
});

// prevent iframe //
app.use((req, res, next) => {
  res.setHeader("X-Frame-Options", "DENY");
  res.setHeader("Content-Security-Policy", "frame-ancestors 'none'");
  next();
});

app.use(cookieParser());
app.use(express.urlencoded({ extended: false }));
app.use(express.static(path.join(__dirname, "public")));
app.use(csrf.doubleCsrfProtection);

app.get("/", (req, res) => {
  if (!auth.hasAdmin()) return res.redirect(302, "/setup");
  const token = req.cookies[auth.SESSION_COOKIE];
  if (auth.isValidSession(token)) return res.redirect(302, "/dashboard");
  return res.redirect(302, "/login");
});

// Setup only works if no admin //
app.get("/setup", (req, res) => {
  if (auth.hasAdmin())
    return res.status(404).type("html").send(views.notFoundPage());
  res.type("html").send(views.setupPage(null, req.csrfToken()));
});

app.post("/setup", (req, res) => {
  if (auth.hasAdmin())
    return res.status(404).type("html").send(views.notFoundPage());
  const { password, confirm } = req.body;
  if (!password || password.length < 8) {
    return res
      .status(400)
      .type("html")
      .send(
        views.setupPage(
          "Password must be at least 8 characters.",
          req.csrfToken(),
        ),
      );
  }
  if (password !== confirm) {
    return res
      .status(400)
      .type("html")
      .send(views.setupPage("Passwords do not match.", req.csrfToken()));
  }
  auth.setAdminPassword(password);
  res.redirect(302, "/login");
});

app.get("/login", loginRateLimit, (req, res) => {
  if (!auth.hasAdmin()) return res.redirect(302, "/setup");
  res.type("html").send(views.loginPage(null, req.csrfToken()));
});

app.post("/login", loginRateLimit, (req, res) => {
  if (!auth.hasAdmin()) return res.redirect(302, "/setup");
  const lockedMs = loginThrottle.msUntilUnlocked(req.ip);
  if (lockedMs > 0) {
    const minutes = Math.ceil(lockedMs / 60000);
    return res
      .status(429)
      .type("html")
      .send(
        views.loginPage(
          `Too many failed attempts. Try again in ${minutes} minute${minutes === 1 ? "" : "s"}.`,
          req.csrfToken(),
        ),
      );
  }
  if (!auth.checkAdminPassword(req.body.password || "")) {
    loginThrottle.recordFailure(req.ip);
    return res
      .status(401)
      .type("html")
      .send(views.loginPage("Incorrect password.", req.csrfToken()));
  }
  loginThrottle.recordSuccess(req.ip);
  const token = auth.createSession();
  res.cookie(auth.SESSION_COOKIE, token, {
    httpOnly: true,
    sameSite: "strict",
    path: "/",
  });
  res.redirect(302, "/dashboard");
});

app.post("/logout", auth.requireAuth, (req, res) => {
  auth.destroySession(req.cookies[auth.SESSION_COOKIE]);
  res.clearCookie(auth.SESSION_COOKIE);
  res.redirect(302, "/login");
});

app.get("/recover", (req, res) => {
  res.type("html").send(views.recoverPage(null, false, req.csrfToken()));
});

app.post("/recover/generate", (req, res) => {
  const code = auth.createRecoveryCode();
  if (code === null) {
    return res
      .status(429)
      .type("html")
      .send(
        views.recoverPage(
          "A code was already generated in the last 10 minutes -- check this container's log, or wait before generating a new one.",
          false,
          req.csrfToken(),
        ),
      );
  }
  auth.logRecoveryCode(code);
  res.type("html").send(views.recoverPage(null, true, req.csrfToken()));
});

app.post("/recover", (req, res) => {
  const { code, password, confirm } = req.body;
  if (!code) {
    return res
      .status(400)
      .type("html")
      .send(
        views.recoverPage("Recovery code is required.", false, req.csrfToken()),
      );
  }
  if (!password || password.length < 8) {
    return res
      .status(400)
      .type("html")
      .send(
        views.recoverPage(
          "Password must be at least 8 characters.",
          false,
          req.csrfToken(),
        ),
      );
  }
  if (password !== confirm) {
    return res
      .status(400)
      .type("html")
      .send(
        views.recoverPage("Passwords do not match.", false, req.csrfToken()),
      );
  }
  if (!auth.consumeRecoveryCode(code, password)) {
    return res
      .status(400)
      .type("html")
      .send(
        views.recoverPage(
          "Invalid or expired recovery code.",
          false,
          req.csrfToken(),
        ),
      );
  }
  res.redirect(302, "/login");
});

// Renders vault status and linked services //
async function renderDashboard(req, res, error) {
  try {
    const statusResp = await agentClient.callAgent(AGENT_SOCKET_PATH, {
      verb: agentClient.VERB_STATUS,
    });
    if (!statusResp.ok) {
      return res.type("html").send(
        views.dashboardPage({
          vaults: [],
          error: error || statusResp.error,
          csrfToken: req.csrfToken(),
        }),
      );
    }
    const vaults = (statusResp.vaults || []).map((v) => ({
      ...v,
      services: state.getVaultServices(v.vault_id),
    }));
    res
      .type("html")
      .send(views.dashboardPage({ vaults, error, csrfToken: req.csrfToken() }));
  } catch (err) {
    res.type("html").send(
      views.dashboardPage({
        vaults: [],
        error: `Could not reach agent: ${err.message}`,
        csrfToken: req.csrfToken(),
      }),
    );
  }
}

// Render disk space left //
async function renderNewVault(req, res, error) {
  try {
    const spaceResp = await agentClient.callAgent(AGENT_SOCKET_PATH, {
      verb: agentClient.VERB_SPACE,
    });
    const availableMB = spaceResp.ok ? spaceResp.available_mb : null;
    res
      .type("html")
      .send(
        views.newVaultPage({ error, availableMB, csrfToken: req.csrfToken() }),
      );
  } catch (err) {
    res.type("html").send(
      views.newVaultPage({
        error: error || `Could not reach agent: ${err.message}`,
        csrfToken: req.csrfToken(),
      }),
    );
  }
}

app.get("/dashboard", auth.requireAuth, (req, res) => {
  renderDashboard(req, res);
});

app.get("/vaults/new", auth.requireAuth, (req, res) => {
  renderNewVault(req, res);
});

app.post("/vaults", auth.requireAuth, async (req, res) => {
  const { vault_id, size_mb, passphrase, confirm } = req.body;
  if (!VAULT_ID_PATTERN.test(vault_id || "")) {
    return renderNewVault(req, res, "Invalid vault ID.");
  }
  const sizeMB = parseInt(size_mb, 10);
  if (!Number.isInteger(sizeMB) || sizeMB < 32) {
    return renderNewVault(req, res, "Size must be at least 32 MB.");
  }
  if (!passphrase || passphrase.length < 8) {
    return renderNewVault(
      req,
      res,
      "Passphrase must be at least 8 characters.",
    );
  }
  if (passphrase !== confirm) {
    return renderNewVault(req, res, "Passphrases do not match.");
  }
  try {
    const spaceResp = await agentClient.callAgent(AGENT_SOCKET_PATH, {
      verb: agentClient.VERB_SPACE,
    });
    if (spaceResp.ok && sizeMB > spaceResp.available_mb) {
      return renderNewVault(
        req,
        res,
        `Size exceeds available space (${spaceResp.available_mb} MB free).`,
      );
    }
    const resp = await agentClient.callAgent(AGENT_SOCKET_PATH, {
      verb: agentClient.VERB_CREATE,
      vault_id,
      size_mb: sizeMB,
      passphrase: Buffer.from(passphrase, "utf8"),
    });
    if (!resp.ok) return renderNewVault(req, res, resp.error);
    res.redirect(302, "/dashboard");
  } catch (err) {
    renderNewVault(req, res, `Could not reach agent: ${err.message}`);
  }
});

app.post("/vaults/:id/unseal", auth.requireAuth, async (req, res) => {
  const vaultId = req.params.id;
  if (!VAULT_ID_PATTERN.test(vaultId)) {
    return renderDashboard(req, res, "Invalid vault ID.");
  }
  if (!req.body.passphrase) {
    return renderDashboard(req, res, "Passphrase is required.");
  }
  try {
    const resp = await agentClient.callAgent(AGENT_SOCKET_PATH, {
      verb: agentClient.VERB_UNSEAL,
      vault_id: vaultId,
      passphrase: Buffer.from(req.body.passphrase, "utf8"),
      services: state.getVaultServices(vaultId),
    });
    if (!resp.ok) return renderDashboard(req, res, resp.error);
    res.redirect(302, "/dashboard");
  } catch (err) {
    renderDashboard(req, res, `Could not reach agent: ${err.message}`);
  }
});

app.post("/vaults/:id/seal", auth.requireAuth, async (req, res) => {
  const vaultId = req.params.id;
  if (!VAULT_ID_PATTERN.test(vaultId)) {
    return renderDashboard(req, res, "Invalid vault ID.");
  }
  try {
    const resp = await agentClient.callAgent(AGENT_SOCKET_PATH, {
      verb: agentClient.VERB_SEAL,
      vault_id: vaultId,
      services: state.getVaultServices(vaultId),
    });
    if (!resp.ok) return renderDashboard(req, res, resp.error);
    res.redirect(302, "/dashboard");
  } catch (err) {
    renderDashboard(req, res, `Could not reach agent: ${err.message}`);
  }
});

async function renderSettings(req, res, vaultId, { error, saved } = {}) {
  try {
    const resp = await agentClient.callAgent(AGENT_SOCKET_PATH, {
      verb: agentClient.VERB_SERVICES,
    });
    if (!resp.ok) {
      return res.type("html").send(
        views.settingsPage({
          vaultId,
          availableServices: [],
          linkedServices: state.getVaultServices(vaultId),
          error: error || resp.error,
          csrfToken: req.csrfToken(),
        }),
      );
    }
    res.type("html").send(
      views.settingsPage({
        vaultId,
        availableServices: resp.services || [],
        linkedServices: state.getVaultServices(vaultId),
        error,
        saved,
        csrfToken: req.csrfToken(),
      }),
    );
  } catch (err) {
    res.type("html").send(
      views.settingsPage({
        vaultId,
        availableServices: [],
        linkedServices: state.getVaultServices(vaultId),
        error: `Could not reach agent: ${err.message}`,
        csrfToken: req.csrfToken(),
      }),
    );
  }
}

app.get("/vaults/:id/settings", auth.requireAuth, (req, res) => {
  const vaultId = req.params.id;
  if (!VAULT_ID_PATTERN.test(vaultId)) {
    return res.status(404).type("html").send(views.notFoundPage());
  }
  renderSettings(req, res, vaultId);
});

app.post("/vaults/:id/settings", auth.requireAuth, async (req, res) => {
  const vaultId = req.params.id;
  if (!VAULT_ID_PATTERN.test(vaultId)) {
    return res.status(404).type("html").send(views.notFoundPage());
  }

  const raw = req.body.services;
  const requested = Array.isArray(raw) ? raw : raw ? [raw] : [];

  try {
    const resp = await agentClient.callAgent(AGENT_SOCKET_PATH, {
      verb: agentClient.VERB_SERVICES,
    });
    if (!resp.ok)
      return renderSettings(req, res, vaultId, { error: resp.error });
    // Defense in depth only -- agent independently re-validates every service name regardless.
    const known = new Set(resp.services || []);
    const services = requested.filter((s) => known.has(s));
    state.setVaultServices(vaultId, services);
    renderSettings(req, res, vaultId, { saved: true });
  } catch (err) {
    renderSettings(req, res, vaultId, {
      error: `Could not reach agent: ${err.message}`,
    });
  }
});

app.post("/vaults/:id/destroy", auth.requireAuth, async (req, res) => {
  const vaultId = req.params.id;
  if (!VAULT_ID_PATTERN.test(vaultId)) {
    return res.status(404).type("html").send(views.notFoundPage());
  }
  const { confirm_id, admin_password } = req.body;
  if (confirm_id !== vaultId) {
    return renderSettings(req, res, vaultId, {
      error: "Retyped vault ID does not match.",
    });
  }
  if (!auth.checkAdminPassword(admin_password || "")) {
    return renderSettings(req, res, vaultId, {
      error: "Incorrect admin password.",
    });
  }
  try {
    const resp = await agentClient.callAgent(AGENT_SOCKET_PATH, {
      verb: agentClient.VERB_DESTROY,
      vault_id: vaultId,
    });
    if (!resp.ok)
      return renderSettings(req, res, vaultId, { error: resp.error });
    state.deleteVaultServices(vaultId);
    res.redirect(302, "/dashboard");
  } catch (err) {
    renderSettings(req, res, vaultId, {
      error: `Could not reach agent: ${err.message}`,
    });
  }
});

// CSRF token missing/invalid: report clearly instead of the default error page //
app.use((err, req, res, next) => {
  if (err && err.code === "EBADCSRFTOKEN") {
    return res.status(403).type("html").send(views.forbiddenPage());
  }
  next(err);
});

app.listen(PORT, () => {
  console.log(`portcullio ui: listening on :${PORT}`);
});
