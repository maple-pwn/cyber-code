import test from "node:test";
import assert from "node:assert/strict";

import { readRuntimeConfiguration, runtimeIdentity } from "./runtime.js";

test("reads cyber-agent runtime configuration without falling back", () => {
  assert.deepEqual(readRuntimeConfiguration({
    runtime: "cyber-agent",
    cyberAgentEndpoint: "https://agent.example.test",
    cyberAgentExecutable: "/opt/cyber-agent",
  }), {
    runtime: "cyber-agent",
    endpoint: "https://agent.example.test",
    executable: "/opt/cyber-agent",
  });
  assert.equal(runtimeIdentity("cyber-agent"), "Security Runtime - cyber-agent");
  assert.equal(runtimeIdentity("coding"), "Coding Runtime - cyber-code");
});

test("rejects unknown runtime values", () => {
  assert.throws(() => readRuntimeConfiguration({ runtime: "demo" }), /invalid cyber-code runtime/);
});
