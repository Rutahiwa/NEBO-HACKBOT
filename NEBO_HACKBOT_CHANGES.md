# NEBO-HACKBOT Changes Log

Tracks every change from the PentAGI v2.1.0 base, with files affected, rationale, and runtime-testing notes.

---

## Rename: PentAGI → NEBO-HACKBOT (user-facing branding)

**Decision:** Keep Go module path (`github.com/vxcontrol/pentagi`) and internal import paths unchanged to avoid a sprawling rename that risks breaking the build. Only user-facing display names, container names, env var prefixes, and branding strings are changed. Auth salt prefixes in `session.go` and `auth_middleware.go` are kept as-is to preserve session/token compatibility.

### Commit 1: Backend Go display strings (14 files)
- `backend/cmd/pentagi/main.go` — startup log
- `backend/pkg/version/version.go` — binary name default
- `backend/pkg/server/router.go` — Swagger title, description, logger name
- `backend/pkg/server/docs/{docs.go,swagger.json,swagger.yaml}` — API docs
- `backend/pkg/providers/performer.go` — Graphiti source descriptions
- `backend/pkg/providers/helpers.go` — error message
- `backend/pkg/tools/searchers/searxng.go` — User-Agent header
- `backend/pkg/tools/terminal.go` — container name prefix
- `backend/pkg/docker/client.go` — warning message
- `backend/pkg/config/config.go` — default DB connection string
- `backend/pkg/config/tenant.go` — Docker label key
- `backend/pkg/server/services/graphql.go` — logger names

**Runtime test:** Start the stack, verify UI title shows "NEBO-HACKBOT", containers are named `nebo-hackbot-terminal-*`, Swagger UI shows correct title.

### Commit 2: Frontend UI strings (16 files)
- Page title, web manifest, sidebar, login heading, flow placeholders
- API tokens page, resources page, route titles, storage keys
- All test assertions updated

**Runtime test:** Load the web UI; verify branding on login page, sidebar, flow creation, and settings pages.

### Commit 3: Compose, Dockerfile, scripts, env (11 files)
- docker-compose*.yml — service/container/volume/network names, env var prefixes, image defaults
- Dockerfile — binary name, system user/group, /opt paths, labels
- scripts — SSL cert org/CN, build version strings
- .env.example — all PENTAGI_ prefixes renamed

**Runtime test:** `docker compose up -d` works with new service names; containers named correctly.

### Commit 4: Installer, locale, docs (12 files)
- Installer Go files — banner, checker paths/volumes, processor constants, locale (~100+ strings)
- README.md — all prose, env vars, container names (~243 references)
- CLAUDE.md, CONTRIBUTING.md — project description, paths

**Runtime test:** Run the installer wizard; verify all screens show NEBO-HACKBOT branding.

### NOT changed (by design)
- Go module path and all import paths
- Auth salt prefixes (pentagi.cookie.auth, pentagi.jwt.signing, pentagi.automation)
- DB advisory lock names and GORM logger names
- GitHub URLs (github.com/vxcontrol/pentagi)
- pentagi.com and update.pentagi.com domain URLs
- Go identifier names (PentagiRunning, ProductStackPentagi, etc.)

---

## Phase A: Context Efficiency

**Goal:** Reduce token usage per agent call by truncating large tool outputs, abbreviating stale execution context, and pruning verbose chain history.

### Tool Output Truncation

Tool results (terminal output, browser content, search results) that exceed a size threshold are truncated to an 8 KB head + 2 KB tail with a `[... truncated N bytes ...]` marker. This prevents a single verbose `nmap` or `nikto` scan from consuming the entire context window.

**Files changed:**
- `backend/pkg/providers/performer.go` — applies truncation to tool call responses before appending to the chain
- `backend/pkg/providers/helpers.go` — truncation utility functions

### Execution Context Abbreviation

When building the execution context template for later subtasks, older subtask results are abbreviated to short summaries rather than full verbatim output. The most recent subtask retains its full output for the agent's immediate use.

