"use strict";

const test = require("node:test");
const assert = require("node:assert/strict");
const net = require("net");
const fs = require("fs");
const os = require("os");
const path = require("path");

const { callAgent, VERB_UNSEAL, VERB_STATUS } = require("./agentClient");

// Bare Unix-socket server speaking internal/socket's protocol //
function startFakeAgent(handleRequest) {
  const socketPath = path.join(
    fs.mkdtempSync(path.join(os.tmpdir(), "portcullio-ui-test-")),
    "agent.sock",
  );
  const server = net.createServer((conn) => {
    let buffered = "";
    conn.on("data", (chunk) => {
      buffered += chunk.toString("utf8");
      const newlineIndex = buffered.indexOf("\n");
      if (newlineIndex === -1) return;
      const req = JSON.parse(buffered.slice(0, newlineIndex));
      conn.end(JSON.stringify(handleRequest(req)) + "\n");
    });
  });
  return new Promise((resolve) => {
    server.listen(socketPath, () => resolve({ server, socketPath }));
  });
}

// A Buffer passphrase goes out as base64, then gets wiped from memory right after //
test("callAgent sends the verb and base64-encodes a Buffer passphrase, then zeroes it", async () => {
  let seen;
  const { server, socketPath } = await startFakeAgent((req) => {
    seen = req;
    return { ok: true, state: "unsealed" };
  });
  try {
    const passphrase = Buffer.from("hunter2-but-longer");
    const resp = await callAgent(socketPath, {
      verb: VERB_UNSEAL,
      vault_id: "demo",
      passphrase,
      services: ["service-a"],
    });
    assert.equal(resp.ok, true);
    assert.equal(resp.state, "unsealed");
    assert.equal(seen.verb, VERB_UNSEAL);
    assert.equal(seen.vault_id, "demo");
    assert.deepEqual(seen.services, ["service-a"]);
    assert.equal(
      Buffer.from(seen.passphrase, "base64").toString(),
      "hunter2-but-longer",
    );
    assert.equal(
      passphrase.every((b) => b === 0),
      true,
    );
  } finally {
    server.close();
  }
});

// Fields you don't pass shouldn't sneak into the request at all //
test("callAgent omits passphrase/services/size_mb when not provided", async () => {
  let seen;
  const { server, socketPath } = await startFakeAgent((req) => {
    seen = req;
    return { ok: true, vaults: [] };
  });
  try {
    const resp = await callAgent(socketPath, { verb: VERB_STATUS });
    assert.equal(resp.ok, true);
    assert.deepEqual(resp.vaults, []);
    assert.equal("passphrase" in seen, false);
    assert.equal("services" in seen, false);
    assert.equal("size_mb" in seen, false);
  } finally {
    server.close();
  }
});

// A string password can't be safely wiped from memory -- rejected before it's even sent //
test("callAgent rejects a string passphrase outright, since a string can never be zeroed", async () => {
  const { server, socketPath } = await startFakeAgent(() => ({ ok: true }));
  try {
    await assert.rejects(
      callAgent(socketPath, {
        verb: VERB_UNSEAL,
        vault_id: "demo",
        passphrase: "not-a-buffer",
      }),
      /must be a Buffer/,
    );
  } finally {
    server.close();
  }
});

// An error reported by the agent is a normal reply, not a broken connection //
test("callAgent resolves (not rejects) an application-level ok:false response", async () => {
  const { server, socketPath } = await startFakeAgent(() => ({
    ok: false,
    error: "vault not found",
  }));
  try {
    const resp = await callAgent(socketPath, {
      verb: VERB_STATUS,
      vault_id: "missing",
    });
    assert.equal(resp.ok, false);
    assert.equal(resp.error, "vault not found");
  } finally {
    server.close();
  }
});
