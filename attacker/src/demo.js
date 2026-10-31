// End-to-end proof. The victim places ONE order with a man in the middle on its
// client, against the API directly and then through the gateway. Then we count
// how many orders actually landed.
//
//   direct to the API  → expect 14 orders (1 + 10 dupes + key-swap + tamper + late)
//   through the gateway → expect exactly 1
import { randomUUID } from "node:crypto";
import axios from "axios";
import { buildAttacks } from "./attacks.js";
import { createClient } from "./client.js";
import { createMitm, summarize } from "./mitm.js";

const API_URL = process.env.API_URL ?? "http://localhost:8080";
const GATEWAY_URL = process.env.GATEWAY_URL ?? "http://localhost:8081";
const SECRET = process.env.CLIENT_SECRET ?? "dev-secret-change-me-0123456789";
const REPLAY_DELAY_MS = Number(process.env.REPLAY_DELAY_MS ?? 6000);

const api = axios.create({ baseURL: API_URL, validateStatus: () => true });

async function scenario(label, target) {
  const customer = `demo-${label}-${Date.now()}`;
  const mitm = createMitm({ target, attacks: buildAttacks({ replayDelayMs: REPLAY_DELAY_MS }) });
  const victim = createClient({ baseURL: target, secret: SECRET, tap: mitm.tap });

  const res = await victim.post(
    "/orders",
    { customer, item: "mechanical keyboard", quantity: 1 },
    { headers: { "Idempotency-Key": randomUUID() } },
  );
  const results = await mitm.done();
  const { data } = await api.get("/orders", { params: { customer } });
  return { label, target, victim: res.status, results, orders: data.count, quantities: data.orders.map((o) => o.quantity) };
}

console.log(`\nvictim → man in the middle → API or gateway; late replay waits ${REPLAY_DELAY_MS / 1000}s…\n`);
const [unprotected, gateway] = await Promise.all([scenario("direct", API_URL), scenario("gateway", GATEWAY_URL)]);

console.log("══════════════════════════════ RESULT ══════════════════════════════");
for (const o of [unprotected, gateway]) {
  console.log(`\n${o.label} ${o.target}   (victim got ${o.victim})`);
  for (const { attack, shots } of o.results) {
    console.log(`  ${attack.name.padEnd(15)} ${attack.what.padEnd(58)} ${summarize(shots)}`);
  }
  console.log(`  → orders created: ${o.orders}   quantities: [${o.quantities.join(", ")}]`);
}

// A 401 on a byte-for-byte copy would mean the tap ran before signing, and the
// gateway would be "winning" against requests no real attacker would send.
const race = gateway.results.find((r) => r.attack.name === "duplicate-race");
const genuine = race.shots.every((s) => s.status !== 401);
const ok = genuine && gateway.orders === 1 && gateway.quantities[0] === 1 && unprotected.orders > 1;
console.log(
  ok
    ? `\n✅ direct to the API created ${unprotected.orders} orders from 1 click; through the gateway, exactly 1.\n`
    : `\n❌ unexpected: gateway created ${gateway.orders} orders; duplicates reached it signed: ${genuine}.\n`,
);
process.exit(ok ? 0 : 1);
