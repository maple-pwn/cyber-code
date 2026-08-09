export const protocolVersion = 1;
export const protocolSemanticVersion = "1.1";
export const protocolCapabilities = ["base", "permissions", "ide-context", "diff"] as const;

export interface Position {
  line: number;
  character: number;
}

export interface Selection {
  path: string;
  start: Position;
  end: Position;
  text?: string;
}

export interface Diagnostic {
  path: string;
  message: string;
  severity?: string;
  start?: Position;
  end?: Position;
}

export interface IDEContext {
  workspace?: string;
  focus?: string;
  selection?: Selection;
  diagnostics?: Diagnostic[];
}

export interface IDEDiff {
  path: string;
  old_text: string;
  new_text: string;
}

export interface Request {
  version: 1;
  id: string;
  type: "handshake" | "start" | "input" | "cancel" | "status" | "permission";
  prompt?: string;
  reason?: string;
  permission_id?: string;
  decision?: "allow" | "deny";
  ide_context?: IDEContext;
  protocol?: string;
  capabilities?: string[];
}

export interface RuntimeEvent {
  type: string;
  text?: string;
  error?: { message?: string };
  [key: string]: unknown;
}

export interface PermissionPrompt {
  id: string;
  request: {
    tool?: string;
    action?: string;
    workspace?: string;
    command?: string;
    paths?: string[];
    network?: string[];
    [key: string]: unknown;
  };
}

export interface Response {
  version: number;
  id?: string;
  type: "handshake" | "accepted" | "event" | "turn_finished" | "status" | "permission" | "diff" | "error";
  event?: RuntimeEvent;
  status?: { session_id?: string; running: boolean; history_messages: number };
  permission?: PermissionPrompt;
  diff?: IDEDiff;
  error?: string;
  canceled?: boolean;
  protocol?: string;
  capabilities?: string[];
}
