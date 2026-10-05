"use strict";

const crypto = require("node:crypto");
const fs = require("node:fs");
const path = require("node:path");
const { execFileSync } = require("node:child_process");
const state = require("./state");

const CERT_FILE = "tls.crt";
const KEY_FILE = "tls.key";
const CERT_VALID_DAYS = 3650; // 10y //
const OPENSSL = "/usr/bin/openssl"; // absolute so $PATH can't swap in a fake binary //

function parseMode(value) {
  if (value === "on") return true;
  if (value === undefined || value === "" || value === "off") return false;
  throw new Error(
    `tls: invalid PORTCULLIO_TLS=${JSON.stringify(value)} (expected "on" or "off")`,
  );
}

const enabled = parseMode(process.env.PORTCULLIO_TLS);

function isExpired(certPem) {
  return Date.now() > Date.parse(new crypto.X509Certificate(certPem).validTo);
}

// Writes a fresh self-signed cert+key into dir, swapped in atomically //
function generate(dir) {
  fs.mkdirSync(dir, { recursive: true });
  const suffix = `.tmp-${process.pid}-${Date.now()}`;
  const tmpKey = path.join(dir, KEY_FILE + suffix);
  const tmpCert = path.join(dir, CERT_FILE + suffix);
  try {
    execFileSync(
      OPENSSL,
      [
        "req",
        "-x509",
        "-nodes",
        "-newkey",
        "ec",
        "-pkeyopt",
        "ec_paramgen_curve:prime256v1",
        "-keyout",
        tmpKey,
        "-out",
        tmpCert,
        "-days",
        String(CERT_VALID_DAYS),
        "-subj",
        "/CN=portcullio",
        "-addext",
        "subjectAltName=DNS:localhost,IP:127.0.0.1",
      ],
      { stdio: ["ignore", "ignore", "pipe"] },
    );
    fs.chmodSync(tmpKey, 0o600);
    fs.renameSync(tmpKey, path.join(dir, KEY_FILE));
    fs.renameSync(tmpCert, path.join(dir, CERT_FILE));
  } catch (err) {
    fs.rmSync(tmpKey, { force: true });
    fs.rmSync(tmpCert, { force: true });
    if (err.code === "ENOENT") {
      throw new Error(
        `tls: ${OPENSSL} not found; install openssl there, set PORTCULLIO_TLS_CERT and PORTCULLIO_TLS_KEY, or set PORTCULLIO_TLS=off`,
      );
    }
    throw new Error(`tls: generate self-signed certificate: ${err.message}`);
  }
}

// User-supplied cert/key if both paths are set, else the generated one in dir //
function loadOrCreate({
  certPath = process.env.PORTCULLIO_TLS_CERT,
  keyPath = process.env.PORTCULLIO_TLS_KEY,
  dir = state.STATE_DIR,
} = {}) {
  if (certPath || keyPath) {
    if (!certPath || !keyPath) {
      throw new Error(
        "tls: PORTCULLIO_TLS_CERT and PORTCULLIO_TLS_KEY must be set together",
      );
    }
    const cert = fs.readFileSync(certPath, "utf8");
    if (isExpired(cert))
      console.warn("portcullio ui: warning: certificate from PORTCULLIO_TLS_CERT has expired");
    return { cert, key: fs.readFileSync(keyPath, "utf8") };
  }

  const genCert = path.join(dir, CERT_FILE);
  const genKey = path.join(dir, KEY_FILE);
  const exists = fs.existsSync(genCert) && fs.existsSync(genKey);
  if (!exists || isExpired(fs.readFileSync(genCert, "utf8"))) {
    generate(dir);
    console.log(
      `portcullio ui: generated self-signed TLS certificate in ${dir}`,
    );
  }
  return {
    cert: fs.readFileSync(genCert, "utf8"),
    key: fs.readFileSync(genKey, "utf8"),
  };
}

module.exports = { enabled, parseMode, loadOrCreate, CERT_FILE, KEY_FILE };
