import { spawn } from "node:child_process";
import path from "node:path";

import * as vscode from "vscode";

import { ProtocolClient, permissionDetail, type ManagedProcess } from "./client.js";
import type { IDEContext, IDEDiff, PermissionPrompt, Response } from "./protocol.js";
import { readRuntimeConfiguration, runtimeIdentity, type RuntimeConfiguration } from "./runtime.js";

let client: ProtocolClient | undefined;
let output: vscode.OutputChannel | undefined;
let runtimeTree: RuntimeTreeProvider | undefined;
let pendingPermissionID: string | undefined;

class RuntimeTreeItem extends vscode.TreeItem {
  constructor(label: string, state?: vscode.TreeItemCollapsibleState) {
    super(label, state ?? vscode.TreeItemCollapsibleState.None);
    this.contextValue = "cyber-code.runtime";
  }
}

class RuntimeTreeProvider implements vscode.TreeDataProvider<RuntimeTreeItem> {
  private readonly changed = new vscode.EventEmitter<void>();
  readonly onDidChangeTreeData = this.changed.event;
  private status = "disconnected";
  private session = "no active session";
  private approval = "no pending approval";
  constructor(private readonly configuration: RuntimeConfiguration) {}
  getTreeItem(item: RuntimeTreeItem): vscode.TreeItem { return item; }
  getChildren(): RuntimeTreeItem[] {
    return [
      new RuntimeTreeItem(runtimeIdentity(this.configuration.runtime)),
      new RuntimeTreeItem(`Authority: ${this.configuration.runtime === "cyber-agent" ? "cyber-agent" : "cyber-code"}`),
      new RuntimeTreeItem(`Session: ${this.session}`),
      new RuntimeTreeItem(`State: ${this.status}`),
      new RuntimeTreeItem(`Approval: ${this.approval}`),
    ];
  }
  update(values: Partial<{ status: string; session: string; approval: string }>): void {
    Object.assign(this, values); this.changed.fire();
  }
}

export function activate(context: vscode.ExtensionContext): void {
  output = vscode.window.createOutputChannel("cyber-code");
  context.subscriptions.push(output);

  const configuration = readRuntimeConfiguration({
    runtime: vscode.workspace.getConfiguration("cyber-code").get<string>("runtime", "coding"),
    cyberAgentEndpoint: vscode.workspace.getConfiguration("cyber-code").get<string>("cyberAgentEndpoint", ""),
    cyberAgentExecutable: vscode.workspace.getConfiguration("cyber-code").get<string>("cyberAgentExecutable", "cyber-agent"),
  });
  runtimeTree = new RuntimeTreeProvider(configuration);
  context.subscriptions.push(vscode.window.registerTreeDataProvider("cyber-code.sessions", runtimeTree));
  output.appendLine(`[runtime] ${runtimeIdentity(configuration.runtime)}`);
  if (configuration.endpoint) output.appendLine(`[runtime] endpoint: ${configuration.endpoint}`);
  client = createClient();
  context.subscriptions.push({ dispose: () => client?.dispose() });
  context.subscriptions.push(vscode.commands.registerCommand("cyber-code.ask", ask));
  context.subscriptions.push(vscode.commands.registerCommand("cyber-code.cancel", () => client?.cancel("canceled from VS Code")));
  context.subscriptions.push(vscode.commands.registerCommand("cyber-code.approve", () => {
    if (pendingPermissionID) client?.respondToPermission(pendingPermissionID, "allow", "approved from VS Code command");
  }));
}

export function deactivate(): void {
  client?.dispose();
  client = undefined;
}

function createClient(): ProtocolClient {
  const configuration = readRuntimeConfiguration({
    runtime: vscode.workspace.getConfiguration("cyber-code").get<string>("runtime", "coding"),
    cyberAgentEndpoint: vscode.workspace.getConfiguration("cyber-code").get<string>("cyberAgentEndpoint", ""),
    cyberAgentExecutable: vscode.workspace.getConfiguration("cyber-code").get<string>("cyberAgentExecutable", "cyber-agent"),
  });
  const result = new ProtocolClient(() => {
    const executable = configuration.runtime === "cyber-agent"
      ? (configuration.executable ?? "cyber-agent")
      : vscode.workspace.getConfiguration("cyber-code").get<string>("executable", "cyber-code");
    const workspace = vscode.workspace.workspaceFolders?.[0]?.uri.fsPath;
    return spawn(executable, ["serve"], {
      cwd: workspace,
      env: { ...process.env, ...(configuration.endpoint ? { CYBER_AGENT_ENDPOINT: configuration.endpoint } : {}) },
      stdio: ["pipe", "pipe", "pipe"],
      windowsHide: true
    }) as ManagedProcess;
  });
  result.on("event", (response: Response) => renderEvent(response));
  result.on("permission", (response: Response) => void handlePermission(response.permission));
  result.on("diff", (response: Response) => void handleDiff(response.diff));
  result.on("stderr", (text: string) => output?.append(text));
  result.on("protocolError", (error: Error) => output?.appendLine(`[protocol] ${error.message}`));
  result.on("disconnect", (error: Error) => { runtimeTree?.update({ status: "disconnected" }); output?.appendLine(`[disconnected] ${error.message}`); });
  result.on("handshake", () => runtimeTree?.update({ status: "connected" }));
  result.on("turn_finished", (response: Response) => runtimeTree?.update({ session: response.id ?? "active" }));
  return result;
}

