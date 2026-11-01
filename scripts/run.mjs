// Runs the whole stack without Docker, from any shell (Git Bash, PowerShell, cmd,
// sh). Fetches PocketBase into bin/ on first use, builds the Go commands, starts
// pocketbase + api + gateway, and runs the demo.
//
//   node scripts/run.mjs           run the demo, stop everything, exit with its code
//   node scripts/run.mjs --serve   leave the stack up until Ctrl+C
import { spawn, spawnSync } from "node:child_process";
import { createHash } from "node:crypto";
import { existsSync, mkdirSync, openSync, readFileSync, writeFileSync } from "node:fs";
import net from "node:net";
import path from "node:path";
import { fileURLToPath } from "node:url";

const ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const BIN = path.join(ROOT, "bin");
const LOGS = path.join(BIN, "logs");
const EXE = process.platform === "win32" ? ".exe" : "";
const SERVE = process.argv.includes("--serve");

const PB_EMAIL = "admin@local.test";
const PB_PASSWORD = "change-me-please";
const PB_FLAGS = [
  `--dir=${path.join(BIN, "pb_data")}`,
  `--migrationsDir=${path.join(ROOT, "pocketbase", "pb_migrations")}`,
  `--hooksDir=${path.join(ROOT, "pocketbase", "pb_hooks")}`,
];

const env = {
  ...process.env,
  CLIENT_SECRET: "dev-secret-change-me-0123456789",
  SIGNATURE_MAX_AGE: "5s", // the demo's late replay waits 6s
  PB_EMAIL,
  PB_PASSWORD,
};

const children = [];
const stopAll = () => children.forEach((c) => c.proc.kill());
process.on("SIGINT", () => {
  stopAll();
  process.exit(130);
});

try {
  await main();
} catch (err) {
  stopAll();
  console.error(`\n✖ ${err.message}`);
  process.exit(1);
}

async function main() {
  mkdirSync(LOGS, { recursive: true });
  await assertPortsFree([8090, 8080, 8081]);

  const pocketbase = await ensurePocketBase();
  if (!existsSync(path.join(ROOT, "attacker", "node_modules"))) {
    console.log("installing attacker dependencies…");
    run("npm install", [], { cwd: path.join(ROOT, "attacker"), shell: true });
  }
  console.log("building cmd/api and cmd/gateway…");
  run("go", ["build", "-o", BIN + path.sep, "./cmd/api", "./cmd/gateway"]);
  run(pocketbase, ["superuser", "upsert", PB_EMAIL, PB_PASSWORD, ...PB_FLAGS]);

  start("pocketbase", pocketbase, ["serve", "--http=127.0.0.1:8090", ...PB_FLAGS]);
  await waitFor("http://localhost:8090/api/health");
  start("api", path.join(BIN, `api${EXE}`));
  await waitFor("http://localhost:8080/healthz");
  start("gateway", path.join(BIN, `gateway${EXE}`));
  await waitFor("http://localhost:8081/healthz");
  console.log(`stack is up: pocketbase :8090, api :8080, gateway :8081 (logs in ${LOGS})`);

  if (SERVE) {
    console.log("Ctrl+C to stop.");
    return;
  }
  const demo = spawn(process.execPath, ["src/demo.js"], { cwd: path.join(ROOT, "attacker"), env, stdio: "inherit" });
  const code = await new Promise((resolve) => demo.on("exit", resolve));
  stopAll();
  process.exit(code ?? 1);
}

// Installs the version pinned in pocketbase/Dockerfile, so the two can't drift.
async function ensurePocketBase() {
  const target = path.join(BIN, `pocketbase${EXE}`);
  if (existsSync(target)) return target;

  const version = readFileSync(path.join(ROOT, "pocketbase", "Dockerfile"), "utf8").match(/PB_VERSION=(\S+)/)[1];
  const os = { win32: "windows", darwin: "darwin", linux: "linux" }[process.platform];
  const arch = { x64: "amd64", arm64: "arm64" }[process.arch];
  if (!os || !arch) throw new Error(`no PocketBase download for ${process.platform}/${process.arch}`);

  const zip = `pocketbase_${version}_${os}_${arch}.zip`;
  const base = `https://github.com/pocketbase/pocketbase/releases/download/v${version}`;
  console.log(`downloading ${zip}…`);
  const archive = await download(`${base}/${zip}`);
  const checksums = (await download(`${base}/checksums.txt`)).toString();
  const expected = checksums.split("\n").find((line) => line.trim().endsWith(zip))?.split(/\s+/)[0];
  const actual = createHash("sha256").update(archive).digest("hex");
  if (actual !== expected) throw new Error(`checksum mismatch for ${zip}`);

  const zipPath = path.join(BIN, zip);
  writeFileSync(zipPath, archive);
  // Windows' own tar reads zip files. Git Bash's tar, which may be first on PATH, doesn't.
  if (process.platform === "win32") {
    run(path.join(process.env.SystemRoot, "System32", "tar.exe"), ["-xf", zipPath, "-C", BIN, "pocketbase.exe"]);
  } else {
    run("unzip", ["-oq", zipPath, "pocketbase", "-d", BIN]);
  }
  return target;
}

async function download(url) {
  const res = await fetch(url);
  if (!res.ok) throw new Error(`GET ${url}: ${res.status}`);
  return Buffer.from(await res.arrayBuffer());
}

function run(cmd, args, opts = {}) {
  const res = spawnSync(cmd, args, { cwd: ROOT, env, encoding: "utf8", ...opts });
  if (res.error) throw new Error(`${cmd}: ${res.error.message}`);
  if (res.status !== 0) throw new Error(`${cmd} ${args.join(" ")} failed:\n${res.stdout}${res.stderr}`);
}

function start(name, cmd, args = []) {
  const log = path.join(LOGS, `${name}.log`);
  const fd = openSync(log, "w");
  const proc = spawn(cmd, args, { cwd: ROOT, env, stdio: ["ignore", fd, fd] });
  const child = { name, proc, log, exited: false };
  proc.on("exit", () => (child.exited = true));
  children.push(child);
}

async function waitFor(url) {
  for (let i = 0; i < 100; i++) {
    const dead = children.find((c) => c.exited);
    if (dead) throw new Error(`${dead.name} exited during startup:\n${readFileSync(dead.log, "utf8")}`);
    try {
      if ((await fetch(url)).ok) return;
    } catch {
      // not listening yet
    }
    await new Promise((r) => setTimeout(r, 100));
  }
  throw new Error(`timed out waiting for ${url}`);
}

// A leftover server would answer the health checks and hide the real problem.
async function assertPortsFree(ports) {
  for (const port of ports) {
    const busy = await new Promise((resolve) => {
      const socket = net.connect({ port, host: "localhost" });
      socket.once("connect", () => (socket.destroy(), resolve(true)));
      socket.once("error", () => resolve(false));
    });
    if (busy) throw new Error(`port ${port} is already in use; stop whatever is on it and retry`);
  }
}