**Files changed:**
- `backend/pkg/templates/prompts/short_execution_context.tmpl` — abbreviated format for older subtask results
- `backend/pkg/templates/prompts/full_execution_context.tmpl` — full format for the current subtask

**Runtime test:** Run a multi-subtask Juice Shop flow. Verify token count per agent call is lower than baseline. Verify large tool outputs show the truncation marker in the UI.

---

## Phase B: CLI-Operator Behaviour

**Goal:** Ensure agents behave as CLI operators, using terminal commands and tool calls instead of narrating GUI tool usage or writing essay-length explanations.

### System Prompt Tuning

Agent system prompts were tuned to enforce CLI-operator behaviour:
- Agents are instructed to use command-line tools (`nmap`, `sqlmap`, `ffuf`, `curl`, `httpx`, `nikto`, etc.) instead of GUI tools (Burp Suite, ZAP, etc.)
- Agents are instructed to be concise in their message fields (under 200 characters)
- Agents are instructed to prefer tool calls over plain-text narration

**Files changed:**
- `backend/pkg/templates/prompts/pentester.tmpl` — CLI-operator instructions added to the pentester system prompt
- `backend/pkg/templates/prompts/primary_agent.tmpl` — CLI-operator instructions added to the primary agent system prompt
- `backend/pkg/templates/prompts/coder.tmpl` — conciseness instructions for the coder agent
- `backend/pkg/templates/prompts/searcher.tmpl` — conciseness instructions for the searcher agent

**Runtime test:** Run a Juice Shop flow. Verify no Burp Suite / ZAP / GUI tool references in agent logs. Verify message fields are under 200 chars. Verify most agent turns include tool calls.

---

## Phase C: Context Architecture

**Goal:** Implement a sliding-window context system that keeps agent calls fast by limiting the number of conversation messages sent to the LLM, while preserving the full chain in the database.

### Sliding-Window Context

A new `applyContextWindow` function slices the message chain to keep only the pinned prefix (system prompt + first human message) and the last N messages from the conversation history. The full chain is still persisted to the database; the window only affects what the LLM sees.

**Files created:**
- `backend/pkg/providers/context_window.go` — `applyContextWindow(chain, windowSize)` function

**Files changed:**
- `backend/pkg/config/config.go` — new config fields: `UseContextWindow` (bool, default `true`), `ContextWindowSize` (int, default `10`)
- `backend/pkg/providers/performer.go` — calls `applyContextWindow` before sending the chain to the LLM when `UseContextWindow` is enabled

### Chain Pruning

Stale tool results (older than the 5 most recent tool-call/response pairs) are replaced with short stubs to prevent unbounded chain growth between summarization passes. This is a pure string operation with no LLM call.

**Files created:**
- `backend/pkg/providers/chain_pruning.go` — `pruneStaleToolResults(chain, keepRecent)` function; replaces old tool response content with a 120-char stub + `[... pruned N bytes ...]` pointer to memory search

### Environment Variables

| Variable | Default | Description |
|---|---|---|
| `USE_CONTEXT_WINDOW` | `true` | Enable/disable sliding-window context |
| `CONTEXT_WINDOW_SIZE` | `10` | Number of trailing messages in the window |

**Runtime test:** Run a long flow (10+ subtasks) with context windowing on and off. Measure agent call times. Verify the fallback path (`USE_CONTEXT_WINDOW=false`) still works. Test `CONTEXT_WINDOW_SIZE=5` for tighter windows.

---

## Phase D: Specialist Agents

**Goal:** Break the monolithic pentester agent into focused specialists that the Primary Agent delegates to based on vulnerability class.

### Specialist Agent Types

Six specialist agents were added, each with its own system prompt, question prompt, tool definition, and handler:

