# Asset Management

[中文](../zh-CN/asset-management.md)

Asset management consolidates domains, IP addresses, ports, and services discovered through manual entry, network-space search engines, HTTP APIs, and Agent tasks into a maintainable baseline. It answers three questions: what assets exist, which assets have been assessed, and where risk is concentrated.

> This feature is designed for security testing and attack-surface governance. It is not a replacement for a full enterprise CMDB. Add and scan only systems you own or are explicitly authorized to test.

## Overview

Asset management provides three main views:

- **Overview**: asset totals, IPs, domains, ports, recent changes, scan coverage, and protocol distribution.
- **Asset inventory**: identity, service details, source, tags, project ownership, responsibility and business metadata, scan history, and risk state.
- **Reconnaissance**: search FOFA, ZoomEye, Quake, or Shodan and save confirmed results individually or in batches.

Assets can launch single-target analysis or batch scans. After an Agent records findings and completes the scan callback, the inventory displays related vulnerability counts, risk level, and latest scan time.

The overview can show the last 7, 30, or 90 days. It includes added/inactive asset trends, vulnerability discovery trends (including critical/high findings), total and 30-day scan coverage, never-scanned and stale counts, and the top eight protocols. Every statistic is restricted to the current user's accessible assets.

## Asset fields

An asset can include:

- host, IP address, domain, port, and protocol;
- page title and service or product fingerprint;
- country/region, state/province, and city;
- responsible person, department, business system, environment, and criticality;
- source, source query, and tags;
- active or inactive status;
- project and owner;
- first seen, last seen, created, and updated timestamps;
- latest scan time and linked conversation, task queue, and subtask;
- related vulnerability count and current risk level.

At least one of `host`, `ip`, or `domain` is required.

## Build an asset baseline

### Add an asset manually

Go to **Asset Management → Asset Inventory** and select **Add Asset**. Supported target forms include:

```text
https://example.com:8443
example.com
192.0.2.10:443
[2001:db8::1]:443
```

The system attempts to identify the URL, domain, IP address, port, and protocol. You can then add a project, tags, title, service fingerprint, location, responsible person, department, business system, environment, criticality, and status.

### Import from a spreadsheet

Go to **Asset Management → Asset Inventory** and select **Bulk Import**:

1. Download the XLSX (recommended) or CSV template.
2. Enter assets in the `Assets` sheet without changing the header row.
3. Choose the completed file or drop it onto the upload area.
4. Review row-level validation. Duplicates, invalid values, and inaccessible projects are marked as errors.
5. Select **Import valid rows**. Invalid rows are not submitted. When the file contains more than 100 rows, the preview shows the first 100 while submission processes every valid row.
6. Review the created, updated, and skipped counts.

Template columns:

| Column | Required | Description |
| --- | --- | --- |
| `target` | Conditional | URL, domain, IPv4, IPv6, or a target with a port; required when `host`, `ip`, and `domain` are all empty |
| `project` | No | Exact name or ID of an existing project; leave blank for no project |
| `tags` | No | Comma, semicolon, or pipe-separated; up to 30 tags and 64 characters per tag |
| `host` | Conditional | Full URL or host; may supplement `target` |
| `ip` | Conditional | Valid IPv4 or IPv6 address |
| `domain` | Conditional | Valid domain; internationalized domains are normalized |
| `port` | No | `0-65535`; may be inferred from `target` |
| `protocol` | No | Such as `http`, `https`, or `ssh`; may be inferred from a URL or common port |
| `title` | No | Page title, up to 500 characters |
| `server` | No | Service or product fingerprint |
| `country` / `province` / `city` | No | Location metadata |
| `responsible_person` | No | Responsible person, up to 255 characters |
| `department` | No | Responsible department, up to 255 characters |
| `business_system` | No | Owning business system, up to 255 characters |
| `environment` | No | `production`, `staging`, `testing`, `development`, or `other` |
| `criticality` | No | `critical`, `high`, `medium`, or `low` |
| `status` | No | `active` or `inactive`; defaults to `active` |

