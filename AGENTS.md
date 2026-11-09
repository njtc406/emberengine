---
name: ember-skills
description: "Use when looking for an EmberEngine skill for a specific development or maintenance task."
---

| Skill | 用途 | 路径 |
|---|---|---|
| ember-skill-builder | 根据详细需求创建、更新或重组 Ember skill，并同步维护此主索引。 | [skills/ember-skill-builder/SKILL.md](./skills/ember-skill-builder/SKILL.md) |


## 框架变更后的 Skill 与文档同步（强制）

- 修改框架层代码时，必须检查本次变更是否会使相关 skill 或文档中的 API、配置项、默认值、行为约束、生命周期、使用步骤或示例滞后；不能仅以编译或测试通过作为完成标准。
- 若存在影响，必须在代码修改完成后、任务交付前，主动更新对应 skill 及其引用资料，以及受影响的说明文档、配置参考和示例，无需用户再次要求。以实际代码和验证结果为准，删除或修正过时说明，不得只追加新说明而保留冲突内容。
- 更新 EmberEngine skill 时，必须使用 `ember-skill-builder`；若新增、重命名、移动或删除 skill，必须同步维护本文件中的 skill 索引及相关引用。
- 同步范围限于本次变更影响的内容，不得顺带改写无关文档，也不得将历史设计记录误写成当前实现。若无相关 skill 或文档需要更新，必须在交付说明中简述判断依据。
- 交付前必须核对代码与 skill、文档的一致性，检查受影响的路径、符号、链接、配置和示例，并执行适用的现有验证。交付说明必须列出已同步的 skill 和文档；存在未同步项或无法验证的内容时，必须明确说明原因，不得宣称全部完成。

<!-- gitnexus:start -->
# GitNexus — Code Intelligence

This project is indexed by GitNexus as **emberengine** (10873 symbols, 29980 relationships, 300 execution flows). Use the GitNexus MCP tools to understand code, assess impact, and navigate safely.

> Index stale? Run `node .gitnexus/run.cjs analyze` from the project root — it auto-selects an available runner. No `.gitnexus/run.cjs` yet? `npx gitnexus analyze` (npm 11 crash → `npm i -g gitnexus`; #1939).

## Always Do

- **MUST run impact analysis before editing any symbol.** Before modifying a function, class, or method, run `impact({target: "symbolName", direction: "upstream"})` and report the blast radius (direct callers, affected processes, risk level) to the user.
- **MUST run `detect_changes()` before committing** to verify your changes only affect expected symbols and execution flows. For regression review, compare against the default branch: `detect_changes({scope: "compare", base_ref: "main"})`.
- **MUST warn the user** if impact analysis returns HIGH or CRITICAL risk before proceeding with edits.
- When exploring unfamiliar code, use `query({query: "concept"})` to find execution flows instead of grepping. It returns process-grouped results ranked by relevance.
- When you need full context on a specific symbol — callers, callees, which execution flows it participates in — use `context({name: "symbolName"})`.

## Never Do

- NEVER edit a function, class, or method without first running `impact` on it.
- NEVER ignore HIGH or CRITICAL risk warnings from impact analysis.
- NEVER rename symbols with find-and-replace — use `rename` which understands the call graph.
- NEVER commit changes without running `detect_changes()` to check affected scope.

## Resources

| Resource | Use for |
|----------|---------|
| `gitnexus://repo/emberengine/context` | Codebase overview, check index freshness |
| `gitnexus://repo/emberengine/clusters` | All functional areas |
| `gitnexus://repo/emberengine/processes` | All execution flows |
| `gitnexus://repo/emberengine/process/{name}` | Step-by-step execution trace |

## CLI

| Task | Read this skill file |
|------|---------------------|
| Understand architecture / "How does X work?" | `.claude/skills/gitnexus/gitnexus-exploring/SKILL.md` |
| Blast radius / "What breaks if I change X?" | `.claude/skills/gitnexus/gitnexus-impact-analysis/SKILL.md` |
| Trace bugs / "Why is X failing?" | `.claude/skills/gitnexus/gitnexus-debugging/SKILL.md` |
| Rename / extract / split / refactor | `.claude/skills/gitnexus/gitnexus-refactoring/SKILL.md` |
| Tools, resources, schema reference | `.claude/skills/gitnexus/gitnexus-guide/SKILL.md` |
| Index, status, clean, wiki CLI commands | `.claude/skills/gitnexus/gitnexus-cli/SKILL.md` |

<!-- gitnexus:end -->
