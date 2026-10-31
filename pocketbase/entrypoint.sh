#!/bin/sh
set -e
PB="/pb/pocketbase --dir=/pb/pb_data --migrationsDir=/pb/pb_migrations"
# Idempotent: creates or updates the superuser the API logs in with.
$PB superuser upsert "$PB_ADMIN_EMAIL" "$PB_ADMIN_PASSWORD"
exec $PB serve --http=0.0.0.0:8090
