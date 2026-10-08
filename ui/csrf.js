"use strict";

const crypto = require("node:crypto");
const { doubleCsrf } = require("csrf-csrf");
const state = require("./state");
const auth = require("./auth");

const CSRF_COOKIE = `${auth.COOKIE_PREFIX}portcullio_csrf`;

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
    req.cookies?.[auth.SESSION_COOKIE] || "anonymous",
  cookieName: CSRF_COOKIE,
  cookieOptions: auth.COOKIE_OPTIONS,
  getCsrfTokenFromRequest: (req) => req.body?._csrf,
});

module.exports = { CSRF_COOKIE, doubleCsrfProtection, invalidCsrfTokenError };
