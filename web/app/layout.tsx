import type { Metadata } from "next";
import { Suspense } from "react";

import "./globals.css";
import Sidebar from "./sidebar";
import TopRibbon from "./components/TopRibbon";
import { RefreshProvider } from "./lib/refresh";
import { SSEProvider } from "./lib/sse";

export const metadata: Metadata = {
  title: "apogee — Claude Code observability",
  description:
    "The highest vantage point over your Claude Code agents. Real-time observability for multi-agent sessions.",
};

export default function RootLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return (
    <html lang="en" className="dark">
      <body className="min-h-screen bg-[var(--bg-deepspace)] text-gray-100">
        <Suspense fallback={<div className="min-h-screen bg-[var(--bg-deepspace)]" />}>
          <SSEProvider>
            <RefreshProvider>
              <TopRibbon />
              <Sidebar>{children}</Sidebar>
            </RefreshProvider>
          </SSEProvider>
        </Suspense>
      </body>
    </html>
  );
}
