/// <reference path="../pb_data/types.d.ts" />

// The search indexer reads every customer's orders from oldest to newest. Without
// this it would sort the whole table on each poll.
migrate((app) => {
  const orders = app.findCollectionByNameOrId("orders");
  orders.addIndex("idx_orders_created", false, "created, id", "");
  app.save(orders);
}, (app) => {
  const orders = app.findCollectionByNameOrId("orders");
  orders.removeIndex("idx_orders_created");
  app.save(orders);
});
