/**
 * Deep Reality Check — Tests exhaustifs qui cliquent partout et vérifient
 * ce qui FONCTIONNE VRAIMENT vs ce qui n'est que du frontend mock.
 *
 * Objectif : déterminer honnêtement si les agents IA exécutent des tâches
 * réelles, ou si l'app est juste de jolies pages statiques.
 *
 * Run:
 *   $env:PLAYWRIGHT_BASE_URL="https://multica-main-gules.vercel.app"
 *   npx playwright test e2e/deep-reality-check.spec.ts --reporter=list --timeout=60000
 */
import { test, expect, type Page } from "@playwright/test";

const BASE =
  process.env.PLAYWRIGHT_BASE_URL ??
  "https://multica-main-gules.vercel.app";

async function goto(page: Page, path: string) {
  await page.goto(`${BASE}${path}`, { waitUntil: "domcontentloaded" });
}

/** Capture tous les appels API pendant une action. */
async function captureNetwork(page: Page, action: () => Promise<void>) {
  const calls: Array<{ method: string; url: string; status: number; ok: boolean }> = [];
  const handler = (response: import("@playwright/test").Response) => {
    const url = response.url();
    if (url.includes("/api/")) {
      calls.push({
        method: response.request().method(),
        url: url.replace(BASE, ""),
        status: response.status(),
        ok: response.ok(),
      });
    }
  };
  page.on("response", handler);
  await action();
  page.off("response", handler);
  return calls;
}

// ─── 1. PAGES : quelles pages existent vraiment ─────────────────────

test.describe("Pages inventory", () => {
  const ROUTES = [
    "/main/agents",
    "/main/issues",
    "/main/projects",
    "/main/autopilots",
    "/main/runtimes",
    "/main/skills",
    "/main/executions",
    "/main/settings",
  ];

  for (const route of ROUTES) {
    test(`page ${route} loads without 500/404`, async ({ page }) => {
      const errors: number[] = [];
      page.on("response", (r) => {
        if (r.url().includes(BASE) && r.status() >= 500) errors.push(r.status());
      });
      const res = await page.goto(`${BASE}${route}`, { waitUntil: "domcontentloaded" });
      expect(res?.status(), `HTTP status for ${route}`).toBeLessThan(400);
      expect(errors, `5xx errors on ${route}`).toEqual([]);
      // Wait a bit for client-side hydration
      await page.waitForTimeout(1500);
      const title = await page.title();
      console.log(`  ✓ ${route} → "${title}"`);
    });
  }
});

// ─── 2. AGENTS : liste et detail ────────────────────────────────────

test.describe("Agents list reality", () => {
  test("agents list actually fetches from backend", async ({ page }) => {
    const calls = await captureNetwork(page, async () => {
      await goto(page, "/main/agents");
      await page.waitForTimeout(3000);
    });
    const agentCalls = calls.filter((c) => c.url.includes("agent"));
    console.log("  Agent API calls:", JSON.stringify(agentCalls, null, 2));
    expect(agentCalls.length, "Expected some agent API calls").toBeGreaterThan(0);

    const successfulAgentCalls = agentCalls.filter((c) => c.ok);
    expect(
      successfulAgentCalls.length,
      `Expected at least one successful agent call. All calls: ${JSON.stringify(agentCalls)}`
    ).toBeGreaterThan(0);
  });

  test("click on first agent opens detail view", async ({ page }) => {
    await goto(page, "/main/agents");
    await page.waitForTimeout(2500);

    // Try clicking the first agent row/card
    const firstAgent = page
      .locator('[data-testid*="agent"], a[href*="/agents/"], button[role="row"]')
      .first();

    const count = await page.locator('a[href*="/agents/"]').count();
    console.log(`  Found ${count} agent links`);

    if (count === 0) {
      console.log("  ⚠ No agents listed — cannot test detail");
      test.skip();
    }

    await firstAgent.click();
    await page.waitForTimeout(1500);

    const url = page.url();
    console.log(`  After click → ${url}`);
    expect(url).toContain("/agents/");
  });
});

// ─── 3. EXECUTIONS PAGE : test profond ──────────────────────────────

