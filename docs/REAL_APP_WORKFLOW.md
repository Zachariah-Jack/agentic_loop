# Real App Workflow

Use this flow from the root of the target app repo.

## 1. Configure The Operator Environment

- Run `orchestrator setup`.
- Keep `OPENAI_API_KEY` in the environment.
- Configure `ntfy` only if you want remote ask-human delivery.
- Review `orchestrator settings show` or the V2 shell Settings tab before long runs. Executor turns and human waits default to `unlimited`; all timeout fields accept finite durations or `unlimited`.
- Choose a permission profile (`guided`, `balanced`, `autonomous`, or `full_send`) that matches how much setup/install/test autonomy you want.

## 2. Initialize The Target Repo

- Run `orchestrator init`.
- This scaffolds:
  - `.orchestrator/brief.md`
  - `.orchestrator/roadmap.md`
  - `.orchestrator/decisions.md`
  - `.orchestrator/human-notes.md`
  - `.orchestrator/state/`
  - `.orchestrator/logs/`
  - `.orchestrator/artifacts/`
  - `AGENTS.md` if it is missing

## 3. Fill In The Human-Owned Contract

Before the first real app-building run, update:
- `.orchestrator/brief.md`
- `.orchestrator/roadmap.md`
- `.orchestrator/decisions.md`

Append extra human context to `.orchestrator/human-notes.md` instead of hiding it in chat.

## 4. Run The Planner-Led Workflow

- Start a new goal with `orchestrator run --goal "..."`
- Or keep one foreground loop running with `orchestrator auto start --goal "..."`
- Use `orchestrator resume` for one more bounded cycle on the latest unfinished run
- Use `orchestrator continue --max-cycles N` for repeated bounded cycles in the foreground
- Use `orchestrator auto continue` to keep advancing the latest unfinished run automatically in the foreground

If the planner asks a human question:
- answer in the terminal, or
- answer through `ntfy` when configured

Replies are forwarded raw.

## 5. Inspect The Runtime

- `orchestrator status` shows the latest durable run snapshot
- Status and the V2 shell use `Total Build Time` for active loop-running time only. It pauses while stopped, blocked, or waiting on a human.
- `orchestrator history` shows recent runs
- `orchestrator doctor` checks target-repo contract, persistence, planner prerequisites, executor availability, and `ntfy` readiness
- The V2 shell Side Chat pane can answer from observable runtime context while the loop runs. Normal side-chat messages do not change the run; planner notes and Safe Stop requests use explicit audited actions.
- The V2 shell Updates card and `orchestrator update ...` commands read GitHub Releases and can show/copy changelogs. Self-install remains disabled until signed/checksummed Windows assets are available.

## 6. Find Logs And Artifacts

- Durable state: `.orchestrator/state/`
- Journal: `.orchestrator/logs/events.jsonl`
- Large orchestration artifacts: `.orchestrator/artifacts/`

Actual product code, tests, assets, and docs may still be written in the repo itself when the planner requests that work and the executor write scope allows it.
