"use strict";

const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("fs");
const os = require("os");
const path = require("path");

// csrf.js reads/writes its secret at require time. //
process.env.PORTCULLIO_STATE_DIR = fs.mkdtempSync(
  path.join(os.tmpdir(), "portcullio-ui-test-"),
);

const csrf = require("./csrf");
const auth = require("./auth");

// Stands in for an Express req without pulling in Express //
function makeReq({ method = "GET", cookies = {}, body = {} } = {}) {
  return { method, cookies, body };
}

// Stands in for an Express res, recording whatever cookie() sets //
function makeRes() {
  const cookies = {};
  return {
    cookies,
    cookie(name, value) {
      cookies[name] = value;
    },
  };
}

// Runs the middleware and returns the error next() was called with //
function runProtection(req, res) {
  let error;
  csrf.doubleCsrfProtection(req, res, (err) => {
    error = err;
  });
  return error;
}

// Just loading a page shouldn't need a token yet, but it hands one out for later //
test("GET requests are never blocked and get a usable token", () => {
  const req = makeReq();
  const res = makeRes();
  assert.equal(runProtection(req, res), undefined);
  const token = req.csrfToken();
  assert.equal(typeof token, "string");
  assert.ok(token.length > 0);
  assert.equal(res.cookies.portcullio_csrf, token);
});

// No token at all -- rejected //
test("POST with no CSRF cookie or body field is rejected", () => {
  const req = makeReq({ method: "POST" });
  const error = runProtection(req, makeRes());
  assert.equal(error && error.code, "EBADCSRFTOKEN");
});

// Real cookie, fake form value -- rejected //
test("POST with a mismatched token is rejected", () => {
  const getReq = makeReq();
  runProtection(getReq, makeRes());
  const token = getReq.csrfToken();

  const postReq = makeReq({
    method: "POST",
    cookies: { portcullio_csrf: token },
    body: { _csrf: "not-the-real-token" },
  });
  const error = runProtection(postReq, makeRes());
  assert.equal(error && error.code, "EBADCSRFTOKEN");
});

// Real cookie, matching form value -- accepted //
test("a token generated on GET is accepted on the matching POST", () => {
  const getReq = makeReq();
  runProtection(getReq, makeRes());
  const token = getReq.csrfToken();

  const postReq = makeReq({
    method: "POST",
    cookies: { portcullio_csrf: token },
    body: { _csrf: token },
  });
  assert.equal(runProtection(postReq, makeRes()), undefined);
});

// Same token, same logged-in session -- still accepted //
test("a token stays valid when the session cookie is unchanged", () => {
  const getReq = makeReq({
    cookies: { [auth.SESSION_COOKIE]: "session-a" },
  });
  runProtection(getReq, makeRes());
  const token = getReq.csrfToken();

  const postReq = makeReq({
    method: "POST",
    cookies: {
      [auth.SESSION_COOKIE]: "session-a",
      portcullio_csrf: token,
    },
    body: { _csrf: token },
  });
  assert.equal(runProtection(postReq, makeRes()), undefined);
});

// Real token, but stolen for a different session -- rejected //
test("a token minted for one session cannot be replayed against another", () => {
  const getReq = makeReq({
    cookies: { [auth.SESSION_COOKIE]: "session-a" },
  });
  runProtection(getReq, makeRes());
  const token = getReq.csrfToken();

  const postReq = makeReq({
    method: "POST",
    cookies: {
      [auth.SESSION_COOKIE]: "session-b",
      portcullio_csrf: token,
    },
    body: { _csrf: token },
  });
  const error = runProtection(postReq, makeRes());
  assert.equal(error && error.code, "EBADCSRFTOKEN");
});
