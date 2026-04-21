# Windows Install And Release

This is the current Windows packaging and install runbook for the orchestrator.

## Prerequisites

For a portable build:
- Windows PowerShell
- Go installed locally
- the repository checked out locally

For an installer build:
- everything above
- Inno Setup 6 with `ISCC.exe`
  - either installed in the default location, or
  - exposed through `INNO_SETUP_PATH`

For first real use after install:
- `OPENAI_API_KEY` available in the user environment
- Codex installed and logged in
- a target repo available locally

## Build A Portable Release

From the repo root:

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\build-release.ps1
```

Optional metadata overrides:

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\build-release.ps1 `
  -Version "v1.0.0" `
  -Revision "abc1234" `
  -BuildTime "2026-04-21T19:00:00Z"
```

Outputs land under:
- `dist\windows-amd64\portable\`
- `dist\windows-amd64\orchestrator_<version>_windows_amd64_portable.zip`

The portable folder includes:
- `orchestrator.exe`
- `README.md`
- `WINDOWS_INSTALL_AND_RELEASE.md`
- `REAL_APP_WORKFLOW.md`
- `build-metadata.txt`

## Build The Windows Installer

From the repo root:

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\build-installer.ps1
```

This script:
1. builds the portable payload first when needed
2. invokes Inno Setup
3. writes installer artifacts under `dist\windows-amd64\installer\`

If Inno Setup is missing, installer creation remains a manual prerequisite rather than a hidden failure. The script stops with a mechanical error that tells you to install Inno Setup or set `INNO_SETUP_PATH`.

## Installer Behavior

The first installer path is Inno Setup based.

Current installer actions:
- installs `orchestrator.exe` under `Program Files\Orchestrator`
- installs the bundled docs and build metadata file beside the binary
- creates a Start Menu shortcut
- can optionally create a desktop shortcut
- can optionally add the install directory to the current user `PATH`

Current installer limits:
- it does not migrate existing target-repo runtime state
- it does not manage Codex installation or Codex login
- it does not write `OPENAI_API_KEY`
- it does not remove a manually added `PATH` entry on uninstall yet

## Config And Runtime Locations

User config:
- default config path comes from `os.UserConfigDir()`
- on Windows this is typically:
  - `%AppData%\orchestrator\config.json`
- you can override it with:
  - `orchestrator --config PATH ...`

Target-repo runtime state:
- SQLite state:
  - `.orchestrator\state\orchestrator.db`
- JSONL journal:
  - `.orchestrator\logs\events.jsonl`
- orchestration artifacts:
  - `.orchestrator\artifacts\`

Environment-only:
- `OPENAI_API_KEY` remains environment-only in this slice
- it is not stored by `setup`
- it is not stored by the installer

## First Run After Install

1. Open a new terminal so any PATH change is visible.
2. Run:

```powershell
orchestrator version
orchestrator setup
orchestrator doctor
```

3. In a target repo, run:

```powershell
orchestrator init
```

4. Fill in:
- `.orchestrator\brief.md`
- `.orchestrator\roadmap.md`
- `.orchestrator\decisions.md`

5. Start work with:

```powershell
orchestrator run --goal "..."
```

## Common Failure Points

- `orchestrator` not found after install
  - open a new terminal
  - or add the install directory to PATH manually

- installer build fails
  - confirm Inno Setup 6 is installed
  - confirm `ISCC.exe` is reachable or set `INNO_SETUP_PATH`

- `doctor` shows planner failure
  - confirm `OPENAI_API_KEY`

- `doctor` shows executor failure
  - confirm Codex is installed and logged in

- repo contract failures
  - run `orchestrator init` inside the target repo

## Uninstall And Cleanup

Known uninstall behavior:
- the installer removes the installed files and shortcuts
- repo-local `.orchestrator\` state is not touched
- config under `%AppData%\orchestrator\config.json` is not removed automatically
- a PATH entry added during install may need manual cleanup if you want it removed immediately

Manual cleanup targets if desired:
- `%AppData%\orchestrator\config.json`
- target repo `.orchestrator\`
- any leftover user PATH entry for the install directory
