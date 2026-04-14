// Playwright screenshot capture for the apogee dashboard. Driven by
// scripts/capture-screenshots.sh which boots the collector, posts the
// fixture batch, then invokes this script.
//
// Inputs (env vars):
//   APOGEE_BASE_URL  default http://127.0.0.1:4977
//   OUT_DIR          absolute path to assets/screenshots
//
// Output: four PNG files written under OUT_DIR.

import { chromium } from "playwright";
import { mkdirSync } from "node:fs";
import { dirname, join } from "node:path";

const BASE = process.env.APOGEE_BASE_URL ?? "http://127.0.0.1:4977";
const OUT = process.env.OUT_DIR;
if (!OUT) {
  console.error("OUT_DIR env var is required");
  process.exit(1);
}
mkdirSync(OUT, { recursive: true });

function out(name) {
  const p = join(OUT, name);
  mkdirSync(dirname(p), { recursive: true });
  return p;
}

async function shot(page, name) {
  const path = out(name);
  await page.screenshot({ path, fullPage: false });
  console.log("wrote", path);
}

async function waitNetIdle(page, ms = 800) {
  await page.waitForLoadState("networkidle").catch(() => {});
  await page.waitForTimeout(ms);
}

(async () => {
  const browser = await chromium.launch({ headless: true });
  const ctx = await browser.newContext({
    viewport: { width: 1600, height: 1000 },
    deviceScaleFactor: 2,
    colorScheme: "dark",
  });
  const page = await ctx.newPage();

  // 1. Dashboard overview.
  await page.goto(BASE + "/", { waitUntil: "domcontentloaded" });
  await waitNetIdle(page, 1500);
  await shot(page, "dashboard-overview.png");

  // 2. Session detail — pick the first session via the /sessions catalog.
  await page.goto(BASE + "/sessions", { waitUntil: "domcontentloaded" });
  await waitNetIdle(page, 1200);

  // The session catalog renders rows with anchor hrefs to /session/?id=...
  // Fall back to the API if the DOM does not surface one.
  let sessHref = await page
    .locator('a[href*="/session/?id="]')
    .first()
    .getAttribute("href")
    .catch(() => null);
  if (!sessHref) {
    const resp = await page.request.get(BASE + "/v1/sessions/recent");
    const json = await resp.json();
    const first = json?.sessions?.[0];
    if (!first) throw new Error("no sessions to screenshot");
    sessHref = `/session/?id=${first.session_id}`;
  }
  await page.goto(BASE + sessHref, { waitUntil: "domcontentloaded" });
  await waitNetIdle(page, 1500);
  await shot(page, "session-detail.png");

  // 3. Turn detail — grab the latest turn for the same session.
  const sessId = new URL(BASE + sessHref).searchParams.get("id");
  const turnsResp = await page.request.get(
    BASE + `/v1/sessions/${sessId}/turns`,
  );
  const turnsJson = await turnsResp.json();
  const latestTurn = turnsJson?.turns?.[0];
  if (latestTurn) {
    await page.goto(
      BASE + `/turn/?sess=${sessId}&turn=${latestTurn.turn_id}`,
      { waitUntil: "domcontentloaded" },
    );
    await waitNetIdle(page, 1500);
    await shot(page, "turn-detail.png");
  } else {
    console.warn("no turns; skipping turn-detail");
  }

  // 4. Command palette: navigate back to / and dispatch Meta+K.
  await page.goto(BASE + "/", { waitUntil: "domcontentloaded" });
  await waitNetIdle(page, 800);
  // Use real keyboard so the listener (registered with addEventListener) fires.
  await page.keyboard.down("Meta");
  await page.keyboard.press("K");
  await page.keyboard.up("Meta");
  await page.waitForTimeout(600);
  await shot(page, "command-palette.png");

  await browser.close();
  console.log("ok");
})().catch((err) => {
  console.error(err);
  process.exit(1);
});