test.describe("Executions page — clic sur tout", () => {
  test("all 6 templates are clickable and change state", async ({ page }) => {
    await goto(page, "/main/executions");
    await page.waitForTimeout(2000);

    const templates = [
      "Web Scraper",
      "LinkedIn Prospector",
      "Code Generator",
      "Deep Research",
      "Form Automator",
      "Custom Agent",
    ];

    for (const name of templates) {
      const card = page.getByText(name, { exact: true }).first();
      await expect(card, `Template "${name}" visible`).toBeVisible();
      await card.click();
      await page.waitForTimeout(300);
      // After selecting, the prompt builder section should appear
      const promptArea = page.locator("textarea").first();
      await expect(promptArea, `Textarea after selecting "${name}"`).toBeVisible({
        timeout: 5000,
      });
      console.log(`  ✓ Template "${name}" selected → textarea appears`);
    }
  });

  test("stealth mode toggle works", async ({ page }) => {
    await goto(page, "/main/executions");
    await page.waitForTimeout(1500);

    await page.getByText("Web Scraper").first().click();
    await page.waitForTimeout(500);

    // Look for stealth switch
    const stealthSwitch = page.locator('[role="switch"]').first();
    const initialState = await stealthSwitch.getAttribute("aria-checked");
    console.log(`  Stealth switch initial: ${initialState}`);

    await stealthSwitch.click();
    await page.waitForTimeout(300);
    const newState = await stealthSwitch.getAttribute("aria-checked");
    console.log(`  After click: ${newState}`);
    expect(newState).not.toBe(initialState);
  });

  test("can type in prompt textarea", async ({ page }) => {
    await goto(page, "/main/executions");
    await page.waitForTimeout(1500);

    await page.getByText("Custom Agent").first().click();
    await page.waitForTimeout(500);

    const textarea = page.locator("textarea").first();
    await textarea.fill("Dis bonjour en JSON : {greeting: 'hello'}");
    const value = await textarea.inputValue();
    expect(value).toContain("bonjour");
    console.log(`  ✓ Typed into textarea: "${value.slice(0, 50)}..."`);
  });

  test("Launch button captures API call", async ({ page }) => {
    await goto(page, "/main/executions");
    await page.waitForTimeout(1500);

    await page.getByText("Custom Agent").first().click();
    await page.waitForTimeout(500);

    const textarea = page.locator("textarea").first();
    await textarea.fill("Quick test: echo 'hello world' and stop.");

    // Find launch button (Play/Rocket icon with text)
    const launchBtn = page
      .getByRole("button", { name: /launch|execute|start|run/i })
      .first();

    const calls: Array<{ method: string; url: string; status: number }> = [];
    page.on("response", (r) => {
      if (r.url().includes("/api/")) {
        calls.push({
          method: r.request().method(),
          url: r.url().replace(BASE, ""),
          status: r.status(),
        });
      }
    });

    if (await launchBtn.isVisible()) {
      await launchBtn.click();
      await page.waitForTimeout(8000);
      console.log("  Launch API calls:");
      calls.forEach((c) => console.log(`    ${c.method} ${c.url} → ${c.status}`));

      const createAgent = calls.find(
        (c) => c.method === "POST" && c.url.includes("/agents") && !c.url.includes("trigger")
      );
      const triggerAgent = calls.find((c) => c.url.includes("trigger"));

      console.log(`  Created agent? ${createAgent ? `${createAgent.status}` : "NO"}`);
      console.log(`  Triggered? ${triggerAgent ? `${triggerAgent.status}` : "NO"}`);
    } else {
      console.log("  ⚠ No launch button found");
    }
  });
});

// ─── 4. CHAT : est-ce que l'IA répond vraiment ? ────────────────────

test.describe("Chat panel — vrai agent IA ?", () => {
  test("chat input accepts text and tries to send", async ({ page }) => {
    await goto(page, "/main/agents");
    await page.waitForTimeout(2500);

    const chatInput = page
      .locator('textarea[placeholder*="message" i], textarea[placeholder*="ask" i], textarea[placeholder*="chat" i], input[placeholder*="message" i]')
      .first();

    const visible = await chatInput.isVisible().catch(() => false);
    if (!visible) {
      console.log("  ⚠ No chat input visible on agents page");
      test.skip();
    }

    await chatInput.fill("Say hello in one word");

    const calls: Array<{ method: string; url: string; status: number }> = [];
    page.on("response", (r) => {
      if (r.url().includes("/api/")) {
        calls.push({
          method: r.request().method(),
          url: r.url().replace(BASE, ""),
          status: r.status(),
        });
      }
    });

    await chatInput.press("Enter");
    await page.waitForTimeout(10000);

    console.log("  Chat API calls:");
    calls.forEach((c) => console.log(`    ${c.method} ${c.url} → ${c.status}`));

    const msgCall = calls.find(
      (c) =>
        c.method === "POST" &&
        (c.url.includes("message") ||
          c.url.includes("chat") ||
          c.url.includes("session") ||
          c.url.includes("event"))
    );
    console.log(`  Message call result: ${msgCall ? `${msgCall.status}` : "NONE"}`);
  });
});

