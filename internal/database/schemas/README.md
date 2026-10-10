# SQLite latest schema definitions

These SQL files are embedded into the server binary and are the authoritative
source for the tables and columns managed by the application. When adding a
table or column, update its definition here; do not add a separate list of
`ALTER TABLE ADD COLUMN` migrations in startup or handlers.

Startup opens an isolated in-memory reference database with the latest SQL,
compares it with the actual SQLite file, and adds missing tables and columns in
one transaction before creating missing indexes and running business queries.
Existing rows, extra tables and extra columns remain in place. Repeated startup
is safe. The process does not change types or constraints of existing columns.

Column additions use their exact definition, including defaults. SQLite rejects
some additions (for example PRIMARY KEY, UNIQUE, STORED generated columns, or a
required column without a suitable default on a populated table). Such failures
roll back structure repair and stop startup with the table/column and cause.
Resolve the definition or explicitly plan a data migration, then restart; the
repair engine does not invent required values or weaken constraints.

Historical data backfills and foreign-key semantic changes are separate from
missing-table/column repair. Their behavior is documented in
`docs/feature/startup-schema-reconcile/`.
