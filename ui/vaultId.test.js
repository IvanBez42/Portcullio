"use strict";

const test = require("node:test");
const assert = require("node:assert/strict");

const {
  VAULT_ID_PATTERN,
  splitVaultId,
  joinVaultId,
  urlPathFor,
  parseUrlSegments,
} = require("./vaultId");

test("VAULT_ID_PATTERN accepts a bare name and a locker-scoped name", () => {
  assert.equal(VAULT_ID_PATTERN.test("plex"), true);
  assert.equal(VAULT_ID_PATTERN.test("movies/plex"), true);
});

test("VAULT_ID_PATTERN rejects malformed or too-deeply-nested ids", () => {
  for (const id of ["", "movies/", "/plex", "movies//plex", "a/b/c", "../etc/passwd", "movies/../plex"]) {
    assert.equal(VAULT_ID_PATTERN.test(id), false, `expected ${JSON.stringify(id)} to be rejected`);
  }
});

test("splitVaultId / joinVaultId round-trip a root vault", () => {
  assert.deepEqual(splitVaultId("plex"), { locker: "", name: "plex" });
  assert.equal(joinVaultId("", "plex"), "plex");
});

test("splitVaultId / joinVaultId round-trip a locker-scoped vault", () => {
  assert.deepEqual(splitVaultId("movies/plex"), { locker: "movies", name: "plex" });
  assert.equal(joinVaultId("movies", "plex"), "movies/plex");
});

test("urlPathFor uses the '-' sentinel for a root vault, and the real locker otherwise", () => {
  assert.equal(urlPathFor("plex"), "-/plex");
  assert.equal(urlPathFor("movies/plex"), "movies/plex");
});

test("parseUrlSegments inverts urlPathFor for both root and locker-scoped ids", () => {
  for (const id of ["plex", "movies/plex"]) {
    assert.equal(parseUrlSegments(...urlPathFor(id).split("/")), id);
  }
});

test("parseUrlSegments rejects a malformed locker or name segment", () => {
  assert.equal(parseUrlSegments("../etc", "plex"), null);
  assert.equal(parseUrlSegments("movies", "../passwd"), null);
  assert.equal(parseUrlSegments("movies", ""), null);
});
