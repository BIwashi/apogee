import type { NextConfig } from "next";

// The apogee web UI ships embedded inside the Go binary via embed.FS (see
// future PRs). We use `output: "standalone"` so that the Next.js build emits a
// self-contained server directory that the Go embedder can consume without
// pulling node_modules at runtime.
const nextConfig: NextConfig = {
  output: "standalone",
  // API rewrites: /api/* is forwarded to the collector's HTTP server. At dev
  // time this is localhost:8000; in production the frontend and collector are
  // served from the same origin so the rewrite is a no-op.
  async rewrites() {
    return [
      {
        source: "/api/:path*",
        destination:
          (process.env.API_ORIGIN ?? "http://localhost:8000") + "/api/:path*",
      },
    ];
  },
};

export default nextConfig;
