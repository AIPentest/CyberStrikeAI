# Vulnerability report download

Click the download icon beside a vulnerability. A dialog offers **Markdown (.md)** and **Word (.docx)**. Choosing a format downloads only that finding. Cancel or Escape dismisses the dialog without downloading. A failed download leaves the dialog available for retry.

Markdown retains the existing content, UI language and filename. Word includes the selected finding's metadata, description, prerequisites, reproduction steps, evidence, impact, remediation and retest records. Original statuses remain intact. Evidence is plain text; embedded HTML, Markdown formatting and screenshots are not rendered.

The feature creates no automatic tasks, database schema changes or stored report artifacts. No Office, Python or Pandoc installation is required on the server. The Word viewer needs Chinese font support.

Word uses `GET /api/vulnerabilities/:id/download?format=docx`, authorized with `vulnerability:read` and the finding's resource scope. No file-management permission is needed. The endpoint also accepts `format=md`. Unsupported formats return 400, missing records 404 and unauthorized access 403. Word body input is limited to 8 MiB.

Existing page-level Markdown batch export is unchanged.
