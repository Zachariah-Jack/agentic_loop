const path = require("node:path");
const { execFile } = require("node:child_process");
const { app, BrowserWindow, dialog, ipcMain, Menu, shell } = require("electron");
const { createTerminalManager } = require("./terminal-manager");
const { shutdownWindowStateByKey, shutdownAllWindowStates } = require("./window-state-cleanup");
const { ensureBackendBinary, killProcessTree, restartOwnedBackend, startBackendForRepo } = require("./backend-manager");
const packageInfo = require("../package.json");

const {
  normalizeControlBaseURL,
  getStatusSnapshot,
  startRun,
  continueRun,
  getActiveRunGuard,
  recoverStaleRun,
  testPlannerModel,
  testExecutorModel,
  approveExecutor,
  denyExecutor,
  listRecentArtifacts,
  getArtifact,
  listContractFiles,
  openContractFile,
  saveContractFile,
  getSetupHealth,
  runSetupAction,
  captureSnapshot,
  runAIAutofill,
  listRepoTree,
  openRepoFile,
  injectControlMessage,
  sendSideChatMessage,
  listSideChatMessages,
  sideChatContextSnapshot,
  sideChatActionRequest,
  captureDogfoodIssue,
  listDogfoodIssues,
  listWorkers,
  createWorker,
  dispatchWorker,
  removeWorker,
  integrateWorkers,
  getRuntimeConfig,
  setRuntimeConfig,
  testNtfy,
  checkForUpdates,
  getUpdateStatus,
  installUpdate,
  skipUpdate,
  getUpdateChangelog,
  setVerbosity,
  setStopSafe,
  pauseAtSafePoint,
  clearStopFlag,
  streamControlEvents,
} = require("../src/protocol/client");

const defaultAddress = "http://127.0.0.1:44777";
const expectedRepoPath = String(process.env.ORCHESTRATOR_V2_EXPECTED_REPO || "").trim();
const projectRoot = path.resolve(__dirname, "..", "..", "..");
const windowState = new Map();

app.setName("Aurora Orchestrator");

function shouldShowNativeMenu() {
  return String(process.env.ORCHESTRATOR_SHOW_ELECTRON_MENU || "").trim() === "1";
}

function stateForWindow(browserWindow) {
  if (!browserWindow || browserWindow.isDestroyed()) {
    throw new Error("window is unavailable");
  }

  const key = browserWindow.webContents.id;
  if (!windowState.has(key)) {
    windowState.set(key, {
      address: defaultAddress,
      connected: false,
      status: "disconnected",
      message: "not connected",
      expectedRepoPath,
      abortController: null,
      lastEventSequence: 0,
      terminal: createTerminalManager(),
    });

    const state = windowState.get(key);
    state.terminal.onState((snapshot) => {
      sendToWindow(browserWindow, "terminal:state", snapshot);
    });
    state.terminal.onData((payload) => {
      sendToWindow(browserWindow, "terminal:data", payload);
    });
  }
  return windowState.get(key);
}

function sendToWindow(browserWindow, channel, payload) {
  if (!browserWindow || browserWindow.isDestroyed()) {
    return false;
  }
  if (!browserWindow.webContents || browserWindow.webContents.isDestroyed()) {
    return false;
  }
  browserWindow.webContents.send(channel, payload);
  return true;
}

function connectionSnapshot(browserWindow) {
  const state = stateForWindow(browserWindow);
  return {
    connected: state.connected,
    status: state.status,
    address: state.address,
    message: state.message,
    expectedRepoPath: state.expectedRepoPath || expectedRepoPath,
    repoPath: state.repoPath || state.expectedRepoPath || expectedRepoPath,
    ownedBackend: Boolean(state.ownedBackend),
    pid: state.pid || 0,
    autoConnect: Boolean(state.autoConnect),
  };
}

function emitConnectionState(browserWindow, overrides) {
  if (!browserWindow || browserWindow.isDestroyed()) {
    return;
  }

  const state = stateForWindow(browserWindow);
  Object.assign(state, overrides);
  sendToWindow(browserWindow, "protocol:connection-state", connectionSnapshot(browserWindow));
}

function stopEventStream(browserWindow) {
  if (!browserWindow || browserWindow.isDestroyed()) {
    return;
  }

  const state = stateForWindow(browserWindow);
  if (state.abortController) {
    state.abortController.abort();
    state.abortController = null;
  }
}

