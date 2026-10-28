# Citations and external links (example)

This file verifies that a skill package's **`references/`** directory is properly recognized by the list API, HTTP `resource_path`, and the multi-agent native file tools.

## How to test (inside an authorized environment)

1. The `package_files` in the `GET /api/skills/cyberstrike-eino-demo` response should contain `references/citations.md`.
2. `GET /api/skills/cyberstrike-eino-demo?resource_path=references/citations.md` should return this file's content.
3. When multi-agent is on and `eino_skills.filesystem_tools` is enabled, this file can be read by relative path.

## Placeholder citations

- [OWASP Testing Guide](https://owasp.org/www-project-web-security-testing-guide/) (link-format example only)
