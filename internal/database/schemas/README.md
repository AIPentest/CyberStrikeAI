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

## Historical data and foreign keys

Historical data backfills and foreign-key semantic changes are separate from
missing-table/column repair. Existing data migrations remain responsible for
time values, token usage, tool guards, and approvals. The historical vulnerability
foreign-key migration creates the destination from the latest definition and
copies existing columns dynamically, preserving extra columns, indexes,
triggers, and dependent records. It is not the source of missing-column checks.

Both application and knowledge databases use the same reconciliation engine.
HITL and notification-read tables are included in the embedded definitions,
so request handlers do not need to create those tables at request time.

## Regression coverage

[Schema reconciliation tests](../schema_reconcile_test.go) cover missing tables
and columns, complex defaults, repeated startup, rollback on failure, and
preservation of existing data and extra schema objects. Keep these properties
when changing definitions or historical migrations.