async function startEventStream(browserWindow, address) {
  stopEventStream(browserWindow);

  const state = stateForWindow(browserWindow);
  const controller = new AbortController();
  state.abortController = controller;

  streamControlEvents(
    address,
    { fromSequence: state.lastEventSequence },
    (event) => {
      if (Number.isFinite(event && event.sequence)) {
        state.lastEventSequence = Math.max(state.lastEventSequence, event.sequence);
      }
      sendToWindow(browserWindow, "protocol:event", event);
    },
    { signal: controller.signal },
  ).then(
    () => {
      if (!controller.signal.aborted && !browserWindow.isDestroyed()) {
        emitConnectionState(browserWindow, {
          connected: false,
          status: "disconnected",
          message: "event stream ended",
          abortController: null,
        });
      }
    },
    (error) => {
      if (controller.signal.aborted || browserWindow.isDestroyed()) {
        return;
      }
      emitConnectionState(browserWindow, {
        connected: false,
        status: "error",
        message: error.message,
        abortController: null,
      });
    },
  );
}

function browserWindowForEvent(event) {
  const browserWindow = BrowserWindow.fromWebContents(event.sender);
  if (!browserWindow) {
    throw new Error("window is unavailable");
  }
  return browserWindow;
}

async function connectWindow(browserWindow, rawAddress) {
  const address = normalizeControlBaseURL(rawAddress);
  const state = stateForWindow(browserWindow);
  if (state.address !== address) {
    state.lastEventSequence = 0;
  }
  emitConnectionState(browserWindow, {
    connected: false,
    status: "connecting",
    address,
    message: "connecting to control server",
  });

  const snapshot = await getStatusSnapshot(address, "");
  emitConnectionState(browserWindow, {
    connected: true,
    status: "connected",
    address,
    message: "connected to control server",
  });
  await startEventStream(browserWindow, address);

  return {
    connection: connectionSnapshot(browserWindow),
    snapshot,
    expectedRepoPath: state.expectedRepoPath || expectedRepoPath,
  };
}

function requireConnectedAddress(browserWindow, requestedAddress) {
  if (requestedAddress && requestedAddress.trim() !== "") {
    return normalizeControlBaseURL(requestedAddress);
  }

  const state = stateForWindow(browserWindow);
  return normalizeControlBaseURL(state.address);
}

function createWindow(options = {}) {
  const browserWindow = new BrowserWindow({
    width: 1380,
    height: 920,
    minWidth: 1100,
    minHeight: 760,
    title: "Aurora Orchestrator",
    icon: path.join(__dirname, "..", "assets", "icon.svg"),
    autoHideMenuBar: true,
    webPreferences: {
      preload: path.join(__dirname, "preload.js"),
      contextIsolation: true,
      nodeIntegration: false,
    },
  });

  if (!shouldShowNativeMenu()) {
    browserWindow.setAutoHideMenuBar(true);
    browserWindow.setMenuBarVisibility(false);
  }

  const windowKey = browserWindow.webContents.id;
  const state = stateForWindow(browserWindow);
  if (options.address) {
    state.address = options.address;
  }
  if (options.expectedRepoPath || options.repoPath) {
    state.expectedRepoPath = options.expectedRepoPath || options.repoPath;
    state.repoPath = options.repoPath || options.expectedRepoPath;
  }
  if (options.pid) {
    state.pid = options.pid;
  }
  state.ownedBackend = Boolean(options.ownedBackend);
  state.autoConnect = Boolean(options.autoConnect);
  browserWindow.loadFile(path.join(__dirname, "..", "src", "renderer", "index.html"));
  browserWindow.on("close", () => {
    const state = windowState.get(windowKey);
    if (state && state.abortController) {
      state.abortController.abort();
      state.abortController = null;
    }
  });
  browserWindow.on("closed", () => {
    shutdownWindowStateByKey(windowState, windowKey);
  });
  return browserWindow;
}

function shutdownOwnedWindowState() {
  shutdownAllWindowStates(windowState);
}

function shouldOpenLauncher() {
  if (String(process.env.ORCHESTRATOR_V2_FORCE_LAUNCHER || "").trim() === "1") {
    return true;
  }
  if (String(process.env.ORCHESTRATOR_V2_SKIP_LAUNCHER || "").trim() === "1") {
    return false;
  }
  return expectedRepoPath === "";
}

