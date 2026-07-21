import { defineConfig } from "vitest/config";

export default defineConfig({
  test: {
    include: ["test/**/*.test.ts"],
    testTimeout: 20_000,
    hookTimeout: 60_000,
    globalSetup: ["test/global-setup.ts"],
    fileParallelism: false,
  },
});