The parser recognizes the template's English headers and common Chinese aliases. Environment and criticality columns also accept their corresponding Chinese values. Automated exports should keep the English headers and enum values to avoid ambiguous mappings.

Limits and behavior:

- One XLSX/CSV file may contain up to 100,000 rows and be up to 100 MB.
- One `/api/assets/import` request may contain up to 100,000 assets.
- Later rows with the same “IP + domain + port + protocol” (or host when both IP and domain are missing) in one file are marked as duplicates and are not submitted.
- The Web UI parses and previews the file; the server remains responsible for authorization, validation, normalization, deduplication, and transactional writes.
- Existing assets receive non-empty incoming fields and a refreshed last-seen time instead of a duplicate record.
- Bulk import requires `asset:write`. Referenced projects must also be accessible to the current user.
- Do not remove the server-side row limit. Split larger datasets and import them during a low-traffic window.

### Import from network-space search

1. Configure the relevant API key in the configuration file or under **System Settings → Asset Management**. Environment variables are also supported: `FOFA_API_KEY`, `ZOOMEYE_API_KEY`, `QUAKE_API_KEY`, and `SHODAN_API_KEY`.
2. Open **Asset Management → Reconnaissance**.
3. Select the data source: FOFA, ZoomEye, Quake, or Shodan.
4. Enter or generate a query for that source and confirm its scope.
5. Run the query, select results whose ownership has been verified, and choose **Save Selected**.
6. Review the created, updated, and skipped counts.

Internet search results are not automatically your assets. Narrow the query with organization domains, certificates, network ranges, or product fingerprints, then verify authorization before saving results.

## Normalization and deduplication

Different sources may describe the same target in different forms. The system:

- trims surrounding whitespace;
- normalizes IP addresses, domains, and protocols to lowercase;
- extracts hostname, protocol, and port from URL-like hosts;
- fills default HTTP/HTTPS ports when omitted;
- converts internationalized domains to ASCII/Punycode;
- removes empty or duplicate tags;
- supplies default source and status values.

Assets use “IP + domain + port + protocol” as the service-level deduplication key; the host is only used when both IP and domain are missing. As a result, assets under the same domain with different IP addresses (for example, reconnaissance-platform exports where the domain column holds the registrable root domain and the host column holds subdomains) remain separate assets, and `80/http` and `443/https` on the same host also remain separate assets.

When an existing asset is imported again, non-empty incoming fields and the last-seen time are updated. Existing fields omitted by the new record and the original first-seen time are preserved.

## Search, filters, and views

Keyword search in the Web UI covers hosts, IP addresses, domains, titles, services, tags, responsible people, departments, and business systems. Status and project are the primary filters; advanced filters can combine:

- risk level and minimum vulnerability count;
- protocol, port, source, and exact tag;
- scanned, never scanned, or not scanned for 30/60/90 days;
- country/region, state/province, city, responsible person, department, and business system;
- environment, criticality, first-seen dates, and last-seen dates;
- sorting by last seen, latest scan, risk, vulnerability count, first seen, target name, or port.

Sorting by latest scan time in ascending order places never-scanned assets first, making coverage gaps visible.

Frequently used combinations can be saved as filter views. Saved views use the current browser's `localStorage`; they are not synchronized to the server, other browsers, or other users.

The HTTP API and `query_assets` additionally support `max_vulnerabilities`, latest-scan time ranges, and allowlisted creation/update sort fields. HTTP lists allow up to 100 rows per page, while Agent queries allow up to 50.

## Bulk maintenance and export

After selecting assets, you can act on the current page or select every result matching the current filters. Cross-page selection resolves the filters again on the server and is limited to 10,000 assets; narrow the filters when the result exceeds that limit.

Available actions:

- **Bind project**: replace the project binding for all selected assets;
- **Bulk edit**: change status, responsible person, department, business system, environment, and criticality, and add or remove tags;
- **Create scan task / Send to chat**: apply one prompt template to the selected assets;
- **Export CSV / XLSX**: export the currently selected rows in the browser, including ownership, risk, vulnerability count, and timestamp fields;
- **Merge duplicates**: keep the first selected asset as primary, fill its empty fields from the other records, and union their tags;
- **Batch delete**: permanently delete the selected assets.

