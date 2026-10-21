// The bundle's unit tests, in happy-dom (make web-check, in the nsl machine).
import { defineConfig } from "vitest/config";

export default defineConfig({
  test: {
    environment: "happy-dom",
    include: ["test/**/*.test.ts"],
  },
});
