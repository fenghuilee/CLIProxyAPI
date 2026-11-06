# Seedance usage recording

The shared Seedance adapter records the upstream task-query `usage` object for
both `volcengine-seedance` and `chinadatapay-seedance` (covering `volcengine/doubao-seedance-*`,
`doubao-seedance-*`, public `cdp/doubao-seedance-*`, and legacy `chinadatapay/doubao-seedance-*`
models routed via code-owned patterns under zero backend configuration). It is stored in the
existing task metadata under `seedance.usage`:

```json
{
  "seedance": {
    "usage": {
      "completion_tokens": 123456,
      "total_tokens": 123456
    }
  }
}
```

This is an illustrative shape, not a guarantee that both token fields are returned.
All actual fields, including nested tool usage and future extensions, are retained.
Missing token totals are not derived or defaulted. Explicit zeros and empty usage
objects are retained. Missing, null or non-object usage is ignored, so older
responses remain compatible and a response without usage does not erase previously
recorded usage. When another usage object is returned, it replaces the recorded
snapshot rather than inventing fields from a previous response.

The host's existing polling metadata merge persists this object in
`content_generations.metadata`, preserving other task metadata. No new table,
column or configuration switch is required. Recording applies to any parsed task
state carrying usage, including failed tasks; it does not mean that failed tasks
are billable. No new frontend display or API endpoint is added.

An operator can inspect the recorded object with a read-only query:

```sql
SELECT generation_id, model,
       JSON_EXTRACT(metadata, '$.seedance.usage') AS seedance_usage
FROM content_generations
WHERE generation_id = '<generation ID>';
```

Existing tasks are not backfilled automatically. Previously completed tasks will
only gain usage if their upstream responses are queried and persisted again through
an explicitly authorized process.

**Billing is unchanged.** Existing resolution/duration formulas, prices, precharges,
settlement and refunds are not modified. The recorded token data is for future
analysis and potential pricing work, not immediate token-based charging.

Load the rebuilt AIGC plugin on a host supporting poll-result metadata persistence
(the current host implementation already does). Verification uses mock responses
and a test store; it does not call a paid upstream, update production records or
overwrite deployed binaries.
