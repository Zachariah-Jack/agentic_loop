# PLANS.md

Use this file when work requires more than one meaningful step.

## Planning Rules

- Start from the current source-of-truth docs before proposing or implementing a plan.
- Break work into the smallest slices that still produce a meaningful reviewable outcome.
- Each slice must name the files it will touch.
- Each slice must have explicit acceptance criteria.
- Each slice should be independently understandable in review.
- Do not mix architecture changes, persistence changes, and UI changes in one large pass unless the slice cannot be split.

## Required Contents For A Multi-Step Plan

Every non-trivial plan should state:

1. The objective.
2. The smallest useful next slice.
3. The files expected to change.
4. The acceptance criteria for that slice.
5. Any locked decision or dependency that the slice relies on.

## Slice Discipline

- Prefer a working spine before refinements.
- Prefer one durable boundary at a time.
- Prefer contract-first changes when a subsystem boundary is still moving.
- Stop at the first clean checkpoint instead of rolling multiple slices together.

## Reviewability Rules

- Keep changes small enough that a reviewer can understand intent quickly.
- Avoid speculative abstractions.
- Avoid placeholder implementations that defer the real hard part.
- If a slice changes behavior, include the acceptance criteria near the change or in the relevant doc.

## Completion Rules

- Mark a slice complete only when its acceptance criteria are met.
- If a new decision becomes locked, record it in the canonical doc or ADR before moving on.
- If ambiguity remains, stop at the checkpoint and surface the exact question instead of guessing deep into implementation.
