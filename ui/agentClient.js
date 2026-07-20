"use strict";

const net = require("net");

// Verb constants //
const VERB_STATUS = "status";
const VERB_UNSEAL = "unseal";
const VERB_SEAL = "seal";
const VERB_CREATE = "create";
const VERB_DESTROY = "destroy";
const VERB_SERVICES = "services";
const VERB_SPACE = "space";

const DEFAULT_TIMEOUT_MS = 30000;

// Single request handler over unix socket //
function callAgent(socketPath, request, timeoutMs = DEFAULT_TIMEOUT_MS) {
  return new Promise((resolve, reject) => {
    const wireRequest = { verb: request.verb };
    if (request.vault_id !== undefined) wireRequest.vault_id = request.vault_id;
    if (request.services !== undefined) wireRequest.services = request.services;
    if (request.size_mb !== undefined) wireRequest.size_mb = request.size_mb;
    if (request.passphrase !== undefined) {
      if (!Buffer.isBuffer(request.passphrase)) {
        reject(
          new Error("agentClient: passphrase must be a Buffer, not a string"),
        );
        return;
      }
      wireRequest.passphrase = request.passphrase.toString("base64");
      request.passphrase.fill(0);
    }

    const socket = net.createConnection({ path: socketPath });
    let buffered = "";
    let settled = false;

    const fail = (err) => {
      if (settled) return;
      settled = true;
      clearTimeout(timer);
      socket.destroy();
      reject(err);
    };
    const succeed = (value) => {
      if (settled) return;
      settled = true;
      clearTimeout(timer);
      socket.end();
      resolve(value);
    };

    const timer = setTimeout(
      () =>
        fail(
          new Error(
            `agentClient: ${request.verb} timed out after ${timeoutMs}ms`,
          ),
        ),
      timeoutMs,
    );
    timer.unref?.();

    socket.on("connect", () => {
      socket.write(JSON.stringify(wireRequest) + "\n");
    });
    socket.on("data", (chunk) => {
      buffered += chunk.toString("utf8");
      const newlineIndex = buffered.indexOf("\n");
      if (newlineIndex === -1) return;
      const line = buffered.slice(0, newlineIndex);
      try {
        succeed(JSON.parse(line));
      } catch (err) {
        fail(new Error(`agentClient: decode response: ${err.message}`));
      }
    });
    socket.on("error", (err) => {
      fail(new Error(`agentClient: ${err.message}`));
    });
    socket.on("close", () => {
      fail(
        new Error(
          "agentClient: connection closed before a response was received",
        ),
      );
    });
  });
}

module.exports = {
  callAgent,
  VERB_STATUS,
  VERB_UNSEAL,
  VERB_SEAL,
  VERB_CREATE,
  VERB_DESTROY,
  VERB_SERVICES,
  VERB_SPACE,
};
