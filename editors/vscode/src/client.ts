import { EventEmitter } from "node:events";
import type { Readable, Writable } from "node:stream";

import { protocolVersion, type IDEContext, type IDEDiff, type Request, type Response } from "./protocol.js";

export interface ManagedProcess extends EventEmitter {
  stdin: Writable;
  stdout: Readable;
  stderr: Readable;
  kill(): boolean;
}

export type ProcessFactory = () => ManagedProcess;

interface PendingTurn {
  resolve: (result: { canceled: boolean }) => void;
  reject: (error: Error) => void;
}

export class ProtocolClient extends EventEmitter {
  private process?: ManagedProcess;
  private buffer = "";
  private nextID = 1;
  private readonly pending = new Map<string, PendingTurn>();
  private disposed = false;

  constructor(private readonly createProcess: ProcessFactory) {
    super();
  }

  start(prompt: string, ideContext?: IDEContext): Promise<{ canceled: boolean }> {
    const id = this.send({ type: "start", prompt, ide_context: ideContext });
    return new Promise((resolve, reject) => this.pending.set(id, { resolve, reject }));
  }

  cancel(reason?: string): string {
    return this.send({ type: "cancel", reason });
  }

  status(): string {
    return this.send({ type: "status" });
  }

  respondToPermission(permissionID: string, decision: "allow" | "deny", reason?: string): string {
    return this.send({ type: "permission", permission_id: permissionID, decision, reason });
  }

  dispose(): void {
    if (this.disposed) return;
    this.disposed = true;
    const process = this.process;
    this.process = undefined;
    if (process) process.kill();
    this.rejectPending(new Error("cyber-code client disposed"));
    this.removeAllListeners();
  }

  private send(body: Omit<Request, "version" | "id">): string {
    if (this.disposed) throw new Error("cyber-code client is disposed");
    const id = String(this.nextID++);
    const request = { version: protocolVersion, id, ...body } as Request;
    this.ensureProcess().stdin.write(`${JSON.stringify(request)}\n`);
    return id;
  }

  private ensureProcess(): ManagedProcess {
    if (this.process) return this.process;
    const process = this.createProcess();
    this.process = process;
    this.buffer = "";
    process.stdout.on("data", (chunk: Buffer | string) => this.handleData(chunk.toString()));
    process.stderr.on("data", (chunk: Buffer | string) => this.emit("stderr", chunk.toString()));
    process.once("error", (error: Error) => this.handleDisconnect(process, error));
    process.once("exit", (code: number | null, signal: NodeJS.Signals | null) => {
      this.handleDisconnect(process, new Error(`cyber-code serve exited (code=${String(code)}, signal=${String(signal)})`));
    });
    return process;
  }

  private handleData(chunk: string): void {
    this.buffer += chunk;
    for (;;) {
      const newline = this.buffer.indexOf("\n");
      if (newline < 0) return;
      const line = this.buffer.slice(0, newline).trim();
      this.buffer = this.buffer.slice(newline + 1);
      if (!line) continue;
      let response: Response;
      try {
        response = JSON.parse(line) as Response;
      } catch {
        this.emit("protocolError", new Error("invalid JSON from cyber-code serve"));
        continue;
      }
      if (response.version !== protocolVersion || typeof response.type !== "string") {
        this.emit("protocolError", new Error("invalid cyber-code protocol response"));
        continue;
      }
      this.route(response);
    }
  }

  private route(response: Response): void {
    if (response.type === "turn_finished" && response.id) {
      const turn = this.pending.get(response.id);
      if (turn) {
        this.pending.delete(response.id);
        turn.resolve({ canceled: response.canceled === true });
      }
    } else if (response.type === "error" && response.id) {
      const turn = this.pending.get(response.id);
      if (turn) {
        this.pending.delete(response.id);
        turn.reject(new Error(response.error ?? "cyber-code protocol error"));
      }
    }
    this.emit(response.type, response);
  }

  private handleDisconnect(process: ManagedProcess, error: Error): void {
    if (this.process !== process) return;
    this.process = undefined;
    this.buffer = "";
    this.rejectPending(error);
    this.emit("disconnect", error);
  }

  private rejectPending(error: Error): void {
    for (const turn of this.pending.values()) turn.reject(error);
    this.pending.clear();
  }
}

export function applyDiff(currentText: string, diff: IDEDiff): string {
  if (currentText !== diff.old_text) {
    throw new Error(`diff conflict for ${diff.path}: document content changed`);
  }
  return diff.new_text;
}
