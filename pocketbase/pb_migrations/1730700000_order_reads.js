/// <reference path="../pb_data/types.d.ts" />

// Reads for the orders page. The first index serves cursor pagination, the
// second lets the views group one customer's items without touching the table.
// The views do their sums in SQLite, so nothing downstream counts rows.
migrate((app) => {
  const orders = app.findCollectionByNameOrId("orders");
  orders.addIndex("idx_orders_customer_page", false, "customer, created DESC, id DESC", "");
  orders.addIndex("idx_orders_customer_item", false, "customer, item, quantity", "");
  app.save(orders);

  app.save(new Collection({
    type: "view",
    name: "order_totals",
    listRule: null,
    viewRule: null,
    viewQuery: `SELECT customer AS id, COUNT(*) AS order_count, CAST(SUM(quantity) AS INT) AS units, COUNT(DISTINCT item) AS items
      FROM orders GROUP BY customer`,
  }));

  app.save(new Collection({
    type: "view",
    name: "order_item_totals",
    listRule: null,
    viewRule: null,
    viewQuery: `SELECT (customer || ':' || item) AS id, customer, item, CAST(SUM(quantity) AS INT) AS units, COUNT(*) AS order_count
      FROM orders GROUP BY customer, item`,
  }));
}, (app) => {
  for (const name of ["order_item_totals", "order_totals"]) {
    app.delete(app.findCollectionByNameOrId(name));
  }
  const orders = app.findCollectionByNameOrId("orders");
  orders.removeIndex("idx_orders_customer_item");
  orders.removeIndex("idx_orders_customer_page");
  app.save(orders);
});