function createLauncherWindow() {
  const launcherWindow = new BrowserWindow({
    width: 680,
    height: 640,
    minWidth: 600,
    minHeight: 560,
    title: "Aurora Orchestrator Launcher",
    icon: path.join(__dirname, "..", "assets", "icon.svg"),
    autoHideMenuBar: true,
    webPreferences: {
      preload: path.join(__dirname, "launcher-preload.js"),
      contextIsolation: true,
      nodeIntegration: false,
    },
  });

  if (!shouldShowNativeMenu()) {
    launcherWindow.setAutoHideMenuBar(true);
    launcherWindow.setMenuBarVisibility(false);
  }

  launcherWindow.loadFile(path.join(__dirname, "..", "src", "launcher", "index.html"));
  return launcherWindow;
}

function parseUpdateStatus(output) {
  const status = {
    currentVersion: packageInfo.version,
    latestVersion: "",
    updateAvailable: false,
    releaseURL: "",
    installSupported: false,
    installMessage: "",
  };
  for (const line of String(output || "").split(/\r?\n/)) {
    const match = line.match(/^\s*([a-z_]+):\s*(.*)$/i);
    if (!match) {
      continue;
    }
    const key = match[1].toLowerCase();
    const value = match[2].trim();
    if (key === "current_version") {
      status.currentVersion = value;
    } else if (key === "latest_version" && value.toLowerCase() !== "unavailable") {
      status.latestVersion = value;
    } else if (key === "update_available") {
      status.updateAvailable = value.toLowerCase() === "true";
    } else if (key === "release_url" && value.toLowerCase() !== "unavailable") {
      status.releaseURL = value;
    } else if (key === "install_supported") {
      status.installSupported = value.toLowerCase() === "true";
    } else if (key === "install_message") {
      status.installMessage = value;
    }
  }
  return status;
}

function runBinary(command, args, options = {}) {
  return new Promise((resolve, reject) => {
    execFile(command, args, { windowsHide: true, timeout: options.timeout || 30000, cwd: options.cwd || projectRoot }, (error, stdout, stderr) => {
      if (error) {
        error.stdout = stdout;
        error.stderr = stderr;
        reject(error);
        return;
      }
      resolve({ stdout, stderr });
    });
  });
}

async function checkLauncherUpdates() {
  try {
    const binaryPath = await ensureBackendBinary();
    const result = await runBinary(binaryPath, ["update", "check", "--include-prereleases"], { cwd: projectRoot });
    const status = parseUpdateStatus(result.stdout);
    return {
      ok: true,
      ...status,
      message: status.updateAvailable
        ? `Update Aurora Orchestrator to version ${status.latestVersion}.`
        : "You are using the most up to date version of Aurora Orchestrator, have fun building things!",
    };
  } catch (error) {
    return {
      ok: false,
      currentVersion: packageInfo.version,
      latestVersion: "",
      updateAvailable: false,
      installSupported: false,
      installMessage: "Automatic install is not available yet. Use Check for Updates again later or install a published release manually.",
      message: `Update check could not complete: ${error.message}`,
    };
  }
}

ipcMain.handle("launcher:get-info", async () => ({
  version: packageInfo.version,
  projectRoot,
  defaultRepoPath: expectedRepoPath,
}));

ipcMain.handle("launcher:open-readme", async () => {
  const result = await shell.openPath(path.join(projectRoot, "README.md"));
  return { ok: result === "", message: result };
});

ipcMain.handle("launcher:select-repo", async (event) => {
  const browserWindow = browserWindowForEvent(event);
  const result = await dialog.showOpenDialog(browserWindow, {
    title: "Choose the repo or project folder Aurora should work on",
    properties: ["openDirectory"],
  });
  if (result.canceled || !result.filePaths || result.filePaths.length === 0) {
    return { cancelled: true };
  }
  const repoPath = result.filePaths[0];
  return {
    cancelled: false,
    repoPath,
    label: path.basename(repoPath),
  };
});

ipcMain.handle("launcher:check-updates", async () => checkLauncherUpdates());

