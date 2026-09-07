"use strict";

// Single source of truth for vault_id shape: "plex" or "movies/plex" //

const SEGMENT_SOURCE = "[a-zA-Z0-9_][a-zA-Z0-9_-]{0,63}";
const SEGMENT_PATTERN = new RegExp(`^${SEGMENT_SOURCE}$`);
const VAULT_ID_PATTERN = new RegExp(`^(${SEGMENT_SOURCE}/)?${SEGMENT_SOURCE}$`);

// "-" can never be a real segment (must start alnum/underscore), so it's a safe root sentinel //
const ROOT_SENTINEL = "-";

// Splits a compound vault_id into { locker, name } -- locker is "" for a root vault //
function splitVaultId(id) {
  const slash = id.indexOf("/");
  if (slash === -1) return { locker: "", name: id };
  return { locker: id.slice(0, slash), name: id.slice(slash + 1) };
}

// Joins a locker ("" for root) and a bare name into a compound vault_id //
function joinVaultId(locker, name) {
  return locker ? `${locker}/${name}` : name;
}

// Builds the two URL path segments for a vault_id, e.g. "plex" -> "-/plex" //
function urlPathFor(id) {
  const { locker, name } = splitVaultId(id);
  return `${locker || ROOT_SENTINEL}/${name}`;
}

// Validates a pair of URL path segments and returns the compound vault_id, or null //
function parseUrlSegments(lockerParam, nameParam) {
  if (lockerParam !== ROOT_SENTINEL && !SEGMENT_PATTERN.test(lockerParam || "")) return null;
  if (!SEGMENT_PATTERN.test(nameParam || "")) return null;
  const locker = lockerParam === ROOT_SENTINEL ? "" : lockerParam;
  return joinVaultId(locker, nameParam);
}

module.exports = {
  SEGMENT_SOURCE,
  SEGMENT_PATTERN,
  VAULT_ID_PATTERN,
  ROOT_SENTINEL,
  splitVaultId,
  joinVaultId,
  urlPathFor,
  parseUrlSegments,
};