async function ask(): Promise<void> {
  const prompt = await vscode.window.showInputBox({ title: "Ask cyber-code", prompt: "Describe the coding task" });
  if (!prompt?.trim() || !client) return;
  output?.show(true);
  output?.appendLine(`\n> ${prompt}\n`);
  try {
    const result = await client.start(prompt, collectIDEContext());
    if (result.canceled) output?.appendLine("\n[canceled]");
    else output?.appendLine("");
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error);
    output?.appendLine(`\n[error] ${message}`);
    void vscode.window.showErrorMessage(`cyber-code: ${message}`);
  }
}

function collectIDEContext(): IDEContext {
  const editor = vscode.window.activeTextEditor;
  const workspace = vscode.workspace.workspaceFolders?.[0];
  const result: IDEContext = { workspace: workspace?.uri.fsPath };
  if (!editor) return result;

  const document = editor.document;
  result.focus = relativePath(document.uri);
  if (!editor.selection.isEmpty) {
    result.selection = {
      path: result.focus,
      start: position(editor.selection.start),
      end: position(editor.selection.end),
      text: document.getText(editor.selection)
    };
  }
  result.diagnostics = vscode.languages.getDiagnostics(document.uri).slice(0, 32).map((diagnostic) => ({
    path: result.focus ?? document.uri.fsPath,
    message: diagnostic.message,
    severity: diagnosticSeverity(diagnostic.severity),
    start: position(diagnostic.range.start),
    end: position(diagnostic.range.end)
  }));
  return result;
}

function renderEvent(response: Response): void {
  const event = response.event;
  if (!event) return;
  if (event.type === "text_delta" || event.type === "assistant_message") {
    output?.append(event.text ?? "");
  } else if (event.type === "tool_call") {
    output?.appendLine("\n[tool running]");
  } else if (event.type === "tool_result") {
    output?.appendLine("[tool finished]");
  } else if (event.type === "warning") {
    output?.appendLine(`\n[warning] ${event.text ?? ""}`);
  } else if (event.type === "error") {
    output?.appendLine(`\n[error] ${event.error?.message ?? event.text ?? "runtime error"}`);
  }
}

async function handlePermission(permission?: PermissionPrompt): Promise<void> {
	if (!permission || !client) return;
	pendingPermissionID = permission.id;
	runtimeTree?.update({ approval: permission.id });
	const detail = permissionDetail(permission.request);
	const message = `cyber-code requests permission${detail.text ? `: ${detail.text}` : ""}`;
	const choice = detail.reviewable
		? await vscode.window.showWarningMessage(message, { modal: true }, "Allow", "Deny")
		: await vscode.window.showWarningMessage(message, { modal: true }, "Deny");
  const decision = choice === "Allow" ? "allow" : "deny";
  client.respondToPermission(permission.id, decision, choice === "Allow" ? "approved in VS Code" : "denied in VS Code");
  pendingPermissionID = undefined;
  runtimeTree?.update({ approval: "no pending approval" });
}

async function handleDiff(diff?: IDEDiff): Promise<void> {
  if (!diff) return;
  const oldDocument = await vscode.workspace.openTextDocument({ content: diff.old_text, language: languageForPath(diff.path) });
  const newDocument = await vscode.workspace.openTextDocument({ content: diff.new_text, language: languageForPath(diff.path) });
  await vscode.commands.executeCommand("vscode.diff", oldDocument.uri, newDocument.uri, `cyber-code changed: ${diff.path}`);
}

function relativePath(uri: vscode.Uri): string {
  return vscode.workspace.getWorkspaceFolder(uri) ? vscode.workspace.asRelativePath(uri, false) : uri.fsPath;
}

function position(value: vscode.Position): { line: number; character: number } {
  return { line: value.line, character: value.character };
}

function diagnosticSeverity(value: vscode.DiagnosticSeverity): string {
  return ["error", "warning", "information", "hint"][value] ?? "unknown";
}

function languageForPath(file: string): string | undefined {
  const extension = path.extname(file).slice(1);
  return extension || undefined;
}
