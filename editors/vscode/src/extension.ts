import { spawn } from "node:child_process";
import path from "node:path";

import * as vscode from "vscode";

import { ProtocolClient, applyDiff, type ManagedProcess } from "./client.js";
import type { IDEContext, IDEDiff, PermissionPrompt, Response } from "./protocol.js";

let client: ProtocolClient | undefined;
let output: vscode.OutputChannel | undefined;

export function activate(context: vscode.ExtensionContext): void {
  output = vscode.window.createOutputChannel("cyber-code");
  context.subscriptions.push(output);

  client = createClient();
  context.subscriptions.push({ dispose: () => client?.dispose() });
  context.subscriptions.push(vscode.commands.registerCommand("cyber-code.ask", ask));
  context.subscriptions.push(vscode.commands.registerCommand("cyber-code.cancel", () => client?.cancel("canceled from VS Code")));
}

export function deactivate(): void {
  client?.dispose();
  client = undefined;
}

function createClient(): ProtocolClient {
  const result = new ProtocolClient(() => {
    const executable = vscode.workspace.getConfiguration("cyber-code").get<string>("executable", "cyber-code");
    const workspace = vscode.workspace.workspaceFolders?.[0]?.uri.fsPath;
    return spawn(executable, ["serve"], {
      cwd: workspace,
      env: process.env,
      stdio: ["pipe", "pipe", "pipe"],
      windowsHide: true
    }) as ManagedProcess;
  });
  result.on("event", (response: Response) => renderEvent(response));
  result.on("permission", (response: Response) => void handlePermission(response.permission));
  result.on("diff", (response: Response) => void handleDiff(response.diff));
  result.on("stderr", (text: string) => output?.append(text));
  result.on("protocolError", (error: Error) => output?.appendLine(`[protocol] ${error.message}`));
  result.on("disconnect", (error: Error) => output?.appendLine(`[disconnected] ${error.message}`));
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
  const request = permission.request;
  const detail = [request.tool, request.action, request.target].filter(Boolean).join(" · ");
  const choice = await vscode.window.showWarningMessage(
    `cyber-code requests permission${detail ? `: ${detail}` : ""}`,
    { modal: true },
    "Allow",
    "Deny"
  );
  const decision = choice === "Allow" ? "allow" : "deny";
  client.respondToPermission(permission.id, decision, choice === "Allow" ? "approved in VS Code" : "denied in VS Code");
}

async function handleDiff(diff?: IDEDiff): Promise<void> {
  if (!diff) return;
  const target = resolveWorkspacePath(diff.path);
  if (!target) {
    void vscode.window.showErrorMessage(`cyber-code rejected diff path: ${diff.path}`);
    return;
  }
  const oldDocument = await vscode.workspace.openTextDocument({ content: diff.old_text, language: languageForPath(diff.path) });
  const newDocument = await vscode.workspace.openTextDocument({ content: diff.new_text, language: languageForPath(diff.path) });
  await vscode.commands.executeCommand("vscode.diff", oldDocument.uri, newDocument.uri, `cyber-code: ${diff.path}`);
  const choice = await vscode.window.showInformationMessage(`Apply cyber-code diff to ${diff.path}?`, { modal: true }, "Apply", "Reject");
  if (choice !== "Apply") return;

  const document = await vscode.workspace.openTextDocument(target);
  let replacement: string;
  try {
    replacement = applyDiff(document.getText(), diff);
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error);
    void vscode.window.showErrorMessage(`cyber-code: ${message}`);
    return;
  }
  const lastLine = document.lineAt(document.lineCount - 1);
  const edit = new vscode.WorkspaceEdit();
  edit.replace(target, new vscode.Range(new vscode.Position(0, 0), lastLine.rangeIncludingLineBreak.end), replacement);
  if (!(await vscode.workspace.applyEdit(edit))) {
    void vscode.window.showErrorMessage(`cyber-code could not apply diff to ${diff.path}`);
  }
}

function resolveWorkspacePath(relative: string): vscode.Uri | undefined {
  const workspace = vscode.workspace.workspaceFolders?.[0];
  if (!workspace || path.isAbsolute(relative)) return undefined;
  const normalized = path.posix.normalize(relative.replaceAll("\\", "/"));
  if (normalized === ".." || normalized.startsWith("../")) return undefined;
  return vscode.Uri.joinPath(workspace.uri, ...normalized.split("/"));
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
