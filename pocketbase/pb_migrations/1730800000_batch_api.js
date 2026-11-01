/// <reference path="../pb_data/types.d.ts" />

// Imports write through POST /api/batch. maxRequests bounds
// api.import_batch_size in config.toml. A batch holds the write lock, so its
// timeout stays short, and under the Go client's 10s.
migrate((app) => {
  const settings = app.settings();
  settings.batch.enabled = true;
  settings.batch.maxRequests = 1000;
  settings.batch.timeout = 8;
  app.save(settings);
}, (app) => {
  const settings = app.settings();
  settings.batch.enabled = false;
  app.save(settings);
});
