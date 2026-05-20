import { defineConfig } from "vite";
import { svelte } from "@sveltejs/vite-plugin-svelte";
import { localOpsPlugin } from "./dev/local-ops/server";

export default defineConfig({
  plugins: [localOpsPlugin(), svelte()],
  server: {
    host: "127.0.0.1",
    port: 3001,
  },
});
