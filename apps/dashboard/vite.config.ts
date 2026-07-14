import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      // Dev convenience: same-origin API calls, no CORS in the loop.
      "/admin": "http://localhost:8080",
      "/health": "http://localhost:8080",
    },
  },
});
