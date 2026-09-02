"use strict";

const fs = require("fs");
const path = require("path");

const STATE_DIR = process.env.PORTCULLIO_STATE_DIR || "/state";
const STATE_FILE = path.join(STATE_DIR, "state.json");

function emptyState() {
  return {
    admin: null,
    sessions: {},
    vaultServices: {},
    recoveryCode: null,
    csrfSecret: null,
  };
}

// Parse or initialize state //
function load() {
  let raw;
  try {
    raw = fs.readFileSync(STATE_FILE, "utf8");
  } catch (err) {
    if (err.code === "ENOENT") return emptyState();
    throw err;
  }
  return JSON.parse(raw);
}

// Write changes atomicly to avoid corruption on crash //
function save(state) {
  fs.mkdirSync(STATE_DIR, { recursive: true });
  const tmpPath = path.join(
    STATE_DIR,
    `.state.json.tmp-${process.pid}-${Date.now()}`,
  );
  fs.writeFileSync(tmpPath, JSON.stringify(state, null, 2), { mode: 0o600 });
  fs.renameSync(tmpPath, STATE_FILE);
}

// Services linked to vaultId //
function getVaultServices(vaultId) {
  const s = load();
  return s.vaultServices[vaultId] || [];
}

function setVaultServices(vaultId, services) {
  const s = load();
  s.vaultServices[vaultId] = services;
  save(s);
}

// Remove process of vault delete //
function deleteVaultServices(vaultId) {
  const s = load();
  delete s.vaultServices[vaultId];
  save(s);
}

module.exports = {
  load,
  save,
  getVaultServices,
  setVaultServices,
  deleteVaultServices,
};
