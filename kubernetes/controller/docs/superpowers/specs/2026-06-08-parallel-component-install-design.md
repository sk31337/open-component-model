# Parallel Component Installation in local.sh

**Date:** 2026-06-08
**Status:** Approved

## Problem

`local.sh --all-components` installs each component script sequentially. With 8 scripts — each of which `kubectl wait`s for deployments — total setup time is the sum of all install times. Scripts have no ordering constraints between them, so they can run concurrently.

## Scope

Only the `--all-components` path in `test/e2e/setup/local.sh` changes. `cluster.sh` still runs first, sequentially. The per-component scripts themselves are unchanged.

## Design

Replace the sequential `for s in "${scripts[@]}"` loop with a parallel fan-out using bash background jobs.

### Per-script output

Each script gets a dedicated temp file created with `mktemp`. Both `stdout` and `stderr` are redirected there (`>"$tmpfile" 2>&1`). On success the file is deleted silently. On failure its contents are printed to the terminal.

### Fail-fast

After launching all background jobs, the orchestrator loops with `wait -n` (wait for any one job to finish). On the first non-zero exit:

1. Record which PID failed and find its associated temp file.
2. `kill` all remaining background PIDs (best-effort; ignore already-exited).
3. Print the failed script's name and its captured output.
4. `exit 1`.

### Data structures

- `pids`: array of background PIDs in launch order
- `tmpfiles`: parallel array mapping index → temp file path
- `names`: parallel array mapping index → script basename (for error messages)

All three arrays are indexed identically so a failed PID can be mapped back to its name and log file.

### Cleanup

A `trap` on `EXIT` deletes all temp files that still exist, ensuring no orphan files even on unexpected exit.

### Pseudocode

```bash
pids=(); tmpfiles=(); names=()

for s in "${scripts[@]}"; do
  tmp=$(mktemp)
  bash "$s" >"$tmp" 2>&1 &
  pids+=($!)
  tmpfiles+=("$tmp")
  names+=("$(basename "$s")")
done

trap 'rm -f "${tmpfiles[@]}"' EXIT

failed=0
for i in "${!pids[@]}"; do
  wait "${pids[$i]}" || { failed=$i; break; }
done

if (( failed )); then
  # kill siblings
  for j in "${!pids[@]}"; do
    [[ $j -ne $failed ]] && kill "${pids[$j]}" 2>/dev/null || true
  done
  echo "component ${names[$failed]} failed:" >&2
  cat "${tmpfiles[$failed]}" >&2
  exit 1
fi
```

> Note: the loop uses indexed `wait "${pids[$i]}"` rather than `wait -n` because `wait -n` does not return which PID failed — it only returns the exit status of the first finished job. Iterating by index sacrifices true "first failure wins" (it waits for earlier-indexed jobs before noticing a later failure) but is portable to bash 4.x and macOS's bash 3.2. If ordering truly doesn't matter and bash 4.3+ is guaranteed, `wait -n` with a PID-to-index map is an alternative.

## Success criteria

- `local.sh --all-components` completes in wall-clock time close to the slowest single component (not the sum).
- On failure, the terminal shows which script failed and its full captured output.
- On success, no extra output beyond existing per-component banners.
- `local.sh` (without `--all-components`) is unaffected.
