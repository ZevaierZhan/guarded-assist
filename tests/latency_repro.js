const { chromium } = require("playwright");
const { spawn } = require("child_process");

const base = process.env.ASSIST_TEST_BASE;
const admin = process.env.ASSIST_ADMIN_TOKEN;
const clientPath = process.env.ASSIST_CLIENT_PATH;
const browserPath = process.env.ASSIST_BROWSER_PATH;
if (!base || !admin || !clientPath || !browserPath) throw new Error("missing test environment");

async function api(path, options = {}) {
  const response = await fetch(base + path, {
    ...options,
    headers: {
      Authorization: `Bearer ${options.token || admin}`,
      "Content-Type": "application/json",
    },
  });
  const body = await response.json();
  if (!response.ok) throw new Error(`${response.status}: ${JSON.stringify(body)}`);
  return body;
}

(async () => {
  const session = await api("/api/requests", {
    method: "POST",
    body: JSON.stringify({ purpose: "connection latency regression" }),
  });
  const view = new URL(session.invite_url).hash.match(/(?:^|&)t=([^&]+)/)[1];
  const browser = await chromium.launch({ headless: true, executablePath: browserPath });
  const page = await browser.newPage();
  let releaseStaleSSE;
  let staleDelivered = false;
  if (process.env.ASSIST_BLOCK_SSE === "1") {
    await page.route("**/events", (route) => route.abort("failed"));
  } else if (process.env.ASSIST_STALE_SSE === "1") {
    const stale = `event: state\ndata: ${JSON.stringify(session)}\n\n`;
    await page.route("**/events", async (route) => {
      if (staleDelivered) return route.continue();
      staleDelivered = true;
      await new Promise((resolve) => { releaseStaleSSE = resolve; });
      await route.fulfill({ status: 200, contentType: "text/event-stream", body: stale });
    });
  }
  const errors = [];
  page.on("pageerror", (error) => errors.push(error.message));
  await page.goto(session.invite_url);
  await page.locator("h1").waitFor();

  const started = performance.now();
  const wallStarted = Date.now();
  const client = spawn(clientPath, [session.launch_uri], { windowsHide: true });
  client.stdout.setEncoding("utf8");
  client.stderr.setEncoding("utf8");
  let clientOutput = "";
  let clientConnectedAt = null;
  client.stdout.on("data", (chunk) => {
    clientOutput += chunk;
    if (clientConnectedAt === null && clientOutput.includes("已连接")) clientConnectedAt = Math.round(performance.now() - started);
  });
  client.stderr.on("data", (chunk) => (clientOutput += chunk));
  let apiLatency = null;
  let domLatency = null;
  let lastState = null;
  for (let i = 0; i < 200; i++) {
    lastState = await api(`/api/requests/${session.id}`);
    if (apiLatency === null && lastState.online) apiLatency = Math.round(performance.now() - started);
    if (await page.locator(".policy-card").count()) {
      domLatency = Math.round(performance.now() - started);
      break;
    }
    await new Promise((resolve) => setTimeout(resolve, 50));
  }
  let staleRegression = false;
  let beforeStale = null;
  let afterStale = null;
  if (process.env.ASSIST_STALE_SSE === "1" && !releaseStaleSSE) throw new Error("SSE request was not captured");
  if (releaseStaleSSE) {
    beforeStale = await page.evaluate(() => ({ seq: S?.seq, id: S?.id, rid, status: S?.status, host: S?.device?.host }));
    releaseStaleSSE();
    await new Promise((resolve) => setTimeout(resolve, 250));
    staleRegression = !(await page.locator(".policy-card").count());
    afterStale = await page.evaluate(() => ({ seq: S?.seq, id: S?.id, rid, status: S?.status, host: S?.device?.host }));
  }
  const lastHeading = await page.locator("h1").first().innerText().catch(() => "<missing>");

  await api(`/api/requests/${session.id}/stop`, {
    method: "POST",
    body: "{}",
    token: view,
  });
  if (client.exitCode === null) await new Promise((resolve) => client.once("exit", resolve));
  await browser.close();

  console.log(`API_ONLINE_LATENCY_MS=${apiLatency}`);
  console.log(`CLIENT_CONNECTED_LATENCY_MS=${clientConnectedAt}`);
  console.log(`SERVER_CONNECTED_LATENCY_MS=${Math.round(new Date((lastState?.events || []).find((e) => e.type === "connected")?.time).getTime() - wallStarted)}`);
  console.log(`WEB_STATE_LATENCY_MS=${domLatency}`);
  console.log(`STALE_SSE_REGRESSION=${staleRegression}`);
  console.log(`BEFORE_STALE=${JSON.stringify(beforeStale)}`);
  console.log(`AFTER_STALE=${JSON.stringify(afterStale)}`);
  console.log(`LAST_STATE=${JSON.stringify({ online: lastState?.online, status: lastState?.status, deviceType: lastState?.device?.type, host: lastState?.device?.host })}`);
  console.log(`EVENT_TIMES=${JSON.stringify((lastState?.events || []).map((event) => ({ type: event.type, ms: new Date(event.time) - new Date(lastState.created) })))}`);
  console.log(`LAST_H1=${JSON.stringify(lastHeading)}`);
  console.log(`CLIENT_OUTPUT=${JSON.stringify(clientOutput.slice(-500))}`);
  console.log(`PAGE_ERRORS=${JSON.stringify(errors)}`);
  if (apiLatency === null || domLatency === null || domLatency - apiLatency > 1000 || staleRegression) process.exitCode = 2;
})().catch((error) => {
  console.error(error.stack || error);
  process.exitCode = 1;
});