ipcMain.handle("launcher:start", async (event, payload = {}) => {
  const launcherWindow = browserWindowForEvent(event);
  const repoPath = String(payload.repoPath || "").trim();
  const session = await startBackendForRepo({ repoPath });
  createWindow({
    address: session.address,
    expectedRepoPath: session.repoPath,
    repoPath: session.repoPath,
    pid: session.pid,
    ownedBackend: true,
    autoConnect: true,
  });
  if (!launcherWindow.isDestroyed()) {
    launcherWindow.close();
  }
  return session;
});

ipcMain.handle("protocol:connect", async (event, payload = {}) => {
  return connectWindow(browserWindowForEvent(event), payload.address || defaultAddress);
});

ipcMain.handle("protocol:get-connection-state", async (event) => {
  return connectionSnapshot(browserWindowForEvent(event));
});

ipcMain.handle("shell:restart-backend", async (event, payload = {}) => {
  const browserWindow = browserWindowForEvent(event);
  stopEventStream(browserWindow);
  try {
    stateForWindow(browserWindow).terminal.shutdown();
  } catch (_error) {
    // Recovery must still proceed if a terminal session has already exited.
  }
  emitConnectionState(browserWindow, {
    connected: false,
    status: "reconnecting",
    message: "restarting owned backend",
  });
  return restartOwnedBackend({
    address: payload.address || requireConnectedAddress(browserWindow, ""),
  });
});

ipcMain.handle("shell:open-repo-session", async (event) => {
  const browserWindow = browserWindowForEvent(event);
  const result = await dialog.showOpenDialog(browserWindow, {
    title: "Open Aurora project folder",
    properties: ["openDirectory"],
  });
  if (result.canceled || !result.filePaths || result.filePaths.length === 0) {
    return { cancelled: true };
  }
  return startBackendForRepo({ repoPath: result.filePaths[0] });
});

ipcMain.handle("shell:close-repo-session", async (_event, payload = {}) => {
  if (!payload || !payload.pid) {
    return { attempted: false, message: "no owned backend pid provided" };
  }
  return killProcessTree(payload.pid);
});

ipcMain.handle("protocol:get-status", async (event, payload = {}) => {
  const browserWindow = browserWindowForEvent(event);
  return getStatusSnapshot(requireConnectedAddress(browserWindow, payload.address), payload.runId || "");
});

ipcMain.handle("protocol:start-run", async (event, payload = {}) => {
  const browserWindow = browserWindowForEvent(event);
  return startRun(requireConnectedAddress(browserWindow, payload.address), {
    goal: payload.goal || "",
    repo_path: payload.repoPath || "",
    mode: payload.mode || "",
    verbosity: payload.verbosity || "",
  });
});

ipcMain.handle("protocol:continue-run", async (event, payload = {}) => {
  const browserWindow = browserWindowForEvent(event);
  return continueRun(requireConnectedAddress(browserWindow, payload.address), {
    run_id: payload.runId || "",
    repo_path: payload.repoPath || "",
    mode: payload.mode || "",
  });
});

ipcMain.handle("protocol:get-active-run-guard", async (event, payload = {}) => {
  const browserWindow = browserWindowForEvent(event);
  return getActiveRunGuard(requireConnectedAddress(browserWindow, payload.address));
});

ipcMain.handle("protocol:recover-stale-run", async (event, payload = {}) => {
  const browserWindow = browserWindowForEvent(event);
  return recoverStaleRun(requireConnectedAddress(browserWindow, payload.address), {
    run_id: payload.runId || "",
    reason: payload.reason || "operator_recovery",
    force: payload.force === undefined ? true : Boolean(payload.force),
  });
});

ipcMain.handle("protocol:test-planner-model", async (event, payload = {}) => {
  const browserWindow = browserWindowForEvent(event);
  return testPlannerModel(requireConnectedAddress(browserWindow, payload.address), {
    model: payload.model || "",
  });
});

ipcMain.handle("protocol:test-executor-model", async (event, payload = {}) => {
  const browserWindow = browserWindowForEvent(event);
  return testExecutorModel(requireConnectedAddress(browserWindow, payload.address), {
    model: payload.model || "",
  });
});

ipcMain.handle("protocol:approve-executor", async (event, payload = {}) => {
  const browserWindow = browserWindowForEvent(event);
  return approveExecutor(requireConnectedAddress(browserWindow, payload.address), payload.runId || "");
});

