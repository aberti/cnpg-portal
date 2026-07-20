# Screenshot fixture

Repository screenshots must contain synthetic data only.

The current images represent:

- clusters: `primary` and `analytics`;
- namespace: `database`;
- CNPG resources: `postgres-main` and `postgres-analytics`;
- tenants: `billing_api`, `customer_portal`, `event_store`, and
  `worker_queue`;
- identity: `operator@example.com`;
- fixed example backup timestamp: `2026-06-15T09:30:00Z`.

Capture sizes are `1440×1000` for inventory, `1440×1950` for tenant
detail, and `390×1500` for the responsive inventory. The mobile table is
horizontally scrollable by design.

Never capture a live operator portal for public documentation. Before
committing replacements, inspect them visually and scan OCR-visible text
for internal hostnames, real database names, personal email addresses,
credentials, and infrastructure identifiers.
