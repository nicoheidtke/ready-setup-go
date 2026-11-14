---
name: openspec
description: Use the repository's shared OpenSpec workflows when the user asks to explore, propose, implement, revise, sync, or archive an OpenSpec change.
---

Read the selected workflow from the repository's `.agents/skills/` directory and follow its instructions. Resolve all paths below from the repository root, not this skill's directory.

| Request | Shared workflow file |
| --- | --- |
| `explore` | `.agents/skills/openspec-explore/SKILL.md` |
| `propose` | `.agents/skills/openspec-propose/SKILL.md` |
| `apply` | `.agents/skills/openspec-apply-change/SKILL.md` |
| `update-change` | `.agents/skills/openspec-update-change/SKILL.md` |
| `sync` | `.agents/skills/openspec-sync-specs/SKILL.md` |
| `archive` | `.agents/skills/openspec-archive-change/SKILL.md` |

For example, `/openspec propose add-cache-policy` reads `openspec-propose/SKILL.md` and uses `add-cache-policy` as its input. Pass the remaining user request to the selected workflow. Read only that workflow in full before acting, including its planning and authorization boundaries. If it refers to another shared workflow, read that file when needed.

Infer the workflow from a clear natural-language request. If the intended operation is unclear, ask which operation the user wants. `update-change` revises planning artifacts; the terminal command `openspec update` refreshes generated skills and is not that workflow. If a shared file is missing, report it instead of inventing replacement instructions.