| Specialist | Tool Name | System Prompt | Focus Area |
|---|---|---|---|
| Recon | `recon` | `recon.tmpl` | Reconnaissance: nmap, httpx, ffuf, whatweb, directory enumeration |
| Injection | `injection` | `injection.tmpl` | Injection attacks: SQLi (sqlmap), command injection, template injection |
| XSS | `xss_test` | `xss.tmpl` | Cross-site scripting: reflected, stored, DOM XSS |
| Auth | `auth_test` | `auth.tmpl` | Authentication/session: default creds, session fixation, JWT, brute force |
| IDOR | `idor_test` | `idor.tmpl` | Access control: horizontal/vertical privesc, direct object references |
| SSRF | `ssrf_test` | `ssrf.tmpl` | SSRF: internal network access, cloud metadata, URL scheme abuse |

### Files created (prompt templates)

- `backend/pkg/templates/prompts/recon.tmpl` — recon specialist system prompt
- `backend/pkg/templates/prompts/question_recon.tmpl` — recon specialist question prompt
- `backend/pkg/templates/prompts/injection.tmpl` — injection specialist system prompt
- `backend/pkg/templates/prompts/question_injection.tmpl` — injection specialist question prompt
- `backend/pkg/templates/prompts/xss.tmpl` — XSS specialist system prompt
- `backend/pkg/templates/prompts/question_xss.tmpl` — XSS specialist question prompt
- `backend/pkg/templates/prompts/auth.tmpl` — auth specialist system prompt
- `backend/pkg/templates/prompts/question_auth.tmpl` — auth specialist question prompt
- `backend/pkg/templates/prompts/idor.tmpl` — IDOR specialist system prompt
- `backend/pkg/templates/prompts/question_idor.tmpl` — IDOR specialist question prompt
- `backend/pkg/templates/prompts/ssrf.tmpl` — SSRF specialist system prompt
- `backend/pkg/templates/prompts/question_ssrf.tmpl` — SSRF specialist question prompt

### Files changed (backend wiring)

