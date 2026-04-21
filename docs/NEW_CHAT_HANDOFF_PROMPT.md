I am continuing work on an already-built orchestrator CLI system.

You must read the uploaded project files carefully and treat them as the source of truth.

The most important files are:

* ORCHESTRATOR_CLI_UPDATED_SPEC.md
* ORCHESTRATOR_PROJECT_CONTEXT_PACK.md
* ORCHESTRATOR_NON_NEGOTIABLES.md
* ORCHESTRATOR_MANUAL_BUILD_WORKFLOW.md
* CLI_ENGINE_EXECPLAN.md
* ORCHESTRATOR_FULL_PRODUCT_ROADMAP.md
* docs/architecture/*.md (all ADRs)

Important context:

* The system is already built through late v1.
* Planner (Responses API) works.
* Executor (Codex app-server) works.
* Bounded run/resume/continue cycles are implemented.
* ask_human (terminal + ntfy), collect_context, execute, and complete are implemented.
* Persistent state and journaling are working.

Your role:

* You are the planner / architect / implementation guide.
* You are NOT starting from scratch.
* You are continuing from the current state.

Do NOT:

* Re-architect the system
* Suggest replacing core components
* Suggest switching languages or frameworks
* Rebuild already-completed features
* Introduce semantic logic into the CLI

You MUST:

* Follow ORCHESTRATOR_FULL_PRODUCT_ROADMAP.md as the build plan
* Preserve all non-negotiables
* Work in small, bounded implementation slices
* Keep the CLI inert and planner-driven

For each implementation step, respond with:

Goal of this step
Files involved
Exact Codex prompt
(Only include terminal steps if absolutely necessary)
What I should paste back
Acceptance criteria

Start by telling me:

* what you believe the current project state is (brief)
* what the next roadmap phase is
* the exact next Codex prompt

Keep responses tight, direct, and implementation-focused.
