# Prime-Time Smoke Checklist

Status: Optional manual live validation for current v1 surfaces

Purpose: cover the few end-to-end behaviors that fast local fake-based tests cannot prove by themselves.

## Before You Start

- Run `orchestrator setup`
- Confirm `OPENAI_API_KEY` is set in the environment
- Confirm `orchestrator doctor` reports the planner transport ready
- If testing `ntfy`, confirm the configured server/topic are reachable from the current machine and from the replying device
- Use a disposable test repo or a branch with clean expectations

## Manual Smoke Cases

1. Planner live call
- Run `orchestrator run --goal "return pause without executor work"`
- Confirm a real planner response is persisted
- Confirm `status` shows the latest planner outcome and a truthful stop reason

2. Executor live call
- Run `orchestrator run --goal "planner should choose one small execute slice"`
- Confirm one real `codex app-server` executor turn completes
- Confirm the executor thread metadata, turn status, and final message preview appear in `status`

3. ntfy ask/reply
- Configure `ntfy` in `setup`
- Run a goal that causes planner `ask_human`
- Confirm the exact question arrives through `ntfy`
- Send one raw reply from the phone/browser client
- Confirm the raw reply is persisted and the run stops after the second planner turn

4. Continue to completion
- Start from an unfinished run
- Run `orchestrator continue --max-cycles 3`
- Confirm it stops only on planner `complete`, planner `ask_human`, max cycles, or a persisted mechanical failure

5. Resume after stop
- Start from an unfinished run paused at a safe checkpoint
- Run `orchestrator resume`
- Confirm it resumes the existing run instead of creating a new one
- Confirm it stops again after one bounded cycle

6. Status, history, and doctor sanity
- Run `orchestrator status`
- Run `orchestrator history --limit 5`
- Run `orchestrator doctor`
- Confirm the latest checkpoint, stop reason, latest run status, and current mechanical readiness all read cleanly

## Expected Outcome

- Automated tests cover durable bounded-cycle behavior with local fakes
- This checklist covers live planner transport, live executor transport, live `ntfy`, and operator-facing sanity in the real environment
