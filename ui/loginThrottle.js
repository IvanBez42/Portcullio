"use strict";

const MAX_ATTEMPTS = 5;
const LOCKOUT_MS = 5 * 60 * 1000; // 5 minutes
const WINDOW_MS = 15 * 60 * 1000; // failed attempts older than this don't count toward a lockout

const attempts = new Map(); // ip -> { count, windowStart, lockedUntil }

// 0 = not locked, above 0 = ms until unlocked //
function msUntilUnlocked(ip) {
  const record = attempts.get(ip);
  if (!record || !record.lockedUntil) return 0;
  const remaining = record.lockedUntil - Date.now();
  if (remaining <= 0) {
    attempts.delete(ip);
    return 0;
  }
  return remaining;
}

// IP based login throttling //
function recordFailure(ip) {
  const now = Date.now();
  let record = attempts.get(ip);
  if (!record || now - record.windowStart > WINDOW_MS) {
    record = { count: 0, windowStart: now, lockedUntil: 0 };
  }
  record.count += 1;
  if (record.count >= MAX_ATTEMPTS) {
    record.lockedUntil = now + LOCKOUT_MS;
  }
  attempts.set(ip, record);
}

// Clears ip's record entirely on a correct password //
function recordSuccess(ip) {
  attempts.delete(ip);
}

module.exports = {
  msUntilUnlocked,
  recordFailure,
  recordSuccess,
  MAX_ATTEMPTS,
};
