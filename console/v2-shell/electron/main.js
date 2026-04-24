const path = require("node:path");
const { app, BrowserWindow, ipcMain } = require("electron");
const { createTerminalManager } = require("./terminal-manager");

const {
  normalizeControlBaseURL,
  getStatusSnapshot,
  startRun,
  continueRun,
  testPlannerModel,
  testExecutorModel,
  approveExecutor,
  denyExecutor,
  listRecentArtifacts,
  getArtifact,
  listContractFiles,
  openContractFile,
  saveContractFile,
  runAIAutofill,
  listRepoTree,
  openRepoFile,
  injectControlMessage,
  sendSideChatMessage,
  listSideChatMessages,
  captureDogfoodIssue,
  listDogfoodIssues,
  listWorkers,
  createWorker,
  dispatchWorker,
  removeWorker,
  integrateWorkers,
  setVerbosity,
  setStopSafe,
  clearStopFlag,
  streamControlEvents,
} = require("../src/protocol/client");

const defaultAddress = "http://127.0.0.1:44777";
const windowState = new Map();

app.setName("Orchestrator Console");

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
      abortController: null,
      lastEventSequence: 0,
      terminal: createTerminalManager(),
    });

    const state = windowState.get(key);
    state.terminal.onState((snapshot) => {
      if (!browserWindow.isDestroyed()) {
        browserWindow.webContents.send("terminal:state", snapshot);
      }
    });
    state.terminal.onData((payload) => {
      if (!browserWindow.isDestroyed()) {
        browserWindow.webContents.send("terminal:data", payload);
      }
    });
  }
  return windowState.get(key);
}

function connectionSnapshot(browserWindow) {
  const state = stateForWindow(browserWindow);
  return {
    connected: state.connected,
    status: state.status,
    address: state.address,
    message: state.message,
  };
}

function emitConnectionState(browserWindow, overrides) {
  if (!browserWindow || browserWindow.isDestroyed()) {
    return;
  }

  const state = stateForWindow(browserWindow);
  Object.assign(state, overrides);
  browserWindow.webContents.send("protocol:connection-state", connectionSnapshot(browserWindow));
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
      if (!browserWindow.isDestroyed()) {
        browserWindow.webContents.send("protocol:event", event);
      }
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
  };
}

function requireConnectedAddress(browserWindow, requestedAddress) {
  if (requestedAddress && requestedAddress.trim() !== "") {
    return normalizeControlBaseURL(requestedAddress);
  }

  const state = stateForWindow(browserWindow);
  return normalizeControlBaseURL(state.address);
}

function createWindow() {
  const browserWindow = new BrowserWindow({
    width: 1380,
    height: 920,
    minWidth: 1100,
    minHeight: 760,
    title: "Orchestrator Console",
    icon: path.join(__dirname, "..", "assets", "icon.svg"),
    webPreferences: {
      preload: path.join(__dirname, "preload.js"),
      contextIsolation: true,
      nodeIntegration: false,
    },
  });

  browserWindow.loadFile(path.join(__dirname, "..", "src", "renderer", "index.html"));
  browserWindow.on("closed", () => {
    stopEventStream(browserWindow);
    const key = browserWindow.webContents.id;
    const state = windowState.get(key);
    if (state) {
      state.terminal.shutdown();
    }
    windowState.delete(key);
  });
  return browserWindow;
}

function shutdownOwnedWindowState() {
  for (const state of windowState.values()) {
    if (state.abortController) {
      state.abortController.abort();
      state.abortController = null;
    }
    if (state.terminal) {
      state.terminal.shutdown();
    }
  }
  windowState.clear();
}

ipcMain.handle("protocol:connect", async (event, payload = {}) => {
  return connectWindow(browserWindowForEvent(event), payload.address || defaultAddress);
});

ipcMain.handle("protocol:get-connection-state", async (event) => {
  return connectionSnapshot(browserWindowForEvent(event));
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

ipcMain.handle("protocol:set-verbosity", async (event, payload = {}) => {
  const browserWindow = browserWindowForEvent(event);
  return setVerbosity(requireConnectedAddress(browserWindow, payload.address), payload.verbosity || "normal");
});

ipcMain.handle("protocol:stop-safe", async (event, payload = {}) => {
  const browserWindow = browserWindowForEvent(event);
  return setStopSafe(requireConnectedAddress(browserWindow, payload.address), payload.runId || "", payload.reason || "");
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
  createWindow();

  app.on("activate", () => {
    if (BrowserWindow.getAllWindows().length === 0) {
      createWindow();
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