Bulk edit, project binding, and batch delete are all-or-nothing transactions. If any requested asset is missing or outside the caller's scope, the entire operation fails without a partial update.

Merge is only allowed when every duplicate shares a domain, IP address, or Host with the primary asset, and accepts 2-100 selected records. Existing primary values win, tags are unioned subject to the 30-tag limit, and the other records are deleted. It requires both `asset:write` and `asset:delete`; confirm the primary record and the scan history you need to retain before merging.

## Scanning and risk updates

### Scan one asset

Select **Scan** from the asset inventory. The system:

1. creates a conversation containing the target and asset ID;
2. links the conversation to the asset;
3. prompts the Agent to inspect exposed services and authorized risks;
4. stores confirmed findings with `record_vulnerability`;
5. updates scan state with `complete_asset_scan`.

Scan prompts support `{{asset_id}}`, `{{target}}`, `{{host}}`, `{{ip}}`, `{{domain}}`, and `{{port}}`. Adjust scope, ports, test intensity, and validation methods to match the authorization before starting.

### Batch scans

Select multiple assets and choose **Create Scan Task**. The system creates one subtask per asset and links each asset to its queue and subtask.

The current defaults use manual scheduling, one concurrent task, and Eino single-Agent mode to limit load on targets and the local host. You must still confirm the test window, request rate, permitted validation methods, and approval requirements.

### Risk calculation

Asset risk is calculated dynamically from open vulnerabilities in the latest linked scan:

- `critical`, `high`, `medium`, `low`, or `info`: an open finding at that level exists;
- `normal`: the asset was scanned and has no open risk;
- `unassessed`: no scan has completed.

Resolved, false-positive, and ignored findings remain in historical counts but no longer increase the current risk level.

## Agent tools

Six built-in tools expose asset operations to Agents:

- `create_asset`: create or deduplicate and update an asset;
- `get_asset`: retrieve full details by ID;
- `query_assets`: filter, sort, and paginate assets;
- `update_asset`: partially update an asset;
- `delete_asset`: delete an asset;
- `complete_asset_scan`: record scan completion.

`query_assets` returns 20 summaries by default and allows at most 50 per page. Use `get_asset` for full details so large inventories do not consume the model context.

Both `create_asset` and `update_asset` accept responsibility and business metadata, and `query_assets` can filter by those fields. Agent writes go through the same normalization, validation, deduplication, and authorization checks as the HTTP API.

## Access control

Asset permissions are separated into:

- `asset:read`: view assets and statistics;
- `asset:write`: create, import, edit, and update scan state;
- `asset:delete`: delete assets.

Server-side authorization considers the asset owner, explicit resource assignments, the linked project, and permission scope (`all`, `assigned`, or `own`). When a conversation is linked to a project, Agent asset queries are restricted to that project and tool arguments cannot widen the boundary.

Asset batch endpoint limits:

- `POST /api/assets/import`: up to 100,000 assets per request;
- `GET /api/assets/selection`: resolve up to 10,000 matching assets;
- `POST /api/assets/scan-links`: up to 10,000 links per request;
- `PUT /api/assets/bulk`: up to 10,000 asset IDs per request;
- `PUT /api/assets/project-binding`: up to 10,000 asset IDs per request;
- `POST /api/assets/batch-delete`: up to 10,000 asset IDs per request;
- `POST /api/assets/merge`: merge 2-100 asset IDs per request.

## Recommended workflow

1. Define an explicitly authorized set of domains, IP addresses, or network ranges.
2. Add a few critical targets manually and verify normalization and deduplication.
3. Use tags to separate production, testing, critical-business, and internet-facing scopes.
4. Configure one or more network-space search engines, begin with narrow queries, and verify ownership.
5. Test scanning and vulnerability callbacks on one low-risk target.
6. Use never-scanned and over-30-day filters to identify coverage gaps.
7. After validating the workflow, expand gradually with small batch tasks.

A small, verified baseline is usually more valuable than a large inventory with unclear ownership and inconsistent sources.