ipcMain.handle("protocol:deny-executor", async (event, payload = {}) => {
  const browserWindow = browserWindowForEvent(event);
  return denyExecutor(requireConnectedAddress(browserWindow, payload.address), payload.runId || "");
});

ipcMain.handle("protocol:list-recent-artifacts", async (event, payload = {}) => {
  const browserWindow = browserWindowForEvent(event);
  return listRecentArtifacts(requireConnectedAddress(browserWindow, payload.address), {
    run_id: payload.runId || "",
    category: payload.category || "",
    limit: payload.limit || 12,
  });
});

ipcMain.handle("protocol:get-artifact", async (event, payload = {}) => {
  const browserWindow = browserWindowForEvent(event);
  return getArtifact(requireConnectedAddress(browserWindow, payload.address), payload.artifactPath || "");
});

ipcMain.handle("protocol:list-contract-files", async (event, payload = {}) => {
  const browserWindow = browserWindowForEvent(event);
  return listContractFiles(requireConnectedAddress(browserWindow, payload.address), payload.repoPath || "");
});

ipcMain.handle("protocol:open-contract-file", async (event, payload = {}) => {
  const browserWindow = browserWindowForEvent(event);
  return openContractFile(requireConnectedAddress(browserWindow, payload.address), {
    repo_path: payload.repoPath || "",
    path: payload.path || "",
  });
});

ipcMain.handle("protocol:save-contract-file", async (event, payload = {}) => {
  const browserWindow = browserWindowForEvent(event);
  return saveContractFile(requireConnectedAddress(browserWindow, payload.address), {
    repo_path: payload.repoPath || "",
    path: payload.path || "",
    content: payload.content || "",
    expected_mtime: payload.expectedMTime || "",
  });
});

ipcMain.handle("protocol:get-setup-health", async (event, payload = {}) => {
  const browserWindow = browserWindowForEvent(event);
  return getSetupHealth(requireConnectedAddress(browserWindow, payload.address));
});

ipcMain.handle("protocol:run-setup-action", async (event, payload = {}) => {
  const browserWindow = browserWindowForEvent(event);
  return runSetupAction(requireConnectedAddress(browserWindow, payload.address), {
    action: payload.action || "",
    repo_path: payload.repoPath || "",
  });
});

ipcMain.handle("protocol:capture-snapshot", async (event, payload = {}) => {
  const browserWindow = browserWindowForEvent(event);
  return captureSnapshot(requireConnectedAddress(browserWindow, payload.address), {
    run_id: payload.runId || "",
    repo_path: payload.repoPath || "",
  });
});

ipcMain.handle("protocol:run-ai-autofill", async (event, payload = {}) => {
  const browserWindow = browserWindowForEvent(event);
  return runAIAutofill(requireConnectedAddress(browserWindow, payload.address), {
    repo_path: payload.repoPath || "",
    targets: Array.isArray(payload.targets) ? payload.targets : [],
    answers: payload.answers || {},
  });
});

ipcMain.handle("protocol:list-repo-tree", async (event, payload = {}) => {
  const browserWindow = browserWindowForEvent(event);
  return listRepoTree(requireConnectedAddress(browserWindow, payload.address), {
    repo_path: payload.repoPath || "",
    path: payload.path || "",
    limit: payload.limit || 200,
  });
});

ipcMain.handle("protocol:open-repo-file", async (event, payload = {}) => {
  const browserWindow = browserWindowForEvent(event);
  return openRepoFile(requireConnectedAddress(browserWindow, payload.address), {
    repo_path: payload.repoPath || "",
    path: payload.path || "",
  });
});

ipcMain.handle("protocol:inject-control-message", async (event, payload = {}) => {
  const browserWindow = browserWindowForEvent(event);
  return injectControlMessage(requireConnectedAddress(browserWindow, payload.address), {
    run_id: payload.runId || "",
    message: payload.message || "",
    source: payload.source || "control_chat",
    reason: payload.reason || "operator_intervention",
  });
});

ipcMain.handle("protocol:send-side-chat-message", async (event, payload = {}) => {
  const browserWindow = browserWindowForEvent(event);
  return sendSideChatMessage(requireConnectedAddress(browserWindow, payload.address), {
    repo_path: payload.repoPath || "",
    message: payload.message || "",
    context_policy: payload.contextPolicy || "",
  });
});

