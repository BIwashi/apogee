import { Suspense } from "react";
import type { Metadata } from "next";
import CrossCuttingDrawer from "./components/CrossCuttingDrawer";
import TopRibbon from "./components/TopRibbon";
import "./globals.css";
import { RefreshProvider } from "./lib/refresh";
import { SSEProvider } from "./lib/sse";
import { ThemeProvider } from "./lib/theme";
import Sidebar from "./sidebar";

export const metadata: Metadata = {
  title: "apogee — Claude Code observability",
  description:
    "The highest vantage point over your Claude Code agents. Real-time observability for multi-agent sessions.",
};

/*
 * Inline theme-init script — runs before React hydrates so the page
 * paints with the correct palette on the first frame. Reads the
 * `apogee:theme` localStorage entry (set by the Appearance control);
 * falls back to `prefers-color-scheme` when the user has not picked a
 * preference yet. Wrapped in try/catch so a locked-down browser (SSR
 * cookies, private mode) never throws and still renders dark.
 *
 * This has to be a string that runs synchronously — `next/script`
 * with `beforeInteractive` only works in the root layout but still
 * races React hydration; emitting a plain <script> as the first child
 * of <head> is the simplest guaranteed pre-hydration path.
 */
const THEME_INIT_SCRIPT = `
(function () {
  try {
    var saved = window.localStorage.getItem('apogee:theme');
    var theme = saved === 'light' || saved === 'dark' ? saved : null;
    if (!theme) {
      theme = window.matchMedia('(prefers-color-scheme: light)').matches
        ? 'light'
        : 'dark';
    }
    document.documentElement.setAttribute('data-theme', theme);
  } catch (e) {
    document.documentElement.setAttribute('data-theme', 'dark');
  }
})();
`.trim();

export default function RootLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return (
    <html lang="en">
      <head>
        <script dangerouslySetInnerHTML={{ __html: THEME_INIT_SCRIPT }} />
      </head>
      <body className="min-h-screen bg-[var(--bg-deepspace)] text-[var(--artemis-white)]">
        <Suspense
          fallback={<div className="min-h-screen bg-[var(--bg-deepspace)]" />}
        >
          <ThemeProvider>
            <SSEProvider>
              <RefreshProvider>
                <TopRibbon />
                <Sidebar>{children}</Sidebar>
                <CrossCuttingDrawer />
              </RefreshProvider>
            </SSEProvider>
          </ThemeProvider>
        </Suspense>
      </body>
    </html>
  );
}