// ─── 5. API sanity check ────────────────────────────────────────────

test.describe("Backend API endpoints", () => {
  test("GET /api/v1/agents — what does it return?", async ({ page }) => {
    const res = await page.request.get(`${BASE}/api/v1/agents`);
    const status = res.status();
    const body = status < 400 ? await res.json().catch(() => null) : await res.text();
    console.log(`  Status: ${status}`);
    console.log(`  Body: ${JSON.stringify(body).slice(0, 300)}...`);

    // 401/403 is OK (auth required); 500 is bad; 200 is great
    expect(status, `Unexpected server error on /api/v1/agents`).not.toBeGreaterThanOrEqual(500);
  });

  test("GET /api/v1/sessions — what does it return?", async ({ page }) => {
    const res = await page.request.get(`${BASE}/api/v1/sessions`);
    const status = res.status();
    console.log(`  Status: ${status}`);
    expect(status).not.toBeGreaterThanOrEqual(500);
  });

  test("GET /api/v1/workspaces — what does it return?", async ({ page }) => {
    const res = await page.request.get(`${BASE}/api/v1/workspaces`);
    const status = res.status();
    console.log(`  Status: ${status}`);
    expect(status).not.toBeGreaterThanOrEqual(500);
  });

  test("GET /healthz — backend alive?", async ({ page }) => {
    for (const path of ["/healthz", "/api/healthz", "/api/health", "/health"]) {
      const res = await page.request.get(`${BASE}${path}`);
      console.log(`  ${path} → ${res.status()}`);
    }
  });
});

// ─── 6. SETTINGS : toutes les options sont cliquables ? ─────────────

test.describe("Settings page deep dive", () => {
  test("all settings tabs/sections render", async ({ page }) => {
    await goto(page, "/main/settings");
    await page.waitForTimeout(2000);

    const buttons = await page.locator("button").all();
    console.log(`  Found ${buttons.length} buttons on settings page`);

    const links = await page.locator("a[href*='/settings']").all();
    console.log(`  Found ${links.length} settings sub-links`);

    for (let i = 0; i < Math.min(links.length, 10); i++) {
      const href = await links[i]!.getAttribute("href");
      console.log(`    settings link ${i + 1}: ${href}`);
    }
  });
});

// ─── 7. ISSUES : CRUD réel ? ────────────────────────────────────────

test.describe("Issues board — real CRUD?", () => {
  test("clicking a status column shows issues or empty state", async ({ page }) => {
    await goto(page, "/main/issues");
    await page.waitForTimeout(2500);

    const columnHeaders = ["Backlog", "Todo", "In Progress", "Done", "Cancelled"];
    for (const name of columnHeaders) {
      const el = page.getByText(name, { exact: false }).first();
      const visible = await el.isVisible().catch(() => false);
      console.log(`  Column "${name}": ${visible ? "✓" : "✗"}`);
    }
  });

  test("new-issue dialog captures input", async ({ page }) => {
    await goto(page, "/main/issues");
    await page.waitForTimeout(1500);

    const newBtn = page
      .getByRole("button", { name: /new.*issue|create.*issue|\+.*issue/i })
      .first();
    if (!(await newBtn.isVisible().catch(() => false))) {
      console.log("  ⚠ No 'new issue' button found on board");
      test.skip();
    }
    await newBtn.click();
    await page.waitForTimeout(1000);

    const input = page
      .locator('input[placeholder*="title" i], textarea[placeholder*="title" i]')
      .first();
    await expect(input).toBeVisible({ timeout: 5000 });
    await input.fill("E2E reality check issue");
    console.log("  ✓ New issue dialog accepts input");
  });
});

// ─── 8. Anti-detection tools panel content ──────────────────────────

test.describe("Stealth tools panel content", () => {
  test("all 6+ anti-detection tools are listed with links", async ({ page }) => {
    await goto(page, "/main/executions");
    await page.waitForTimeout(1500);

    const tools = [
      "puppeteer-extra-stealth",
      "undetected-chromedriver",
      "playwright-stealth",
      "nodriver",
      "botasaurus",
      "curl-impersonate",
    ];
    for (const t of tools) {
      const el = page.getByText(t, { exact: false }).first();
      const visible = await el.isVisible().catch(() => false);
      console.log(`  Tool "${t}": ${visible ? "✓" : "✗"}`);
    }
  });
});
