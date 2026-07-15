// Test harness: builds the real glyphuxd binary and the seedcomposition test
// fixture helper, boots a real glyphuxd subprocess, drives the first-run
// setup wizard over raw HTTP, and (since the daemon currently exposes no
// HTTP-level way to declare content types after setup — see
// sdk-js/testdata/seedcomposition/main.go for why) restarts the daemon once
// a content type has been seeded directly via the domain API. No fetch call
// in this file is mocked; this is the real seam the SDK tests run against.
import { type ChildProcessWithoutNullStreams, spawn } from "node:child_process";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { setTimeout as delay } from "node:timers/promises";

const REPO_ROOT = path.resolve(import.meta.dirname, "..", "..");

export interface BuiltBinaries {
  glyphuxd: string;
  seedcomposition: string;
}

/** Builds the glyphuxd daemon and the seedcomposition fixture helper. */
export async function buildBinaries(destDir: string): Promise<BuiltBinaries> {
  const glyphuxd = path.join(destDir, "glyphuxd");
  const seedcomposition = path.join(destDir, "seedcomposition");
  await runGo(["build", "-o", glyphuxd, "./cmd/glyphuxd"]);
  await runGo(["build", "-o", seedcomposition, "./sdk-js/testdata/seedcomposition"]);
  return { glyphuxd, seedcomposition };
}

function runGo(args: string[]): Promise<void> {
  return new Promise((resolve, reject) => {
    const child = spawn("go", args, { cwd: REPO_ROOT, stdio: ["ignore", "pipe", "pipe"] });
    let stderr = "";
    child.stderr.on("data", (chunk) => (stderr += chunk.toString()));
    child.on("error", reject);
    child.on("exit", (code) => {
      if (code === 0) resolve();
      else reject(new Error(`go ${args.join(" ")} failed (${code}): ${stderr}`));
    });
  });
}

export interface RunningDaemon {
  process: ChildProcessWithoutNullStreams;
  baseUrl: string;
}

/** Starts glyphuxd against dataDir on an OS-assigned port and waits until it
 * reports itself listening (parsed from its JSON stdout logs), returning its
 * resolved base URL. */
export function startDaemon(glyphuxdPath: string, dataDir: string): Promise<RunningDaemon> {
  return new Promise((resolve, reject) => {
    const child = spawn(glyphuxdPath, [], {
      cwd: REPO_ROOT,
      env: {
        ...process.env,
        GLYPHUX_DATA_DIR: dataDir,
        GLYPHUX_ADDR: "127.0.0.1:0",
        GLYPHUX_OPEN_BROWSER: "false",
      },
      stdio: ["ignore", "pipe", "pipe"],
    });

    let settled = false;
    let stderr = "";
    let stdoutBuf = "";

    child.stderr.on("data", (chunk) => (stderr += chunk.toString()));
    child.stdout.on("data", (chunk) => {
      stdoutBuf += chunk.toString();
      let idx: number;
      while ((idx = stdoutBuf.indexOf("\n")) !== -1) {
        const line = stdoutBuf.slice(0, idx);
        stdoutBuf = stdoutBuf.slice(idx + 1);
        if (settled || !line.trim()) continue;
        let entry: Record<string, unknown>;
        try {
          entry = JSON.parse(line);
        } catch {
          continue;
        }
        if (entry.msg === "glyphuxd listening" && typeof entry.addr === "string") {
          settled = true;
          resolve({ process: child, baseUrl: `http://${entry.addr}` });
        }
      }
    });
    child.on("error", (err) => {
      if (!settled) reject(err);
    });
    child.on("exit", (code) => {
      if (!settled) reject(new Error(`glyphuxd exited before listening (code ${code}): ${stderr}`));
    });
  });
}

/** Stops a daemon and waits for the process to actually exit. */
export function stopDaemon(daemon: RunningDaemon): Promise<void> {
  return new Promise((resolve) => {
    daemon.process.on("exit", () => resolve());
    daemon.process.kill("SIGTERM");
  });
}

export interface SetupInput {
  siteName: string;
  adminEmail: string;
  adminPassword: string;
}

/** Completes first-run setup by POSTing the wizard's form, exactly as a
 * browser submission would (internal/setup/setup.go handleSubmit). */
export async function completeSetup(baseUrl: string, input: SetupInput): Promise<void> {
  const body = new URLSearchParams({
    site_name: input.siteName,
    admin_email: input.adminEmail,
    admin_password: input.adminPassword,
    database: "sqlite",
  });
  const res = await fetch(`${baseUrl}/setup`, { method: "POST", body });
  if (!res.ok) {
    throw new Error(`POST /setup failed: ${res.status} ${await res.text()}`);
  }
}

/** Runs the seedcomposition fixture helper against a stopped daemon's SQLite
 * file to declare the "post" content type used by the content SDK tests. */
export function seedContentType(seedcompositionPath: string, sqlitePath: string): Promise<void> {
  return new Promise((resolve, reject) => {
    const child = spawn(seedcompositionPath, [sqlitePath], { stdio: ["ignore", "pipe", "pipe"] });
    let stderr = "";
    child.stderr.on("data", (chunk) => (stderr += chunk.toString()));
    child.on("error", reject);
    child.on("exit", (code) => {
      if (code === 0) resolve();
      else reject(new Error(`seedcomposition failed (${code}): ${stderr}`));
    });
  });
}

export async function makeScratchDir(prefix: string): Promise<string> {
  return mkdtemp(path.join(tmpdir(), prefix));
}

export async function removeScratchDir(dir: string): Promise<void> {
  await rm(dir, { recursive: true, force: true });
}

// Give the OS a moment between stopping and restarting the daemon so the
// SQLite file handle is fully released before the seed helper opens it.
export async function settle(ms = 100): Promise<void> {
  await delay(ms);
}
