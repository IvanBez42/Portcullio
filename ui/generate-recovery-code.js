"use strict";

const auth = require("./auth");

const code = auth.createRecoveryCode();
if (code === null) {
  console.log(
    "A recovery code was already generated in the last 10 minutes -- check this",
  );
  console.log(
    "container's earlier log output, or wait before generating a new one.",
  );
} else {
  auth.logRecoveryCode(code);
}
