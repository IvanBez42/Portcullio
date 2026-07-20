"use strict";

const crypto = require("crypto");
const state = require("./state");

const SCRYPT_KEYLEN = 64;
const SESSION_TTL_MS = 24 * 60 * 60 * 1000; // 24h
const SESSION_COOKIE = "portcullio_session";

const RECOVERY_CODE_TTL_MS = 30 * 60 * 1000; // 30m
const RECOVERY_CODE_COOLDOWN_MS = 10 * 60 * 1000; // 10m Recovery Code cooldown
const RECOVERY_CODE_ALPHABET = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"; // No 0/O/1/I/L making it easier to read
const RECOVERY_CODE_LENGTH = 10;

function hashPassword(password) {
  const salt = crypto.randomBytes(16);
  const hash = crypto.scryptSync(password, salt, SCRYPT_KEYLEN);
  return { salt: salt.toString("base64"), hash: hash.toString("base64") };
}

function verifyPassword(password, record) {
  const salt = Buffer.from(record.salt, "base64");
  const expected = Buffer.from(record.hash, "base64");
  const actual = crypto.scryptSync(password, salt, SCRYPT_KEYLEN);
  return (
    actual.length === expected.length &&
    crypto.timingSafeEqual(actual, expected)
  );
}

function hasAdmin() {
  return state.load().admin !== null;
}

// Initial password set //
function setAdminPassword(password) {
  const s = state.load();
  if (s.admin !== null) {
    throw new Error("auth: admin password already set");
  }
  s.admin = hashPassword(password);
  state.save(s);
}

function checkAdminPassword(password) {
  const s = state.load();
  if (!s.admin) return false;
  return verifyPassword(password, s.admin);
}

function randomRecoveryCode() {
  let raw = "";
  for (let i = 0; i < RECOVERY_CODE_LENGTH; i++) {
    raw +=
      RECOVERY_CODE_ALPHABET[crypto.randomInt(RECOVERY_CODE_ALPHABET.length)];
  }
  return raw;
}

// Cleans input //
function normalizeRecoveryCode(input) {
  return String(input || "")
    .toUpperCase()
    .replace(/[^A-Z0-9]/g, "");
}

// Generate Recovery Code //
function createRecoveryCode() {
  const s = state.load();
  if (s.recoveryCode) {
    const generatedAt = s.recoveryCode.expiresAt - RECOVERY_CODE_TTL_MS;
    if (Date.now() - generatedAt < RECOVERY_CODE_COOLDOWN_MS) {
      return null;
    }
  }
  const raw = randomRecoveryCode();
  s.recoveryCode = {
    ...hashPassword(raw),
    expiresAt: Date.now() + RECOVERY_CODE_TTL_MS,
  };
  state.save(s);
  return raw.match(/.{1,5}/g).join("-");
}

// Logs the recovery code to stdout //
function logRecoveryCode(code) {
  console.log("");
  console.log(
    "Portcullio admin password recovery code (valid 30 minutes, single use):",
  );
  console.log("");
  console.log("    " + code);
  console.log("");
  console.log(
    "Enter it at /recover in the web UI along with a new admin password.",
  );
  console.log("");
}

// Set new password with recovery code //
function consumeRecoveryCode(inputCode, newPassword) {
  const s = state.load();
  const record = s.recoveryCode;
  if (!record || Date.now() > record.expiresAt) {
    if (record) {
      delete s.recoveryCode;
      state.save(s);
    }
    return false;
  }
  if (!verifyPassword(normalizeRecoveryCode(inputCode), record)) return false;
  s.admin = hashPassword(newPassword);
  delete s.recoveryCode;
  s.sessions = {};
  state.save(s);
  return true;
}

function createSession() {
  const token = crypto.randomBytes(32).toString("hex");
  const s = state.load();
  s.sessions[token] = { expiresAt: Date.now() + SESSION_TTL_MS };
  state.save(s);
  return token;
}

function destroySession(token) {
  const s = state.load();
  delete s.sessions[token];
  state.save(s);
}

// Remove old sessions //
function isValidSession(token) {
  if (!token) return false;
  const s = state.load();
  const record = s.sessions[token];
  if (!record) return false;
  if (Date.now() > record.expiresAt) {
    delete s.sessions[token];
    state.save(s);
    return false;
  }
  return true;
}

// Go to login if not logged in //
function requireAuth(req, res, next) {
  const token = req.cookies && req.cookies[SESSION_COOKIE];
  if (!isValidSession(token)) {
    return res.redirect(302, "/login");
  }
  next();
}

module.exports = {
  SESSION_COOKIE,
  hasAdmin,
  setAdminPassword,
  checkAdminPassword,
  createSession,
  destroySession,
  isValidSession,
  requireAuth,
  createRecoveryCode,
  consumeRecoveryCode,
  logRecoveryCode,
};