ipcMain.handle("protocol:list-side-chat-messages", async (event, payload = {}) => {
  const browserWindow = browserWindowForEvent(event);
  return listSideChatMessages(requireConnectedAddress(browserWindow, payload.address), {
    repo_path: payload.repoPath || "",
    limit: payload.limit || 20,
  });
});

ipcMain.handle("protocol:side-chat-context-snapshot", async (event, payload = {}) => {
  const browserWindow = browserWindowForEvent(event);
  return sideChatContextSnapshot(requireConnectedAddress(browserWindow, payload.address), {
    repo_path: payload.repoPath || "",
    run_id: payload.runId || "",
    limit: payload.limit || 10,
  });
});

ipcMain.handle("protocol:side-chat-action-request", async (event, payload = {}) => {
  const browserWindow = browserWindowForEvent(event);
  return sideChatActionRequest(requireConnectedAddress(browserWindow, payload.address), {
    repo_path: payload.repoPath || "",
    run_id: payload.runId || "",
    action: payload.action || "",
    message: payload.message || "",
    source: payload.source || "",
    reason: payload.reason || "",
    approved: Boolean(payload.approved),
  });
});

ipcMain.handle("protocol:capture-dogfood-issue", async (event, payload = {}) => {
  const browserWindow = browserWindowForEvent(event);
  return captureDogfoodIssue(requireConnectedAddress(browserWindow, payload.address), {
    repo_path: payload.repoPath || "",
    run_id: payload.runId || "",
    source: payload.source || "operator_shell",
    title: payload.title || "",
    note: payload.note || "",
  });
});

ipcMain.handle("protocol:list-dogfood-issues", async (event, payload = {}) => {
  const browserWindow = browserWindowForEvent(event);
  return listDogfoodIssues(requireConnectedAddress(browserWindow, payload.address), {
    repo_path: payload.repoPath || "",
    limit: payload.limit || 20,
  });
});

ipcMain.handle("protocol:list-workers", async (event, payload = {}) => {
  const browserWindow = browserWindowForEvent(event);
  return listWorkers(requireConnectedAddress(browserWindow, payload.address), {
    run_id: payload.runId || "",
    limit: payload.limit || 20,
  });
});

ipcMain.handle("protocol:create-worker", async (event, payload = {}) => {
  const browserWindow = browserWindowForEvent(event);
  return createWorker(requireConnectedAddress(browserWindow, payload.address), {
    run_id: payload.runId || "",
    name: payload.name || "",
    scope: payload.scope || "",
  });
});

ipcMain.handle("protocol:dispatch-worker", async (event, payload = {}) => {
  const browserWindow = browserWindowForEvent(event);
  return dispatchWorker(requireConnectedAddress(browserWindow, payload.address), {
    worker_id: payload.workerId || "",
    prompt: payload.prompt || "",
  });
});

ipcMain.handle("protocol:remove-worker", async (event, payload = {}) => {
  const browserWindow = browserWindowForEvent(event);
  return removeWorker(requireConnectedAddress(browserWindow, payload.address), {
    worker_id: payload.workerId || "",
  });
});

ipcMain.handle("protocol:integrate-workers", async (event, payload = {}) => {
  const browserWindow = browserWindowForEvent(event);
  return integrateWorkers(requireConnectedAddress(browserWindow, payload.address), {
    worker_ids: Array.isArray(payload.workerIds) ? payload.workerIds : [],
  });
});

ipcMain.handle("protocol:get-runtime-config", async (event, payload = {}) => {
  const browserWindow = browserWindowForEvent(event);
  return getRuntimeConfig(requireConnectedAddress(browserWindow, payload.address));
});

ipcMain.handle("protocol:set-runtime-config", async (event, payload = {}) => {
  const browserWindow = browserWindowForEvent(event);
  const { address, ...patch } = payload || {};
  return setRuntimeConfig(requireConnectedAddress(browserWindow, address), patch);
});

ipcMain.handle("protocol:test-ntfy", async (event, payload = {}) => {
  const browserWindow = browserWindowForEvent(event);
  return testNtfy(requireConnectedAddress(browserWindow, payload.address), {});
});

ipcMain.handle("protocol:check-for-updates", async (event, payload = {}) => {
  const browserWindow = browserWindowForEvent(event);
  return checkForUpdates(requireConnectedAddress(browserWindow, payload.address), {
    include_prereleases: payload.includePrereleases,
  });
});

