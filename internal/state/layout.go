package state

import (
	"os"
	"path/filepath"
)

const (
	orchestratorDirName = ".orchestrator"
	stateDirName        = "state"
	logsDirName         = "logs"
	dbFileName          = "orchestrator.db"
	journalFileName     = "events.jsonl"
)

type Layout struct {
	RepoRoot    string
	RootDir     string
	StateDir    string
	LogsDir     string
	DBPath      string
	JournalPath string
}

func ResolveLayout(repoRoot string) Layout {
	rootDir := filepath.Join(repoRoot, orchestratorDirName)
	stateDir := filepath.Join(rootDir, stateDirName)
	logsDir := filepath.Join(rootDir, logsDirName)

	return Layout{
		RepoRoot:    repoRoot,
		RootDir:     rootDir,
		StateDir:    stateDir,
		LogsDir:     logsDir,
		DBPath:      filepath.Join(stateDir, dbFileName),
		JournalPath: filepath.Join(logsDir, journalFileName),
	}
}

func EnsureRuntimeDirs(layout Layout) error {
	for _, dir := range []string{layout.RootDir, layout.StateDir, layout.LogsDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}

	return nil
}
