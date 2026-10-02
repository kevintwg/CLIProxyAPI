import assert from "node:assert/strict";
import { readFile, mkdir, writeFile } from "node:fs/promises";
import { join } from "node:path";
import { chromium } from "@playwright/test";

if (
  !process.env.DASHBOARD_PROOF_ACCESS ||
  !process.env.DASHBOARD_EVIDENCE_DIR
) {
  throw new Error(
    "Set DASHBOARD_PROOF_ACCESS to the proof runner's access.json and DASHBOARD_EVIDENCE_DIR to a private output directory.",
  );
}
const access = JSON.parse(
  await readFile(process.env.DASHBOARD_PROOF_ACCESS, "utf8"),
);
const output = process.env.DASHBOARD_EVIDENCE_DIR;
await mkdir(output, { recursive: true });
const browser = await chromium.launch({ headless: true });
const errors = [];
const context = await browser.newContext({
  viewport: { width: 1440, height: 1000 },
  colorScheme: "light",
  reducedMotion: "reduce",
  recordVideo: { dir: output, size: { width: 1440, height: 1000 } },
});
const page = await context.newPage();
page.on("pageerror", (error) => errors.push(error.message));
const url = `http://127.0.0.1:${access.port}/dashboard/`;

async function unlock() {
  await page
    .getByRole("button", { name: "Connect gateway", exact: true })
    .first()
    .click();
  await page.getByLabel("Management key", { exact: true }).fill(access.key);
  await page.getByRole("button", { name: "Connect", exact: true }).click();
  await page.getByText("Gateway connected", { exact: true }).first().waitFor();
}
async function capture(name) {
  await page.evaluate(() => window.scrollTo(0, 0));
  await page.evaluate(() => {
    document.querySelector('[aria-label="Dismiss notification"]')?.click();
  });
  await page.locator(".toast").waitFor({ state: "detached", timeout: 3000 });
  await page.screenshot({
    path: join(output, `${name}.png`),
    fullPage: !(await page.getByRole("dialog").isVisible()),
    animations: "disabled",
  });
}
async function navigate(name) {
  if (await page.getByRole("button", { name: "Open navigation" }).isVisible()) {
    await page.getByRole("button", { name: "Open navigation" }).click();
  }
  await page
    .getByRole("navigation", { name: "Main navigation" })
    .getByRole("button", { name: new RegExp(`^${name}`) })
    .click();
}
try {
  await page.goto(url);
  await capture("relay-setup");
  await page
    .getByRole("button", { name: "Connect gateway", exact: true })
    .first()
    .click();
  await page
    .getByLabel("Management key", { exact: true })
    .fill("deliberately-wrong-example-key");
  await page.getByRole("button", { name: "Connect", exact: true }).click();
  await page
    .getByRole("alert")
    .filter({ hasText: "Management key was not accepted" })
    .waitFor();
  await page.getByLabel("Management key", { exact: true }).fill(access.key);
  await page.getByRole("button", { name: "Connect", exact: true }).click();
  await page.getByText("Gateway connected", { exact: true }).first().waitFor();
  await page.getByRole("button", { name: /Claude.*1 account/ }).waitFor();
  await capture("relay-overview");
  await navigate("Accounts");
  const pause = page.getByRole("button", { name: /^Pause / }).first();
  const resumeName = (await pause.getAttribute("aria-label")).replace(
    /^Pause /,
    "Resume ",
  );
  await pause.click();
  await page.getByRole("button", { name: resumeName, exact: true }).waitFor();
  await page.getByRole("button", { name: "Paused", exact: true }).click();
  assert.equal(await page.locator(".account-row").count(), 1);
  await page.reload();
  await unlock();
  await navigate("Accounts");
  await page.getByRole("button", { name: resumeName, exact: true }).click();
  await page
    .getByRole("button", {
      name: resumeName.replace(/^Resume /, "Pause "),
      exact: true,
    })
    .waitFor();
  await capture("relay-accounts");
  await navigate("Models");
  await page.locator(".model-row").first().waitFor();
  const allModels = await page.locator(".model-row").count();
  assert.ok(allModels > 1);
  await page.getByRole("textbox", { name: "Search models" }).fill("claude");
  const filteredModels = await page.locator(".model-row").count();
  assert.ok(filteredModels > 0 && filteredModels < allModels);
  await capture("relay-models");
  await navigate("Settings");
  await page.getByRole("radio", { name: /One account at a time/ }).check();
  await page.getByRole("button", { name: "Save changes" }).click();
  await page.getByText("Routing preference saved").waitFor();
  await page.reload();
  await unlock();
  await navigate("Settings");
  assert.equal(
    await page
      .getByRole("radio", { name: /One account at a time/ })
      .isChecked(),
    true,
  );
  await page.getByRole("radio", { name: /Share the work/ }).check();
  await page.getByRole("button", { name: "Save changes" }).click();
  await page.getByText("Routing preference saved").waitFor();
  await page.getByRole("radio", { name: /Use account weights/ }).check();
  const firstAccount = page.locator(".account-routing").first();
  await firstAccount.getByLabel("Weight (zero skips this account)").fill("3");
  await firstAccount
    .getByRole("button", { name: "Save account", exact: true })
    .click();
  await page.getByText("Account routing saved", { exact: true }).waitFor();
  await page.getByRole("button", { name: "Dismiss notification" }).click();
  await page.locator(".toast").waitFor({ state: "detached" });
  await page.getByRole("radio", { name: /Use subscription order/ }).check();
  await firstAccount
    .getByLabel("Tier rank (blank uses detected plan)")
    .fill("1");
  await firstAccount
    .getByRole("button", { name: "Save account", exact: true })
    .click();
  await page.getByText("Account routing saved", { exact: true }).waitFor();
  await page.getByRole("button", { name: "Dismiss notification" }).click();
  await page.locator(".toast").waitFor({ state: "detached" });
  await page
    .getByLabel("Keep a conversation on the same account", { exact: true })
    .check();
  await page.getByLabel("Conversation affinity lifetime").fill("2h");
  await page.getByRole("button", { name: "Save changes", exact: true }).click();
  await page.getByText("Routing preference saved", { exact: true }).waitFor();
  await page.reload();
  await unlock();
  await navigate("Settings");
  assert.equal(
    await page
      .getByRole("radio", { name: /Use subscription order/ })
      .isChecked(),
    true,
  );
  assert.equal(
    await page
      .getByLabel("Keep a conversation on the same account", { exact: true })
      .isChecked(),
    true,
  );
  assert.ok(
    (
      await page.getByLabel("Conversation affinity lifetime").inputValue()
    ).startsWith("2h"),
  );
  assert.equal(
    await page
      .locator(".account-routing")
      .first()
      .getByLabel("Tier rank (blank uses detected plan)")
      .inputValue(),
    "1",
  );
  await capture("relay-settings");
  await page.setViewportSize({ width: 390, height: 844 });
  await capture("relay-settings-mobile");
  assert.equal(
    await page.evaluate(
      () => document.documentElement.scrollWidth > innerWidth,
    ),
    false,
  );
  await page.setViewportSize({ width: 1440, height: 1000 });
  await page
    .getByRole("button", { name: "Connect account", exact: true })
    .click();
  await page.getByRole("dialog", { name: "Connect an account" }).waitFor();
  await page.getByRole("button", { name: "Claude", exact: true }).click();
  assert.equal(
    await page
      .getByRole("button", { name: "Claude", exact: true })
      .getAttribute("aria-pressed"),
    "true",
  );
  await capture("relay-connect");
  await page.keyboard.press("Escape");
  assert.equal(await page.getByRole("dialog").count(), 0);
  await navigate("Overview");
  await page.emulateMedia({ colorScheme: "dark" });
  await capture("relay-dark");
  await page.setViewportSize({ width: 390, height: 844 });
  await page.emulateMedia({ colorScheme: "light" });
  await capture("relay-mobile");
  assert.equal(
    await page.evaluate(
      () => document.documentElement.scrollWidth > innerWidth,
    ),
    false,
  );
  await navigate("Accounts");
  await capture("relay-mobile-accounts");
  await navigate("Settings");
  await page.getByRole("button", { name: "Disconnect", exact: true }).click();
  await page.getByText("Connect your gateway first").waitFor();
  assert.deepEqual(
    await page.evaluate(() => ({
      local: localStorage.length,
      session: sessionStorage.length,
    })),
    { local: 0, session: 0 },
  );
  assert.deepEqual(errors, []);
  await writeFile(
    join(output, "browser-checks.json"),
    JSON.stringify(
      {
        result: "pass",
        accounts: "real pause, reload, resume",
        models: { allModels, filteredModels },
        routing: "saved, reloaded and verified",
        rejectedKey: "verified",
        providerPicker:
          "verified; provider login completion tested with substitutes",
        mobileOverflow: false,
        browserErrors: errors,
      },
      null,
      2,
    ),
  );
  console.log(
    "Browser checks passed: real account persistence, routing persistence, model filtering, auth rejection, provider picker, mobile and session clearing.",
  );
} finally {
  await context.close();
  await browser.close();
}
