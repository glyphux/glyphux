// Reads the running test daemon's connection info written by global-setup.ts.
// Read fresh (not cached at import time) since ESM module state can be
// re-evaluated per test file under some Vitest pool configurations.
import { readFileSync } from "node:fs";
import { ENV_FILE } from "./global-setup.js";

export interface TestEnv {
  baseUrl: string;
  adminEmail: string;
  adminPassword: string;
}

export function testEnv(): TestEnv {
  const raw = readFileSync(ENV_FILE, "utf8");
  return JSON.parse(raw) as TestEnv;
}
