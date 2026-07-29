import assert from "node:assert/strict";
import { EventEmitter, once } from "node:events";
import { PassThrough } from "node:stream";
import test from "node:test";

import { ProtocolClient, permissionDetail, type ManagedProcess } from "./client.js";

class FakeProcess extends EventEmitter implements ManagedProcess {
  readonly stdin = new PassThrough();
  readonly stdout = new PassThrough();
  readonly stderr = new PassThrough();
  killed = false;

  kill(): boolean {
    this.killed = true;
    this.emit("exit", 0, null);
    return true;
  }

  send(message: unknown): void {
    this.stdout.write(`${JSON.stringify(message)}\n`);
  }
}

function readRequest(process: FakeProcess): Promise<Record<string, unknown>> {
  return new Promise((resolve) => process.stdin.once("data", (data) => resolve(JSON.parse(data.toString()))));
}

test("streams events with IDE context and finishes the correlated turn", async () => {
  const process = new FakeProcess();
  const client = new ProtocolClient(() => process);
  const requestPromise = readRequest(process);
  const events: string[] = [];
  client.on("event", (response) => events.push(response.event?.text ?? ""));

  const finished = client.start("fix", {
    workspace: "/workspace",
    focus: "main.go",
    selection: { path: "main.go", start: { line: 1, character: 2 }, end: { line: 1, character: 5 }, text: "bad" },
    diagnostics: [{ path: "main.go", message: "broken", severity: "error" }]
  });
  const request = await requestPromise;
  assert.equal(request.type, "start");
  assert.deepEqual(request.ide_context, {
    workspace: "/workspace",
    focus: "main.go",
    selection: { path: "main.go", start: { line: 1, character: 2 }, end: { line: 1, character: 5 }, text: "bad" },
    diagnostics: [{ path: "main.go", message: "broken", severity: "error" }]
  });
  process.send({ version: 1, id: request.id, type: "accepted" });
  process.send({ version: 1, id: request.id, type: "event", event: { type: "text_delta", text: "done" } });
  process.send({ version: 1, id: request.id, type: "turn_finished" });

  assert.deepEqual(await finished, { canceled: false });
  assert.deepEqual(events, ["done"]);
  client.dispose();
});

test("isolates malformed JSON and continues reading", async () => {
  const process = new FakeProcess();
  const client = new ProtocolClient(() => process);
  client.status();
  const malformed = once(client, "protocolError");
  const status = once(client, "status");
  process.stdout.write("not-json\n");
  process.send({ version: 1, type: "status", status: { running: false, history_messages: 0 } });
  assert.match(String((await malformed)[0]), /invalid JSON/);
  assert.equal((await status)[0].status?.running, false);
  client.dispose();
});

test("sends cancellation and permission decisions with server challenge IDs", async () => {
  const process = new FakeProcess();
  const client = new ProtocolClient(() => process);
  const cancelRequest = readRequest(process);
  client.cancel("stop");
  assert.equal((await cancelRequest).type, "cancel");
  const permissionRequest = readRequest(process);
  client.respondToPermission("permission-7", "allow", "reviewed");
  assert.deepEqual(await permissionRequest, {
    version: 1,
    id: "2",
    type: "permission",
    permission_id: "permission-7",
    decision: "allow",
    reason: "reviewed"
  });
  client.dispose();
});

test("restarts after disconnect and dispose cleans up the child", async () => {
  const processes = [new FakeProcess(), new FakeProcess()];
  let index = 0;
  const client = new ProtocolClient(() => processes[index++]);
  client.status();
  processes[0].emit("exit", 1, null);
  client.status();
  assert.equal(index, 2);
  client.dispose();
  assert.equal(processes[1].killed, true);
  assert.equal(processes[1].stdin.writableEnded, true);
});

test("registers a turn before a synchronous child response", async () => {
  const process = new FakeProcess();
  process.stdin.on("data", (data) => {
    const request = JSON.parse(data.toString());
    process.send({ version: 1, id: request.id, type: "accepted" });
    process.send({ version: 1, id: request.id, type: "turn_finished" });
  });
  const client = new ProtocolClient(() => process);
  assert.deepEqual(await client.start("fast"), { canceled: false });
  client.dispose();
});

test("formats stable permission target fields", () => {
  assert.equal(permissionDetail({ tool: "shell", action: "execute", command: "go test ./..." }), "shell · execute · go");
  assert.equal(permissionDetail({ tool: "read_file", action: "read", paths: ["main.go"] }), "read_file · read · main.go");
  assert.equal(permissionDetail({ tool: "web", action: "network", network: ["example.test"] }), "web · network · example.test");
});
