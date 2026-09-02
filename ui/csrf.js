"use strict";

const crypto = require("crypto");
const { doubleCsrf } = require("csrf-csrf");
const state = require("./state");
const auth = require("./auth");

// Persists the secret so outstanding CSRF cookies survive a container restart //
function getOrCreateSecret() {
  const s = state.load();
  if (!s.csrfSecret) {
    s.csrfSecret = crypto.randomBytes(32).toString("hex");
    state.save(s);
  }
  return s.csrfSecret;
}

const CSRF_SECRET = getOrCreateSecret();

const { doubleCsrfProtection, invalidCsrfTokenError } = doubleCsrf({
  getSecret: () => CSRF_SECRET,
  getSessionIdentifier: (req) =>
    (req.cookies && req.cookies[auth.SESSION_COOKIE]) || "anonymous",
  cookieName: "portcullio_csrf",
  cookieOptions: {
    sameSite: "strict",
    path: "/",
    secure: false,
    httpOnly: true,
  },
  getCsrfTokenFromRequest: (req) => req.body && req.body._csrf,
});

module.exports = { doubleCsrfProtection, invalidCsrfTokenError };
