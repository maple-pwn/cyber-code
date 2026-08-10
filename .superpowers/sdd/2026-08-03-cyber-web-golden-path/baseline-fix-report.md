# Baseline Fix Report

## Root cause

`NewRunner(Options{})` defaults to `SandboxBestEffort`. With `bwrap` installed, the command runs with `--unshare-all`, so the shell writes a namespace-local child PID (`3`) to `$!`. The test then calls `syscall.Kill(3, 0)` in the host namespace, checking an unrelated process and producing a stable false failure. The test now uses `SandboxOff` so the child PID is host-visible.

## Diff

```diff
- _, err := NewRunner(Options{}).Run(ctx, ExecRequest{
+ _, err := NewRunner(Options{SandboxMode: SandboxOff}).Run(ctx, ExecRequest{
```

Only `internal/platform/exec_unix_test.go` was changed for the code fix.

## Tests

Focused test, run five times:

```text
focused run 1
ok  cyber-code/internal/platform  0.014s
focused run 2
ok  cyber-code/internal/platform  0.013s
focused run 3
ok  cyber-code/internal/platform  0.013s
focused run 4
ok  cyber-code/internal/platform  0.013s
focused run 5
ok  cyber-code/internal/platform  0.013s
```

Package test:

```text
ok  cyber-code/internal/platform  0.098s
```

Self-review: `git diff --check` passed; the code diff is one targeted option change with no unnecessary comment.

## Commit

`ae0068d47abe8009b5c7aee0aff8c28d49148578`
