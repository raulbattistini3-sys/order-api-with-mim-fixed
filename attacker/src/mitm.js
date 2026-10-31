// The man in the middle, as an axios request interceptor. Installed on the
// victim's client, it sees each request after it was signed and before it hits
// the wire. It lets the original through and launches every attack alongside it.
import { runAttack } from "./attacks.js";

export function createMitm({ target, attacks }) {
  const seen = new Set();
  const running = [];

  const tap = (config) => {
    const method = config.method.toUpperCase();
    const key = config.headers.get("Idempotency-Key");
    if (method === "GET" || seen.has(key)) return config; // a 409 retry is the same capture
    seen.add(key);

    const captured = { method, path: config.url, headers: config.headers.toJSON(), body: config.data };
    for (const attack of attacks) {
      running.push(runAttack(attack, captured, target).then((shots) => ({ attack, shots })));
    }
    return config;
  };

  return { tap, done: () => Promise.all(running) };
}

export function summarize(shots) {
  const counts = {};
  for (const s of shots) {
    const label = s.status + (s.replayed ? " (replayed)" : "");
    counts[label] = (counts[label] ?? 0) + 1;
  }
  return Object.entries(counts)
    .map(([label, n]) => (n > 1 ? `${n}× ${label}` : label))
    .join(", ");
}
