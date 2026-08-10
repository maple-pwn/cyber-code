export type VSCodeRuntime = "coding" | "cyber-agent";

export type RuntimeConfiguration = {
  runtime: VSCodeRuntime;
  endpoint?: string;
  executable?: string;
};

export function readRuntimeConfiguration(values: {
  runtime?: string;
  cyberAgentEndpoint?: string;
  cyberAgentExecutable?: string;
}): RuntimeConfiguration {
  const runtime = values.runtime ?? "coding";
  if (runtime !== "coding" && runtime !== "cyber-agent") throw new Error(`invalid cyber-code runtime: ${runtime}`);
  const endpoint = values.cyberAgentEndpoint?.trim();
  const executable = values.cyberAgentExecutable?.trim();
  return {
    runtime,
    ...(endpoint ? { endpoint } : {}),
    ...(executable ? { executable } : {}),
  };
}

export function runtimeIdentity(runtime: VSCodeRuntime): string {
  return runtime === "cyber-agent" ? "Security Runtime - cyber-agent" : "Coding Runtime - cyber-code";
}
