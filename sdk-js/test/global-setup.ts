// Vitest global setup: builds the real glyphuxd binary, boots it as a real
// subprocess against a scratch SQLite data dir, completes first-run setup
// over raw HTTP, seeds a "post" content type (see harness.ts /
// sdk-js/testdata/seedcomposition), and leaves a long-lived daemon running
// for the whole test run. Test files talk to it over real fetch — nothing
// here is mocked.
import path from "node:path";
import { writeFile } from "node:fs/promises";
import {
  buildBinaries,
  completeSetup,
  makeScratchDir,
  removeScratchDir,
  seedContentType,
  settle,
  startDaemon,
  stopDaemon,
  type RunningDaemon,
} from "./harness.js";

export const ADMIN_EMAIL = "admin@example.com";
export const ADMIN_PASSWORD = "correct horse battery staple";
export const SITE_NAME = "SDK Test Site";

export const ENV_FILE = path.join(process.env.TMPDIR ?? "/tmp", "glyphux-sdk-test-env.json");

export default async function globalSetup() {
  const buildDir = await makeScratchDir("glyphux-sdk-build-");
  const dataDir = await makeScratchDir("glyphux-sdk-data-");

  const { glyphuxd, seedcomposition } = await buildBinaries(buildDir);

  // First boot: complete the wizard over real HTTP, exactly as a browser
  // would (per the confirmed seam).
  let daemon: RunningDaemon = await startDaemon(glyphuxd, dataDir);
  await completeSetup(daemon.baseUrl, {
    siteName: SITE_NAME,
    adminEmail: ADMIN_EMAIL,
    adminPassword: ADMIN_PASSWORD,
  });
  await stopDaemon(daemon);
  await settle();

  // Seed the "post" content type directly via the domain API (see
  // sdk-js/testdata/seedcomposition/main.go for why this HTTP-level gap
  // exists) while the daemon is stopped, so there is only ever one writer
  // of the SQLite file at a time.
  const sqlitePath = path.join(dataDir, "glyphux.db");
  await seedContentType(seedcomposition, sqlitePath);
  await settle();

  // Second boot: the long-lived daemon the actual SDK tests run against.
  daemon = await startDaemon(glyphuxd, dataDir);

  await writeFile(
    ENV_FILE,
    JSON.stringify({
      baseUrl: daemon.baseUrl,
      adminEmail: ADMIN_EMAIL,
      adminPassword: ADMIN_PASSWORD,
    }),
    "utf8",
  );

  return async () => {
    await stopDaemon(daemon);
    await removeScratchDir(buildDir);
    await removeScratchDir(dataDir);
  };
}