- `backend/pkg/templates/templates.go` — new `PromptType` constants for all specialist + question prompts; new `AllowedVars` entries; prompt registration in `FlowPrompts`
- `backend/pkg/tools/registry.go` — tool name constants (`ReconToolName`, `InjectionToolName`, `XSSToolName`, `AuthToolName`, `IDORToolName`, `SSRFToolName`); tool type mappings; tool definitions with `SpecialistAction`/`SpecialistResult` parameter schemas; tools added to primary agent's available tool list
- `backend/pkg/tools/args.go` — `SpecialistAction` and `SpecialistResult` structs (shared across all specialist delegation tools)
- `backend/pkg/tools/tools.go` — `SpecialistExecutorConfig` struct for specialist executor setup
- `backend/pkg/providers/handlers.go` — `specialistMeta` struct and `specialistRegistry` map wiring each tool name to its prompt types, option type, and message chain type; `GetSpecialistHandler` factory method
- `backend/pkg/providers/performers.go` — `performSpecialist` method that runs the specialist agent loop (similar to `performPentester` but using the specialist's own prompt and tool set)
- `backend/pkg/providers/pconfig/config.go` — new `OptionsType` constants for each specialist (e.g., `OptionsTypeRecon`, `OptionsTypeInjection`)
- `backend/pkg/database/models.go` — new `MsgchainType` constants for each specialist's message chain storage

### Subtask Patch Operations

The subtask management system was refactored to support delta operations (add, remove, modify, reorder) instead of full subtask list replacement. This allows the Primary Agent to incrementally adjust the plan as specialists report findings.

**Files created:**
- `backend/pkg/providers/subtask_patch.go` — `applySubtaskOperations` function with full CRUD operations on the subtask list
- `backend/pkg/providers/subtask_patch_test.go` — comprehensive tests for all subtask patch operations

**Runtime test:** Run a Juice Shop flow. Verify the Primary Agent delegates to specialists. Verify each specialist uses tools appropriate to its focus area. Verify specialist results appear in the final report.

---

## Phase E: Validation Agent

**Goal:** Add an adversarial validation agent that independently reproduces candidate findings before they are included in the final report.

### Validator Agent

The validator agent receives candidate findings from specialist agents and attempts to independently reproduce them. Findings that cannot be reproduced are marked as REJECTED and excluded from the final report. Confirmed findings receive an independent severity assessment.

| Tool Name | Description |
|---|---|
| `validator` | Delegate to the findings validator for adversarial reproduction and severity assessment |
| `validator_result` | Store the validation result (confirmed/rejected with evidence) |

### Files changed

- `backend/pkg/tools/registry.go` — `ValidatorToolName` and `ValidatorResultToolName` constants; tool definitions with `SpecialistAction`/`SpecialistResult` parameter schemas; validator added to primary agent's available tool list
- `backend/pkg/providers/handlers.go` — validator entry in `specialistRegistry` map, wiring it to its prompt type, option type, and message chain type
- `backend/pkg/providers/performers.go` — validator execution via the shared `performSpecialist` path
- `backend/pkg/templates/templates.go` — prompt type constants and registration for the validator agent

**Runtime test:** Manually trigger a finding the validator should reject (e.g., self-XSS). Verify the validator reproduces confirmed findings independently. Verify rejected findings are excluded from the final report.

---

## Phase F: Deployment Documentation

**Goal:** Create deployment and testing documentation for the GPU VM.

### Files created

- `DEPLOY_NEBO_HACKBOT.md` — step-by-step deployment guide for the GPU VM (2x A100 40GB, Debian 13), covering vLLM setup, NEBO-HACKBOT stack configuration, new environment variables, rollback procedures, and future scope enforcement hooks
- `RUNTIME_TEST_CHECKLIST.md` — specific test flows for each phase (A-E) plus baseline comparison methodology
- `NEBO_HACKBOT_CHANGES.md` — this file, updated with Phase A-F entries

### No code changes

Phase F is documentation-only. No backend, frontend, or configuration code was modified.

---

## Context Isolation Verification Pass (pre-deploy)

Systematic verification of 5 context-isolation principles before VM deployment.

### 1. Specialist context isolation — ALREADY CORRECT

**How it works:** `performSpecialist()` in `performers.go:693` calls `restoreChain()` which queries `GetFlowTaskTypeLastMsgChain` for a chain matching the specialist's own `MsgchainType`. On first invocation this returns empty, so `fallback()` creates a fresh `[SystemPrompt, HumanMessage]` chain. The specialist never inherits the orchestrator's chain — it only sees its own system prompt and the delegated question.

**Files:** `backend/pkg/providers/performers.go:763`, `backend/pkg/providers/helpers.go:447-608`

No change needed.

### 2. Compact results back to orchestrator — ALREADY CORRECT

**How it works:** `performSpecialist()` returns only `specialistResult.Result` (a single string extracted from the `SpecialistResult` JSON via the barrier tool handler at line 738). The specialist's full internal transcript stays in the DB chain; the orchestrator only sees the compact result string.

**Files:** `backend/pkg/providers/performers.go:787`, `backend/pkg/tools/args.go` (SpecialistResult struct)

No change needed.

### 3. External memory for findings — ALREADY CORRECT

**How it works:** All specialist delegation tool names are in `allowedStoringInMemoryTools` (`registry.go:197-203`), and the `storeToolResult()` function in `executor.go:519` writes tool results to pgvector when the tool is in that list. Graphiti integration (when enabled) also stores agent responses and tool executions. Findings survive window eviction because they're in durable pgvector storage, retrievable via `search_in_memory`.

**Files:** `backend/pkg/tools/registry.go:181-205`, `backend/pkg/tools/executor.go:519-603`

No change needed.

### 4. Pin critical state — FIXED

**What was wrong:** The context window could cut inside a tool-call exchange, leaving a Tool response message in the window without its preceding AI message (which contains the ToolCall the response answers). The LLM would see an orphaned tool response it never requested, causing confusion or malformed tool call attempts.

**Fix:** Adjusted `applyContextWindow()` to back up the cut point past any leading Tool response messages, so the window always starts at an AI or Human message boundary.

**File:** `backend/pkg/providers/context_window.go`

**Runtime test:** Run a long flow with `USE_CONTEXT_WINDOW=true` and `CONTEXT_WINDOW_SIZE=5` (tight window). Verify no "unknown tool call ID" errors in agent logs.

### 5. Prune output but keep conclusions — FIXED

**What was wrong:** `pruneStaleToolResults()` kept the first 120 raw bytes of content, which often cut mid-word and lost the most informative line. The stub was a generic `[... pruned N bytes ...]` with no tool name or meaningful summary.

**Fix:** Now extracts the first non-empty line (which usually contains the key finding — e.g., "Nmap scan report for 10.0.0.1" or "sqlmap identified the following injection point(s)") and prefixes it with the tool name. Stub size increased from 120 to 200 bytes.

**File:** `backend/pkg/providers/chain_pruning.go`

**Runtime test:** Run a multi-tool flow and inspect pruned stubs in the chain (via Langfuse or DB). Verify they show tool name + meaningful first line, not raw byte truncation.

---

## Final Tooling & Workflow Pass

Fixes for 6 concrete bugs exposed by a baseline Juice Shop run (142 tool calls, ~2h49m, ~3.1M tokens, no report produced).

### Task 1+6: Graceful subtask failure + guaranteed reporter

**Before:** When the LLM returned empty content (stop reason 'stop'), `callWithRetries` retried 3x, then `performCallerReflector` fired, and if that also failed, the error propagated up through `subtaskWorker.Run` → `taskWorker.Run`, killing the entire task. The reporter at `task.go:336` never executed. Duplicate "Address Tool Call Issues" subtasks were spawned.

**After:**
- `performer.go`: Added `ErrSubtaskIncomplete` sentinel. `performReflector` failure wraps this sentinel.
- `subtask.go`: When `PerformAgentChain` returns `ErrSubtaskIncomplete`, marks subtask Failed and returns nil (graceful).
- `task.go`: Recoverable subtask errors (non-context, non-DB) log a warning and continue the loop. Only unrecoverable errors (context.Canceled, sql.ErrConnDone) propagate. Reporter always runs after the loop.

**Files:** `backend/pkg/providers/performer.go`, `backend/pkg/controller/{subtask,task}.go`

**Runtime test:** Force empty LLM response → subtask marked Failed, flow continues, report produced.

### Task 2: Status queries must not trigger new work

**Before:** Asking "is the report ready?" caused the Primary Agent to re-delegate to pentester, restart the target, install tools, re-scan ports.

**After:** Added STATUS QUERY HANDLING directive to `primary_agent.tmpl` and `assistant.tmpl`. Status questions are answered from current knowledge using the done tool. New work only resumes on explicit "continue testing" instructions.

**Files:** `backend/pkg/templates/prompts/{primary_agent,assistant}.tmpl`

**Runtime test:** Ask "is the report ready?" mid-flow → answers without new subtasks.

### Task 3: No confirmation without live target + real evidence

**After:** Added to `validator.tmpl`: TARGET REACHABILITY CHECK (must verify target is live before confirming), EVIDENCE INTEGRITY (only cite actually-executed tools), UNCONFIRMED status for unreachable targets.

**File:** `backend/pkg/templates/prompts/validator.tmpl`

**Runtime test:** Stop target before validation → findings marked UNCONFIRMED.

### Task 4: Validator rejects findings contradicting discovery

**After:** Added DISCOVERY CROSS-CHECK rule to `validator.tmpl`. If discovery found no vulns of a class, findings must be independently reproduced or REJECTED.

**File:** `backend/pkg/templates/prompts/validator.tmpl`

**Runtime test:** Discovery says "No IDOR found" → IDOR finding REJECTED unless reproduced.

### Task 5: Single source of truth for the report

**After:** Added REPORT INTEGRITY RULES to `reporter.tmpl`: single source of truth (from subtask results only), no contradictions (each finding once), evidence-bound severity (no theoretical upgrades).

**File:** `backend/pkg/templates/prompts/reporter.tmpl`

**Runtime test:** Report has no duplicate/contradictory findings, severities match evidence.

---

## Recon-Signal vs Proven-Vulnerability Audit

A baseline run treated nikto scanner output as a CONFIRMED CRITICAL finding. This audit identified which prompts correctly distinguish scanner signals from proven vulnerabilities and which blur the distinction.

### Audit Findings

| Prompt | Status | Issue |
|--------|--------|-------|
| `validator.tmpl` | EXCELLENT | Already has independent reproduction, evidence integrity, anti-blur rules ("a reflection is NOT XSS unless JavaScript executes") |
| `reporter.tmpl` | STRONG | Already has evidence-bound severity and validator veto power |
| `auth.tmpl` | SOUND | Inherently active testing, no scanners involved |
| `idor.tmpl` | SOUND | Purely active with explicit request/response evidence requirements |
| `ssrf.tmpl` | SOUND | Callback-based verification built in |
| `pentester.tmpl` | **FIXED** | Graphiti taxonomy (DETECTED → CONFIRMED → EXPLOITED) was inside `{{if .GraphitiEnabled}}` — disappeared when Graphiti off. Added non-conditional `<evidence_standard>` block. |
| `recon.tmpl` | **FIXED** | Used "findings" ambiguously. Added explicit framing: output is an attack surface map of leads, not confirmed vulnerabilities. |
| `injection.tmpl` | **FIXED** | Methodology implied the right process but didn't state scanner-only detection is insufficient. Added evidence rule requiring demonstrated data extraction. |
| `xss.tmpl` | **FIXED** | Had good "Confirm execution" step but no explicit statement that scanner hits are just leads. Added evidence rule. |
| `primary_agent.tmpl` | **FIXED** | Validator routing was a suggestion in use_cases, not enforced. Added top-level `<validation_mandate>` requiring all findings to pass validator. |
| `reporter.tmpl` | **ENHANCED** | Added Confirmed vs Potential Issues section structure. Scanner-only detections go in Potential Issues, never Confirmed. |

### Files Changed
- `backend/pkg/templates/prompts/pentester.tmpl` — `<evidence_standard>` block (non-conditional)
- `backend/pkg/templates/prompts/recon.tmpl` — explicit "leads not findings" framing
- `backend/pkg/templates/prompts/injection.tmpl` — evidence rule for data extraction
- `backend/pkg/templates/prompts/xss.tmpl` — evidence rule for execution verification
- `backend/pkg/templates/prompts/primary_agent.tmpl` — `<validation_mandate>` top-level rule
- `backend/pkg/templates/prompts/reporter.tmpl` — Confirmed vs Potential Issues sections

### Runtime Test
Run a Juice Shop flow where nikto flags an issue (e.g., "server header disclosure") but active curl verification does NOT demonstrate exploitable impact → verify it appears in "Potential Issues / Needs Verification" section of the report, NOT in the "Confirmed Vulnerabilities" section.

---

## Provider Registry (reference)

The provider registry (`backend/pkg/providers/registry.go`) supports the following provider types. For vLLM deployment, use the `custom` provider:

| Provider | Env Var Gate | Use Case |
|---|---|---|
| `custom` | `LLM_SERVER_URL` + (`LLM_SERVER_MODEL` or `LLM_SERVER_CONFIG`) | vLLM, text-generation-inference, any OpenAI-compatible server |
| `openai` | `OPENAI_API_KEY` | OpenAI API (or compatible endpoint with base URL override) |
| `ollama` | `OLLAMA_SERVER_URL` | Ollama local inference (single GPU only) |
| `anthropic` | `ANTHROPIC_API_KEY` | Anthropic Claude API |
| `gemini` | `GEMINI_API_KEY` | Google Gemini API |
| `bedrock` | AWS credentials | AWS Bedrock |
| `deepseek` | `DEEPSEEK_API_KEY` | DeepSeek API |
| `qwen` | `QWEN_API_KEY` | Qwen API (Alibaba Cloud) |
