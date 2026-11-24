import { defineConfig, type Plugin } from "vite";
import react from "@vitejs/plugin-react";
import fs from "node:fs";
import path from "node:path";
import zlib from "node:zlib";

function precompressDistPlugin(outDir: string): Plugin {
  const compressible = new Set([".js", ".mjs", ".css", ".html", ".svg", ".json"]);
  const walkAndCompress = (dir: string) => {
    if (!fs.existsSync(dir)) return;
    for (const ent of fs.readdirSync(dir, { withFileTypes: true })) {
      const full = path.join(dir, ent.name);
      if (ent.isDirectory()) {
        walkAndCompress(full);
        continue;
      }
      const ext = path.extname(ent.name).toLowerCase();
      if (!compressible.has(ext)) continue;
      const raw = fs.readFileSync(full);
      if (raw.byteLength < 512) continue;

      const br = zlib.brotliCompressSync(raw, {
        params: { [zlib.constants.BROTLI_PARAM_QUALITY]: 9 },
      });
      if (br.byteLength < raw.byteLength) {
        fs.writeFileSync(full + ".br", br);
      }

      const gz = zlib.gzipSync(raw, { level: 9 });
      if (gz.byteLength < raw.byteLength) {
        fs.writeFileSync(full + ".gz", gz);
      }
    }
  };

  return {
    name: "modhub-precompress",
    apply: "build",
    closeBundle() {
      walkAndCompress(path.resolve(__dirname, outDir));
    },
  };
}

export default defineConfig({
  plugins: [react(), precompressDistPlugin("../backend/webdist")],
  server: {
    port: 5173,
    proxy: {
      "/api": {
        target: "http://127.0.0.1:8080",
        changeOrigin: true,
      },
    },
  },
  build: {
    outDir: "../backend/webdist",
    emptyOutDir: true,
    target: "es2022",
    cssMinify: "esbuild",
    reportCompressedSize: false,
    rollupOptions: {
      output: {
        manualChunks(id) {
          if (id.includes("node_modules/@tanstack/")) {
            return "virtual-vendor";
          }
          if (id.includes("node_modules/react/") || id.includes("node_modules/react-dom/") || id.includes("node_modules/scheduler/")) {
            return "react-vendor";
          }
          return undefined;
        },
      },
    },
  },
});
