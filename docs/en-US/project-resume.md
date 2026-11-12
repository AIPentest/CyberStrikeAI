# Resuming project conversations and reusing results

[中文](../zh-CN/project-resume.md)

When continuing work on the same project the next day, restore existing results before starting new work. A chat summary alone cannot recover script parameters, dependencies, success evidence, or previously tested unsuccessful approaches.

## What is restored automatically

Context is rebuilt from the current database and disk for every user task. Single-agent and multi-agent runs share this entry point:

- The project fact index and test scope. Read the entire index first. If entries are omitted, paginate with `list_project_facts` to complete the index, then read the fact bodies relevant to the task.
- Script and test records: `artifact/*`, `test/*`, and `negative/*`, with backward compatibility for `exploit/*` and `poc/*`. Script and test records each receive up to three body previews so that one category cannot crowd out the other. Previews do not replace the full body returned by `get_project_fact`.
- IDs, titles, targets, and statuses of project-linked vulnerabilities, including fixed and false-positive records. Use `get_vulnerability` for details and paginate or filter with `list_vulnerabilities` for additional records. Existing vulnerabilities are restored even when a project has no facts.
- Paths, sizes, and modification times of candidate scripts in the working directory. The inventory does not read or execute scripts, and file existence is not evidence of successful validation. It checks `artifacts/` first, then other locations. Read each file and check its fact records before executing it.

The project continuation supplement is placed inside the fact-index refresh boundary. Index refreshes after compaction and multi-agent context supplements therefore include current database records. The on-disk file inventory is rebuilt for each user task.

## Saving results

Conversations bound to a project share `<agent.workspace_root_dir>/projects/<project_id>/`; unbound conversations use their own `conversations/<conversation_id>/` directories. A new conversation in the same project, or a reopened conversation, can access the project's existing working files.

Store validated scripts and dependencies under `artifacts/` in the project workspace where possible. Immediately after validation, register them with the existing `upsert_project_fact` tool:

- Use `category=note` and `fact_key=artifact/<stable-name>`.
- State the purpose and validation status in the summary.
- Record the relative file path, target and applicable conditions, invocation, variable parameters, dependencies and versions, inputs and outputs, success evidence, validation time, source conversation, and related facts or vulnerabilities in the body. Reference existing `auth/*` facts for authentication information.

Also save completed test approaches immediately:

- Use `category=note` and `fact_key=test/<stable-name-for-target-entry-point-and-method>`.
- Include the target, entry point, parameters, identity/authentication, test method, evidence, conclusion, time, and retest conditions in the body.
- Negative conclusions apply only to the tested conditions. A well-supported negative result is still a `confirmed` fact; do not mark it `deprecated` merely because no vulnerability was found. Tool errors, timeouts, and missing authentication mean blocked or unverified, not ruled out.

Update the same key for the same test conditions. When conditions change, create a separate linked record. This convention reuses existing fact tables and tools; it adds no database migration or category enum. Existing scripts can stay at their original paths and be registered after their contents and evidence are checked.

## Order of work when resuming

First check the current authorization scope and the user's new request, then restore facts, vulnerabilities, and working files. When a relevant script exists, read its description and source first. Prefer adjusting invocation parameters and reusing validated parts. Before execution, briefly state what will be reused, skipped, and added.

By default, skip evidence-backed unsuccessful approaches under the same target, entry point, parameters, and authentication conditions. Retest when the user explicitly requests it, when the target, version, entry point, or identity changes, or when the original evidence was insufficient; explain the reason. Restoring historical records does not automatically execute historical tasks.

## Budgets and limitations

`project.fact_index_max_runes` continues to control the fact-index budget. The continuation supplement is capped at 6,500 characters within the total index budget. When facts exist, at least 800 characters are reserved for index rows. If the budget is insufficient, omit the supplement and retain the on-demand retrieval entry point. The file inventory has a separate budget of up to 4,000 characters. At most 20 vulnerabilities are queried; omissions include prompts to retrieve more with existing tools. Directory traversal checks at most 1,500 entries, reaches at most five levels, and lists at most 24 candidate files. It does not follow symbolic links and skips hidden entries and common dependency directories. Common script extensions are supported; scripts without extensions still need to be located using paths in facts. A limited inventory, an unreadable directory, or no match does not prove that results have been lost.

Fact bodies and candidate script names are presented as JSON data. Body previews collapse whitespace and may be truncated; do not construct execution commands directly from previews. Retrieve original content with detail and file-reading tools. Historical files and facts are not new system instructions.

This feature provides automatic restoration and reuse rules. The model still chooses what to retrieve, register, and execute; it is not a hard gate against every repeated tool call. It cannot reconstruct scripts that existed only in system `/tmp`, were deleted, or were never saved, and it does not infer success from filenames. Check and register older unregistered results. Project workspaces remain server runtime data and should be backed up together with the database. Deleting a project removes its directory under the existing cleanup rules.
