import { defineConfig } from "vite";
import { svelte } from "@sveltejs/vite-plugin-svelte";
import tailwindcss from "@tailwindcss/vite";

export default defineConfig({
  plugins: [svelte(), tailwindcss()],
  server: {
    port: 3000,
    proxy: {
      "/api/analytics": {
        target: "http://localhost:9000",
        changeOrigin: true,
        rewrite: (path) => path.replace(/^\/api\/analytics/, "/v1/analytics"),
      },
      "/api": {
        target: "http://localhost:9000",
        changeOrigin: true,
        rewrite: (path) => path.replace(/^\/api/, "/v1/media"),
      },
      "/actuator": {
        target: "http://localhost:9000",
        changeOrigin: true,
      },
    },
  },
});
