import type { NextConfig } from "next";

/**
 * apogee web UI ships embedded inside the Go binary. We use
 * `output: "export"` so the Next.js build emits a fully static HTML/JS/CSS
 * tree under `web/out`, which the Go collector then embeds via embed.FS and
 * serves from the same origin as the /v1 API.
 *
 * Because the collector and the UI share an origin in production, the API
 * client in `app/lib/api.ts` uses relative paths (`/v1/...`). In dev mode the
 * rewrites below forward `/v1/*` from the Next.js dev server on :3000 to the
 * collector on :4100 so the same relative URLs Just Work there too.
 *
 * Dynamic routes are incompatible with `output: "export"` unless every param
 * is enumerable via generateStaticParams at build time. The session id space
 * is unbounded, so PR #10 rewrites the dynamic routes as query-string pages:
 * `/session?id=<id>` and `/turn?sess=<sess>&turn=<turn>`.
 */
const nextConfig: NextConfig = {
  output: "export",
  trailingSlash: true,
  images: { unoptimized: true },
  async rewrites() {
    return [
      {
        source: "/v1/:path*",
        destination:
          (process.env.API_ORIGIN ?? "http://localhost:4100") + "/v1/:path*",
      },
    ];
  },
};

export default nextConfig;
