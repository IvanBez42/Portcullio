"use strict";

const test = require("node:test");
const assert = require("node:assert/strict");
const crypto = require("node:crypto");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");

const tls = require("./tls");

// Fresh empty dir per test so generated certs never leak between cases //
function tmpDir() {
  return fs.mkdtempSync(path.join(os.tmpdir(), "portcullio-tls-test-"));
}

test("parseMode defaults to on and accepts on/off", () => {
  assert.equal(tls.parseMode(undefined), true);
  assert.equal(tls.parseMode(""), true);
  assert.equal(tls.parseMode("on"), true);
  assert.equal(tls.parseMode("off"), false);
});

test("parseMode rejects anything else instead of guessing", () => {
  for (const value of ["true", "false", "OFF", "0", "yes"]) {
    assert.throws(() => tls.parseMode(value), /invalid PORTCULLIO_TLS/);
  }
});

test("loadOrCreate generates a self-signed cert on first run", () => {
  const dir = tmpDir();
  const creds = tls.loadOrCreate({ certPath: "", keyPath: "", dir });

  const cert = new crypto.X509Certificate(creds.cert);
  assert.match(cert.subjectAltName, /DNS:localhost/);
  assert.match(cert.subjectAltName, /IP Address:127\.0\.0\.1/);
  assert.equal(cert.checkPrivateKey(crypto.createPrivateKey(creds.key)), true);

  const keyMode = fs.statSync(path.join(dir, tls.KEY_FILE)).mode & 0o777;
  assert.equal(keyMode, 0o600);
  assert.deepEqual(
    fs.readdirSync(dir).sort(),
    [tls.CERT_FILE, tls.KEY_FILE].sort(),
  );
});

test("loadOrCreate reuses the generated cert on later runs", () => {
  const dir = tmpDir();
  const first = tls.loadOrCreate({ certPath: "", keyPath: "", dir });
  const second = tls.loadOrCreate({ certPath: "", keyPath: "", dir });
  assert.equal(second.cert, first.cert);
  assert.equal(second.key, first.key);
});

test("loadOrCreate prefers a user-supplied cert and key", () => {
  const supplied = tmpDir();
  const own = tls.loadOrCreate({ certPath: "", keyPath: "", dir: supplied });

  const dir = tmpDir();
  const creds = tls.loadOrCreate({
    certPath: path.join(supplied, tls.CERT_FILE),
    keyPath: path.join(supplied, tls.KEY_FILE),
    dir,
  });
  assert.equal(creds.cert, own.cert);
  assert.equal(creds.key, own.key);
  assert.deepEqual(fs.readdirSync(dir), []);
});

test("loadOrCreate refuses a cert path without a key path, and vice versa", () => {
  const dir = tmpDir();
  assert.throws(
    () => tls.loadOrCreate({ certPath: "/x.crt", keyPath: "", dir }),
    /must be set together/,
  );
  assert.throws(
    () => tls.loadOrCreate({ certPath: "", keyPath: "/x.key", dir }),
    /must be set together/,
  );
});
