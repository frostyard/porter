// The bundle lodge serves from /usr/share/lodge/apps/porter/web/ at
// /app/porter/ (lodge's app-contract.md, Public URLs).
import { defineConfig } from "vite";

export default defineConfig({
  base: "/app/porter/",
  build: { target: "es2022" },
});
