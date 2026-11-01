/// <reference path="../pb_data/types.d.ts" />

// The background import queue. Jobs live here so they survive a restart, and the
// oldest unfinished job always runs next. Only the Go API touches this.
migrate((app) => {
  const users = app.findCollectionByNameOrId("users");
  const jobs = new Collection({
    type: "base",
    name: "import_jobs",
    listRule: null,
    viewRule: null,
    createRule: null,
    updateRule: null,
    deleteRule: null,
    fields: [
      { name: "user", type: "relation", required: true, maxSelect: 1, collectionId: users.id, cascadeDelete: true },
      { name: "company", type: "text", max: 15 },
      { name: "batch", type: "text", required: true, max: 200 },
      { name: "spool", type: "text", required: true, max: 64 },
      { name: "status", type: "text", required: true, max: 20 },
      { name: "total", type: "number" },
      { name: "processed", type: "number" },
      { name: "imported", type: "number" },
      { name: "duplicates", type: "number" },
      { name: "failed", type: "number" },
      { name: "skipped", type: "json", maxSize: 20000 },
      { name: "created", type: "autodate", onCreate: true, onUpdate: false },
      { name: "updated", type: "autodate", onCreate: true, onUpdate: true },
    ],
    indexes: ["CREATE INDEX idx_import_jobs_status ON import_jobs (status, created)"],
  });
  app.save(jobs);
}, (app) => {
  app.delete(app.findCollectionByNameOrId("import_jobs"));
});
