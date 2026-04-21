# Release Checklist

Mark each item pass or fail before calling the current build ready.

- [ ] `go test ./...` passes
- [ ] `orchestrator setup` completes and keeps `OPENAI_API_KEY` environment-only
- [ ] `orchestrator doctor` is green for the target operator environment
- [ ] `orchestrator init` scaffolds a clean target repo correctly
- [ ] `orchestrator run --goal "..."` works on a real target repo
- [ ] `orchestrator resume` works on an unfinished run
- [ ] `orchestrator continue --max-cycles N` stops on a truthful mechanical boundary
- [ ] terminal `ask_human` works and stores the raw reply
- [ ] `ntfy` `ask_human` works when configured, or terminal fallback remains usable
- [ ] `orchestrator status` and `orchestrator history` show truthful operator state
- [ ] `orchestrator version` shows version, revision, and build time
- [ ] `scripts/build-release.ps1` produces a portable Windows release under `dist\windows-amd64\`
- [ ] `scripts/build-installer.ps1` produces an installer when Inno Setup is available
- [ ] orchestration-owned artifacts land under `.orchestrator/artifacts/`
- [ ] planner-owned completion marks the run completed without CLI invention
