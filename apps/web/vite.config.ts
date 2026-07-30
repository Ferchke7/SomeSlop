import { defineConfig, loadEnv } from "vite";
import react from "@vitejs/plugin-react";
import { VitePWA } from "vite-plugin-pwa";

export default defineConfig(({ mode }) => {
  const envDir = decodeURIComponent(new URL("../..", import.meta.url).pathname).replace(/^\/(?=[A-Za-z]:)/, "");
  const env = loadEnv(mode, envDir, "");
  const webPort = Number(env.WEB_PORT || 5173);
  const apiTarget = env.VITE_API_PROXY_TARGET || `http://${env.API_HOST || "127.0.0.1"}:${env.API_PORT || "8080"}`;

  return {
    envDir,
    plugins: [
      react(),
      VitePWA({
        registerType: "autoUpdate",
        includeAssets: ["favicon.svg", "push-sw.js"],
        manifest: {
          name: "FocusFlow — задачи и фокус",
          short_name: "FocusFlow",
          description: "Задачи, календарь, таймер, заметки и интервальные повторения.",
          theme_color: "#f5f3ee",
          background_color: "#f5f3ee",
          display: "standalone",
          start_url: "/",
          lang: "ru",
          categories: ["productivity", "education"]
        },
        workbox: {
          navigateFallback: "/index.html",
          maximumFileSizeToCacheInBytes: 3 * 1024 * 1024,
          importScripts: ["push-sw.js"],
          runtimeCaching: [
            {
              urlPattern: ({ url }) => url.pathname.startsWith("/api/"),
              handler: "NetworkFirst",
              options: { cacheName: "focusflow-api", networkTimeoutSeconds: 4 }
            }
          ]
        }
      })
    ],
    server: {
      host: env.WEB_HOST || "127.0.0.1",
      port: Number.isFinite(webPort) ? webPort : 5173,
      strictPort: true,
      proxy: {
        "/api": { target: apiTarget, changeOrigin: true },
        "/health": { target: apiTarget, changeOrigin: true }
      }
    },
    build: {
      rollupOptions: {
        output: {
          manualChunks(id) {
            if (id.includes("@blocknote") || id.includes("prosemirror") || id.includes("@tiptap")) return "editor";
            if (id.includes("@fullcalendar")) return "calendar";
            if (id.includes("recharts") || id.includes("d3-")) return "charts";
            if (id.includes("node_modules/react") || id.includes("node_modules/@radix-ui")) return "react-ui";
          }
        }
      }
    }
  };
});
