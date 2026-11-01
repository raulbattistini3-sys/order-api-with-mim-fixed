// Load test for the gateway's defences. It starts an isolated stack, then
// drives it with the Go and Rust generators and reports what got through.
//
//   node loadtest/run.mjs [--orders N] [--dupes M] [--concurrency C]
//
// It stands up two gateways against one API and database:
//   :19081  open        (no rate limit) — shows idempotency: N orders from N*M requests
//   :19082  rate-limited (--rate/--burst) — shows the flood being shed with 429s
// The API on :19080 has no protection, as the baseline.
//
// Needs Go, a Rust toolchain (first run builds the Rust generator) and
// bin/pocketbase.exe (run `node scripts/run.mjs` once to download it).
import { spawn, spawnSync } from "node:child_process";
import { existsSync, mkdirSync, openSync, rmSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const BIN = path.join(ROOT, "bin");
const WORK = path.join(BIN, "loadtest");
const EXE = process.platform === "win32" ? ".exe" : "";
const PB = path.join(BIN, `pocketbase${EXE}`);
const RUST = path.join(ROOT, "loadtest", "rust", "target", "release", `loadgen${EXE}`);

const PORTS = { pb: 19090, api: 19080, open: 19081, limited: 19082 };
const EMAIL = "admin@local.test";
const PASS = "change-me-please";
const SECRET = "dev-secret-change-me-0123456789";

const opt = (name, fallback) => {
  const i = process.argv.indexOf(`--${name}`);
  return i >= 0 ? process.argv[i + 1] : fallback;
};
const ORDERS = opt("orders", "400");
const DUPES = opt("dupes", "5");
const CONCURRENCY = opt("concurrency", "120");
// Deliberately low so the flood clearly exceeds it and the limiter sheds load.
const RATE = opt("rate", "150");
const BURST = opt("burst", "150");

const env = {
  ...process.env,
  CLIENT_SECRET: SECRET,
  SIGNATURE_MAX_AGE: "60s",
  PB_EMAIL: EMAIL,
  PB_PASSWORD: PASS,
  PB_URL: `http://127.0.0.1:${PORTS.pb}`,
  UPSTREAM: `http://127.0.0.1:${PORTS.api}`,
};

const children = [];
const stop = () => children.forEach((c) => c.kill());
process.on("SIGINT", () => (stop(), process.exit(130)));

try {
  await main();
} catch (err) {
  stop();
  console.error(`\n✖ ${err.message}`);
  process.exit(1);
}

async function main() {
  if (!existsSync(PB)) throw new Error("bin/pocketbase.exe missing; run `node scripts/run.mjs` once first");
  rmSync(path.join(WORK, "pb_data"), { recursive: true, force: true });
  mkdirSync(WORK, { recursive: true });

  console.log("building Go binaries…");
  run("go", ["build", "-o", WORK + path.sep, "./cmd/api", "./cmd/gateway", "./cmd/loadgen"]);
  if (!existsSync(RUST)) {
    console.log("building the Rust generator (first run)…");
    run("cargo", ["build", "--release"], path.join(ROOT, "loadtest", "rust"));
  }

  const flags = [`--dir=${path.join(WORK, "pb_data")}`, `--migrationsDir=${path.join(ROOT, "pocketbase", "pb_migrations")}`, `--hooksDir=${path.join(ROOT, "pocketbase", "pb_hooks")}`];
  run(PB, ["superuser", "upsert", EMAIL, PASS, ...flags]);

  start("pocketbase", PB, ["serve", `--http=127.0.0.1:${PORTS.pb}`, ...flags], {});
  await waitFor(`http://127.0.0.1:${PORTS.pb}/api/health`);
  start("api", path.join(WORK, `api${EXE}`), [], { PORT: String(PORTS.api) });
  start("gateway-open", path.join(WORK, `gateway${EXE}`), [], { PORT: String(PORTS.open), GATEWAY_RATE: "100000000", GATEWAY_BURST: "100000000" });
  start("gateway-limited", path.join(WORK, `gateway${EXE}`), [], { PORT: String(PORTS.limited), GATEWAY_RATE: RATE, GATEWAY_BURST: BURST });
  for (const p of [PORTS.api, PORTS.open, PORTS.limited]) await waitFor(`http://127.0.0.1:${p}/healthz`);
  console.log(`stack up. orders=${ORDERS} dupes=${DUPES} concurrency=${CONCURRENCY} (total ${Number(ORDERS) * Number(DUPES)} requests/run)\n`);

  const gen = (bin, args, label) => ({ bin, args, label });
  const goBin = path.join(WORK, `loadgen${EXE}`);

  // 1. Baseline: the unprotected API. Every request becomes an order.
  const apiCustomer = `api-${Date.now()}`;
  section("1. Baseline — API directly (no protection)");
  runGen(gen(goBin, go(PORTS.api, apiCustomer)));
  const apiCount = await count(apiCustomer);

  // 2. The gateway, open. Idempotency collapses the duplicates.
  const gwCustomer = `gw-${Date.now()}`;
  section("2. Gateway (open) — idempotency collapses duplicates");
  runGen(gen(goBin, go(PORTS.open, gwCustomer)));
  const gwCount = await count(gwCustomer);

  // 3. Flood against the rate-limited gateway. It sheds load with 429s.
  const floodCustomer = `flood-${Date.now()}`;
  section(`3. Flood — rate-limited gateway (${RATE}/s, burst ${BURST})`);
  const floodOut = runGen(gen(goBin, go(PORTS.limited, floodCustomer, CONCURRENCY)));
  const floodCount = await count(floodCustomer);
  const rejected = Number(floodOut.match(/^\s*429\s+(\d+)/m)?.[1] ?? 0);
  const alive = (await fetch(`http://127.0.0.1:${PORTS.limited}/healthz`)).status;

  // 4. Go and Rust at once against the open gateway: two clients, one host.
  section("4. Go and Rust at the same time (open gateway)");
  const goCustomer = `duo-go-${Date.now()}`;
  const rustCustomer = `duo-rs-${Date.now()}`;
  const both = await Promise.all([
    runGenAsync(gen(goBin, go(PORTS.open, goCustomer), "go")),
    runGenAsync(gen(RUST, rust(PORTS.open, rustCustomer), "rust")),
  ]);
  for (const r of both) process.stdout.write(r);
  const goCount = await count(goCustomer);
  const rustCount = await count(rustCustomer);

  const N = Number(ORDERS);
  const M = Number(DUPES);
  console.log("\n════════════════════════════ SUMMARY ════════════════════════════");
  row("API direct", apiCount, `every request landed (≈ ${N * M})`);
  row("Gateway open", gwCount, gwCount === N ? `exactly N=${N} ✓` : `expected N=${N}`);
  row("Flood (limited)", floodCount, `${rejected} requests shed with 429 · gateway up: ${alive === 204 ? "yes ✓" : "NO"}`);
  row("Duo — Go", goCount, goCount === N ? `N=${N} ✓` : `expected N=${N}`);
  row("Duo — Rust", rustCount, rustCount === N ? `N=${N} ✓` : `expected N=${N}`);
  console.log("\n  (A real client retries 429s with Retry-After and its original key, so no order is lost.)");
  const ok = gwCount === N && goCount === N && rustCount === N && apiCount > N && alive === 204 && rejected > 0;
  console.log(ok ? "\n✅ one-order-per-key held under load; the flood was shed with 429s and the gateway stayed up.\n" : "\n❌ see rows above.\n");
  stop();
  process.exit(ok ? 0 : 1);
}

function go(port, customer, concurrency = "80") {
  return ["--target", `http://127.0.0.1:${port}`, "--customer", customer, "--orders", ORDERS, "--dupes", DUPES, "--concurrency", concurrency, "--secret", SECRET];
}
function rust(port, customer) {
  return ["--target", `http://127.0.0.1:${port}`, "--customer", customer, "--orders", ORDERS, "--dupes", DUPES, "--concurrency", "80", "--secret", SECRET];
}

function runGen({ bin, args }) {
  const r = spawnSync(bin, args, { encoding: "utf8" });
  process.stdout.write(r.stdout || "");
  if (r.status !== 0) process.stdout.write(r.stderr || "");
  return r.stdout || "";
}

function runGenAsync({ bin, args, label }) {
  return new Promise((resolve) => {
    const r = spawnSync(bin, args, { encoding: "utf8" });
    resolve(`--- ${label}\n${r.stdout || r.stderr || ""}`);
  });
}

async function count(customer) {
  const res = await fetch(`http://127.0.0.1:${PORTS.api}/orders?customer=${encodeURIComponent(customer)}`);
  return (await res.json()).count;
}

function section(title) {
  console.log(`\n── ${title} ──`);
}
function row(label, count, note) {
  console.log(`  ${label.padEnd(18)} orders=${String(count).padEnd(8)} ${note}`);
}

function run(cmd, args, cwd = ROOT) {
  const r = spawnSync(cmd, args, { cwd, env, encoding: "utf8" });
  if (r.status !== 0) throw new Error(`${cmd} ${args.join(" ")} failed:\n${r.stdout}${r.stderr}`);
}

function start(name, cmd, args, extra) {
  const fd = openSync(path.join(WORK, `${name}.log`), "w");
  children.push(spawn(cmd, args, { cwd: ROOT, env: { ...env, ...extra }, stdio: ["ignore", fd, fd] }));
}

async function waitFor(url) {
  for (let i = 0; i < 150; i++) {
    try {
      if ((await fetch(url)).ok) return;
    } catch {
      // not up yet
    }
    await new Promise((r) => setTimeout(r, 100));
  }
  throw new Error(`timed out waiting for ${url}`);
}