ipcMain.handle("protocol:get-update-status", async (event, payload = {}) => {
  const browserWindow = browserWindowForEvent(event);
  return getUpdateStatus(requireConnectedAddress(browserWindow, payload.address));
});

ipcMain.handle("protocol:install-update", async (event, payload = {}) => {
  const browserWindow = browserWindowForEvent(event);
  return installUpdate(requireConnectedAddress(browserWindow, payload.address), {
    version: payload.version || "",
  });
});

ipcMain.handle("protocol:skip-update", async (event, payload = {}) => {
  const browserWindow = browserWindowForEvent(event);
  return skipUpdate(requireConnectedAddress(browserWindow, payload.address), {
    version: payload.version || "",
  });
});

ipcMain.handle("protocol:get-update-changelog", async (event, payload = {}) => {
  const browserWindow = browserWindowForEvent(event);
  return getUpdateChangelog(requireConnectedAddress(browserWindow, payload.address), {
    include_prereleases: payload.includePrereleases,
  });
});

ipcMain.handle("protocol:set-verbosity", async (event, payload = {}) => {
  const browserWindow = browserWindowForEvent(event);
  return setVerbosity(requireConnectedAddress(browserWindow, payload.address), payload.verbosity || "normal");
});

ipcMain.handle("protocol:stop-safe", async (event, payload = {}) => {
  const browserWindow = browserWindowForEvent(event);
  return setStopSafe(requireConnectedAddress(browserWindow, payload.address), payload.runId || "", payload.reason || "");
});

ipcMain.handle("protocol:pause-at-safe-point", async (event, payload = {}) => {
  const browserWindow = browserWindowForEvent(event);
  return pauseAtSafePoint(requireConnectedAddress(browserWindow, payload.address), payload.runId || "", payload.reason || "");
});

ipcMain.handle("protocol:clear-stop", async (event, payload = {}) => {
  const browserWindow = browserWindowForEvent(event);
  return clearStopFlag(requireConnectedAddress(browserWindow, payload.address), payload.runId || "");
});

ipcMain.handle("terminal:get-state", async (event) => {
  const browserWindow = browserWindowForEvent(event);
  return stateForWindow(browserWindow).terminal.snapshot();
});

ipcMain.handle("terminal:start", async (event, payload = {}) => {
  const browserWindow = browserWindowForEvent(event);
  return stateForWindow(browserWindow).terminal.start({
    cwd: payload.cwd || process.cwd(),
    label: payload.label || "",
  });
});

ipcMain.handle("terminal:activate", async (event, payload = {}) => {
  const browserWindow = browserWindowForEvent(event);
  return stateForWindow(browserWindow).terminal.activate(payload.sessionId || "");
});

ipcMain.handle("terminal:send", async (event, payload = {}) => {
  const browserWindow = browserWindowForEvent(event);
  return stateForWindow(browserWindow).terminal.send(payload.input || "", payload.sessionId || "");
});

ipcMain.handle("terminal:stop", async (event, payload = {}) => {
  const browserWindow = browserWindowForEvent(event);
  return stateForWindow(browserWindow).terminal.stop(payload.sessionId || "");
});

ipcMain.handle("terminal:clear", async (event, payload = {}) => {
  const browserWindow = browserWindowForEvent(event);
  return stateForWindow(browserWindow).terminal.clear(payload.sessionId || "");
});

ipcMain.handle("terminal:close", async (event, payload = {}) => {
  const browserWindow = browserWindowForEvent(event);
  return stateForWindow(browserWindow).terminal.close(payload.sessionId || "");
});

app.whenReady().then(() => {
  if (!shouldShowNativeMenu()) {
    Menu.setApplicationMenu(null);
  }

  if (shouldOpenLauncher()) {
    createLauncherWindow();
  } else {
    createWindow({ expectedRepoPath, repoPath: expectedRepoPath });
  }

  app.on("activate", () => {
    if (BrowserWindow.getAllWindows().length === 0) {
      if (shouldOpenLauncher()) {
        createLauncherWindow();
      } else {
        createWindow({ expectedRepoPath, repoPath: expectedRepoPath });
      }
    }
  });
});

app.on("window-all-closed", () => {
  if (process.platform !== "darwin") {
    app.quit();
  }
});

app.on("before-quit", () => {
  shutdownOwnedWindowState();
});
