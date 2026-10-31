/// <reference path="../pb_data/types.d.ts" />

// Both collections are superuser-only (all rules null). The Go API is the only
// thing that talks to PocketBase, authenticated as a superuser.
migrate((app) => {
  const orders = new Collection({
    type: "base",
    name: "orders",
    listRule: null,
    viewRule: null,
    createRule: null,
    updateRule: null,
    deleteRule: null,
    fields: [
      { name: "customer", type: "text", required: true, max: 100 },
      { name: "item", type: "text", required: true, max: 200 },
      { name: "quantity", type: "number", required: true, onlyInt: true, min: 1 },
      // Empty for orders that didn't come through the gateway.
      { name: "idempotency_key", type: "text", max: 255 },
      { name: "created", type: "autodate", onCreate: true, onUpdate: false },
    ],
    indexes: [
      // Last line of defence: even if the idempotency_keys bookkeeping fails
      // half-way, a second order with the same key cannot be inserted.
      "CREATE UNIQUE INDEX idx_orders_idempotency_key ON orders (idempotency_key) WHERE idempotency_key != ''",
      "CREATE INDEX idx_orders_customer ON orders (customer)",
    ],
  });
  app.save(orders);

  const keys = new Collection({
    type: "base",
    name: "idempotency_keys",
    listRule: null,
    viewRule: null,
    createRule: null,
    updateRule: null,
    deleteRule: null,
    fields: [
      { name: "key", type: "text", required: true, max: 255 },
      // sha256 of method + path + body: same key with a different payload is rejected.
      { name: "fingerprint", type: "text", required: true, max: 64 },
      { name: "status", type: "select", required: true, maxSelect: 1, values: ["pending", "completed"] },
      { name: "response_status", type: "number", onlyInt: true },
      { name: "response_body", type: "text", max: 100000 },
      { name: "created", type: "autodate", onCreate: true, onUpdate: false },
      { name: "updated", type: "autodate", onCreate: true, onUpdate: true },
    ],
    indexes: [
      // The lock. Concurrent duplicates race on this insert and only one wins.
      "CREATE UNIQUE INDEX idx_idempotency_keys_key ON idempotency_keys (key)",
    ],
  });
  app.save(keys);
}, (app) => {
  for (const name of ["idempotency_keys", "orders"]) {
    app.delete(app.findCollectionByNameOrId(name));
  }
});
