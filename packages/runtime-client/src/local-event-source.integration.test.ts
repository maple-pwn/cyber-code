// @vitest-environment node

import { randomBytes } from 'node:crypto';
import { spawn, execFileSync, type ChildProcessWithoutNullStreams } from 'node:child_process';
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { createInterface, type Interface } from 'node:readline';

import { afterAll, beforeAll, expect, test } from 'vitest';

import {
  LocalEventSource,
  RuntimeClient,
  RuntimeSourceFactory,
  type LocalRequest,
  type LocalTransport,
} from './index';
import { defineLocalEventSourceConformance } from './local-event-source-conformance.test-support';

const temporaryRoot = mkdtempSync(join(tmpdir(), 'cyber-runtime-client-'));
const binary = join(temporaryRoot, process.platform === 'win32' ? 'cyber-code.exe' : 'cyber-code');
const goCache = process.env.GOCACHE ?? join(tmpdir(), 'cyber-phase4-go-cache');

class BinaryRuntimeTransport implements LocalTransport {
  readonly secret = randomBytes(32).toString('hex');
  readonly stateDir = mkdtempSync(join(temporaryRoot, 'state-'));
  stdout = '';
  stderr = '';
  private child?: ChildProcessWithoutNullStreams;
  private lines?: Interface;
  private exited?: Promise<void>;
  private readonly pending: Array<{
    resolve: (value: unknown) => void;
    reject: (error: Error) => void;
  }> = [];

  async start(): Promise<unknown> {
    this.child = spawn(binary, ['runtime', 'serve'], {
      cwd: process.cwd(),
      env: {
        ...process.env,
        CYBER_CODE_RUNTIME_BEARER: this.secret,
        CYBER_CODE_STATE_DIR: this.stateDir,
      },
      stdio: ['pipe', 'pipe', 'pipe'],
    });
    this.lines = createInterface({ input: this.child.stdout });
    this.lines.on('line', (line) => {
      this.stdout += `${line}\n`;
      const pending = this.pending.shift();
      if (pending === undefined) return;
      try {
        pending.resolve(JSON.parse(line));
      } catch {
        pending.reject(new Error('runtime_response_invalid'));
      }
    });
    this.child.stderr.on('data', (chunk: Buffer) => {
      this.stderr += chunk.toString('utf8');
    });
    const rejectPending = () => {
      for (const pending of this.pending.splice(0)) pending.reject(new Error('runtime_crashed'));
    };
    this.child.on('exit', rejectPending);
    this.child.on('error', rejectPending);
    this.exited = new Promise((resolve) => {
      this.child?.once('exit', () => resolve());
      this.child?.once('error', () => resolve());
    });
    return this.exchange({
      id: 'binary-startup',
      type: 'handshake',
      handshake: { supportedProtocolVersions: [1], afterCursor: 0 },
    });
  }

  request(request: LocalRequest): Promise<unknown> {
    return this.exchange(request);
  }

  async restart(): Promise<unknown> {
    await this.stop();
    return this.start();
  }

  async stop(): Promise<unknown> {
    const child = this.child;
    if (child === undefined) return { stopped: false };
    if (child.exitCode === null && !child.killed) child.kill();
    await this.exited;
    this.lines?.close();
    child.stdin.destroy();
    child.stdout.destroy();
    child.stderr.destroy();
    this.child = undefined;
    this.lines = undefined;
    this.exited = undefined;
    return { stopped: true };
  }

  assertCredentialDidNotLeak(): void {
    expect(this.stdout).not.toContain(this.secret);
    expect(this.stderr).not.toContain(this.secret);
  }

  private exchange(request: LocalRequest): Promise<unknown> {
    if (this.child === undefined || this.child.stdin.destroyed) {
      return Promise.reject(new Error('runtime_not_running'));
    }
    return new Promise((resolve, reject) => {
      this.pending.push({ resolve, reject });
      this.child?.stdin.write(`${JSON.stringify({ ...request, bearer: this.secret })}\n`, (error) => {
        if (error) reject(new Error('runtime_crashed'));
      });
    });
  }
}

beforeAll(() => {
  execFileSync('go', ['build', '-buildvcs=false', '-o', binary, './cmd/cli'], {
    cwd: process.cwd(),
    env: { ...process.env, GOCACHE: goCache },
    stdio: 'pipe',
  });
}, 120_000);

afterAll(() => {
  rmSync(temporaryRoot, { recursive: true, force: true });
});

defineLocalEventSourceConformance('built cyber-code local runtime', async () => {
  const transport = new BinaryRuntimeTransport();
  return {
    source: new LocalEventSource(transport, { pollIntervalMs: 60_000 }),
    verifyClosed: () => transport.assertCredentialDidNotLeak(),
  };
}, 120_000);

test('source factory executes a task through the built local runtime process', async () => {
  const transport = new BinaryRuntimeTransport();
  const factory = new RuntimeSourceFactory([{
    id: 'local', mode: 'local', label: 'Local', capabilities: ['real-runtime'], available: true,
    create: () => new LocalEventSource(transport, { pollIntervalMs: 60_000 }),
  }]);
  const source = factory.create('local');
  const client = new RuntimeClient(source);
  try {
    await client.connect();
    expect(client.getView().connection).toEqual({ status: 'healthy', lastTrustedCursor: 0 });
    expect(client.getView().source).toMatchObject({ mode: 'local', principal: 'local-user' });
    await client.dispatch({
      type: 'task.create', objective: 'Inspect the authorized workspace',
      runtimeId: client.getView().source?.runtimeId ?? '',
    });
    await expect(source.getSnapshot()).resolves.toMatchObject({
      cursor: 2,
      state: { task: { title: 'Inspect the authorized workspace' }, committedCursor: 2 },
    });
    await client.dispatch({
      type: 'task.create', objective: 'Inspect the second authorized task',
      runtimeId: client.getView().source?.runtimeId ?? '',
    });
    expect(client.getView()).toMatchObject({
      connection: { status: 'healthy', lastTrustedCursor: 2 },
      product: { task: { id: 'task-2', title: 'Inspect the second authorized task' }, committedCursor: 2 },
    });
  } finally {
    await client.disconnect().catch(() => undefined);
    transport.assertCredentialDidNotLeak();
  }
}, 120_000);
