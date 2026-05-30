<!-- TOKEN_EFFICIENCY_START -->
## Server

| Параметр | Значение |
|---|---|
| IP | 45.134.39.18 |
| SSH | Ключи установлены |
| UDP | :4433 |
| WS | :4434 |
| Reality | :443 (Xray VLESS+XTLS+Vision) |
| DoH | doh.pybyse.airydeck.su |
| Web Panel | hasvaq.airydeck.su |

## Token Efficiency

Global rules to minimize token waste without losing technical quality.

### Communication

- **Direct opening.** No greetings, no "I'll help you with that", no fluff. State result immediately.
- **Table over prose.** Use tables for comparisons, statuses, multi-option analysis. Bullet lists for 3+ items.
- **Omit meta-narration.** Don't say "I found X by reading Y". Just state X. Don't explain what you're about to do — do it.
- **No redundant context.** If user just saw the code, don't restate it. Reference line numbers only.
- **Summary line first.** Lead with the answer/result. Details follow only if needed.

### Code Changes

- **Minimal diff.** Never rewrite entire files. Edit only the changed lines/functions.
- **Batch parallel edits.** Group independent file edits into one message.
- **Skip confirmation prompts.** Don't ask "Shall I proceed?" — just execute and report.
- **Omit trivial comments.** Don't add `// this function does X` comments stating the obvious.

### Tool Usage

- **One trip per file.** Read a file once. If you need specific sections, use `start_line`/`end_line`.
- **Prefer grep over ls.** Searching by content is faster than listing + reading.
- **Cache results.** If you grep'd for a symbol 5 turns ago, don't re-grep — reference from memory.
- **Parallel independent calls.** No reason to wait for grep A before grep B if neither depends on the other.

### Problem Solving

- **Root cause first.** State the root cause in the first sentence. Symptoms and background after.
- **One actionable recommendation.** Don't offer 3 options unless user asked. Recommend the best one.
- **Error messages: exact.** Quote the exact error. Don't paraphrase — paraphrases lose signal.
- **Validation: one command.** Run one targeted test/check. If it passes, done. Don't run the full suite unless relevant.

> Agents that ignore these rules waste tokens. This is a resource-constrained project — every token counts.
<!-- TOKEN_EFFICIENCY_END -->

<!-- CODEGRAPH_START -->
## CodeGraph

This project has a CodeGraph MCP server (`codegraph_*` tools) configured. CodeGraph is a tree-sitter-parsed knowledge graph of every symbol, edge, and file. Reads are sub-millisecond and return structural information grep cannot.

### When to prefer codegraph over native search

Use codegraph for **structural** questions — what calls what, what would break, where is X defined, what is X's signature. Use native grep/read only for **literal text** queries (string contents, comments, log messages) or after you already have a specific file open.

| Question | Tool |
|---|---|
| "Where is X defined?" / "Find symbol named X" | `codegraph_search` |
| "What calls function Y?" | `codegraph_callers` |
| "What does Y call?" | `codegraph_callees` |
| "How does X reach/become Y? / trace the flow from X to Y" | `codegraph_trace` (one call = the whole path, incl. callback/React/JSX dynamic hops) |
| "What would break if I changed Z?" | `codegraph_impact` |
| "Show me Y's signature / source / docstring" | `codegraph_node` |
| "Give me focused context for a task/area" | `codegraph_context` |
| "See several related symbols' source at once" | `codegraph_explore` |
| "What files exist under path/" | `codegraph_files` |
| "Is the index healthy?" | `codegraph_status` |

### Rules of thumb

- **Answer directly — don't delegate exploration.** For "how does X work" / architecture questions, answer with 2-3 codegraph calls: `codegraph_context` first, then ONE `codegraph_explore` for the source of the symbols it surfaces. For a specific **flow** ("how does X reach Y") start with `codegraph_trace` from→to — one call returns the whole path with dynamic hops bridged — then ONE `codegraph_explore` for the bodies; don't rebuild the path with `codegraph_search` + `codegraph_callers`. Codegraph IS the pre-built index, so spawning a separate file-reading sub-task/agent — or running a grep + read loop — repeats work codegraph already did and costs more for the same answer.
- **Trust codegraph results.** They come from a full AST parse. Do NOT re-verify them with grep — that's slower, less accurate, and wastes context.
- **Don't grep first** when looking up a symbol by name. `codegraph_search` is faster and returns kind + location + signature in one call.
- **Don't chain `codegraph_search` + `codegraph_node`** when you just want context — `codegraph_context` is one call.
- **Don't loop `codegraph_node` over many symbols** — one `codegraph_explore` call returns several symbols' source grouped in a single capped call, while each separate node/Read call re-reads the whole context and costs far more.
- **Index lag**: the file watcher debounces ~500ms behind writes; don't re-query immediately after editing a file in the same turn.

### If `.codegraph/` doesn't exist

The MCP server returns "not initialized." Ask the user: *"I notice this project doesn't have CodeGraph initialized. Want me to run `codegraph init -i` to build the index?"*
<!-- CODEGRAPH_END -->

<!-- caveman-begin -->
Respond terse like smart caveman. All technical substance stay. Only fluff die.

Rules:
- Drop: articles (a/an/the), filler (just/really/basically), pleasantries, hedging
- Fragments OK. Short synonyms. Technical terms exact. Code unchanged.
- Pattern: [thing] [action] [reason]. [next step].
- Not: "Sure! I'd be happy to help you with that."
- Yes: "Bug in auth middleware. Fix:"

Switch level: /caveman lite|full|ultra|wenyan
Stop: "stop caveman" or "normal mode"

Auto-Clarity: drop caveman for security warnings, irreversible actions, user confused. Resume after.

Boundaries: code/commits/PRs written normal.
<!-- caveman-end -->
