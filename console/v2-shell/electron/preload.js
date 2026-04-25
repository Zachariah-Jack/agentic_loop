const { contextBridge, ipcRenderer } = require("electron");

const bootstrap = {
  defaultControlAddress: String(process.env.ORCHESTRATOR_V2_SHELL_ADDR || "").trim(),
  expectedRepoPath: String(process.env.ORCHESTRATOR_V2_EXPECTED_REPO || "").trim(),
};

function listen(channel, callback) {
  const wrapped = (_event, payload) => callback(payload);
  ipcRenderer.on(channel, wrapped);
  return () => ipcRenderer.removeListener(channel, wrapped);
}

contextBridge.exposeInMainWorld("orchestratorConsole", {
  bootstrap,
  connect(address) {
    return ipcRenderer.invoke("protocol:connect", { address });
  },
  getConnectionState() {
    return ipcRenderer.invoke("protocol:get-connection-state");
  },
  restartBackend(address = "") {
    return ipcRenderer.invoke("shell:restart-backend", { address });
  },
  getStatusSnapshot(runId = "", address = "") {
    return ipcRenderer.invoke("protocol:get-status", { runId, address });
  },
  startRun(payload) {
    return ipcRenderer.invoke("protocol:start-run", payload);
  },
  continueRun(payload) {
    return ipcRenderer.invoke("protocol:continue-run", payload);
  },
  getActiveRunGuard(address = "") {
    return ipcRenderer.invoke("protocol:get-active-run-guard", { address });
  },
  recoverStaleRun(runId = "", reason = "operator_recovery", force = true, address = "") {
    return ipcRenderer.invoke("protocol:recover-stale-run", { runId, reason, force, address });
  },
  testPlannerModel(model = "", address = "") {
    return ipcRenderer.invoke("protocol:test-planner-model", { model, address });
  },
  testExecutorModel(model = "", address = "") {
    return ipcRenderer.invoke("protocol:test-executor-model", { model, address });
  },
  approveExecutor(runId = "", address = "") {
    return ipcRenderer.invoke("protocol:approve-executor", { runId, address });
  },
  denyExecutor(runId = "", address = "") {
    return ipcRenderer.invoke("protocol:deny-executor", { runId, address });
  },
  listRecentArtifacts(runId = "", category = "", limit = 12, address = "") {
    return ipcRenderer.invoke("protocol:list-recent-artifacts", { runId, category, limit, address });
  },
  getArtifact(artifactPath, address = "") {
    return ipcRenderer.invoke("protocol:get-artifact", { artifactPath, address });
  },
  listContractFiles(repoPath = "", address = "") {
    return ipcRenderer.invoke("protocol:list-contract-files", { repoPath, address });
  },
  openContractFile(path, repoPath = "", address = "") {
    return ipcRenderer.invoke("protocol:open-contract-file", { path, repoPath, address });
  },
  saveContractFile(payload) {
    return ipcRenderer.invoke("protocol:save-contract-file", payload);
  },
  runAIAutofill(payload) {
    return ipcRenderer.invoke("protocol:run-ai-autofill", payload);
  },
  listRepoTree(repoPath = "", path = "", limit = 200, address = "") {
    return ipcRenderer.invoke("protocol:list-repo-tree", { repoPath, path, limit, address });
  },
  openRepoFile(path, repoPath = "", address = "") {
    return ipcRenderer.invoke("protocol:open-repo-file", { path, repoPath, address });
  },
  injectControlMessage(payload) {
    return ipcRenderer.invoke("protocol:inject-control-message", payload);
  },
  sendSideChatMessage(payload) {
    return ipcRenderer.invoke("protocol:send-side-chat-message", payload);
  },
  listSideChatMessages(repoPath = "", limit = 20, address = "") {
    return ipcRenderer.invoke("protocol:list-side-chat-messages", { repoPath, limit, address });
  },
  captureDogfoodIssue(payload) {
    return ipcRenderer.invoke("protocol:capture-dogfood-issue", payload);
  },
  listDogfoodIssues(repoPath = "", limit = 20, address = "") {
    return ipcRenderer.invoke("protocol:list-dogfood-issues", { repoPath, limit, address });
  },
  listWorkers(runId = "", limit = 20, address = "") {
    return ipcRenderer.invoke("protocol:list-workers", { runId, limit, address });
  },
  createWorker(payload) {
    return ipcRenderer.invoke("protocol:create-worker", payload);
  },
  dispatchWorker(payload) {
    return ipcRenderer.invoke("protocol:dispatch-worker", payload);
  },
  removeWorker(workerId, address = "") {
    return ipcRenderer.invoke("protocol:remove-worker", { workerId, address });
  },
  integrateWorkers(workerIds = [], address = "") {
    return ipcRenderer.invoke("protocol:integrate-workers", { workerIds, address });
  },
  getRuntimeConfig(address = "") {
    return ipcRenderer.invoke("protocol:get-runtime-config", { address });
  },
  setRuntimeConfig(patch) {
    return ipcRenderer.invoke("protocol:set-runtime-config", patch);
  },
  checkForUpdates(includePrereleases = false, address = "") {
    return ipcRenderer.invoke("protocol:check-for-updates", { includePrereleases, address });
  },
  getUpdateStatus(address = "") {
    return ipcRenderer.invoke("protocol:get-update-status", { address });
  },
  installUpdate(version = "", address = "") {
    return ipcRenderer.invoke("protocol:install-update", { version, address });
  },
  skipUpdate(version = "", address = "") {
    return ipcRenderer.invoke("protocol:skip-update", { version, address });
  },
  getUpdateChangelog(includePrereleases = false, address = "") {
    return ipcRenderer.invoke("protocol:get-update-changelog", { includePrereleases, address });
  },
  setVerbosity(verbosity, address = "") {
    return ipcRenderer.invoke("protocol:set-verbosity", { verbosity, address });
  },
  stopSafe(runId = "", reason = "", address = "") {
    return ipcRenderer.invoke("protocol:stop-safe", { runId, reason, address });
  },
  clearStop(runId = "", address = "") {
    return ipcRenderer.invoke("protocol:clear-stop", { runId, address });
  },
  getTerminalState() {
    return ipcRenderer.invoke("terminal:get-state");
  },
  startTerminal(cwd = "", label = "") {
    return ipcRenderer.invoke("terminal:start", { cwd, label });
  },
  activateTerminalSession(sessionId = "") {
    return ipcRenderer.invoke("terminal:activate", { sessionId });
  },
  sendTerminalInput(input, sessionId = "") {
    return ipcRenderer.invoke("terminal:send", { input, sessionId });
  },
  stopTerminal(sessionId = "") {
    return ipcRenderer.invoke("terminal:stop", { sessionId });
  },
  clearTerminal(sessionId = "") {
    return ipcRenderer.invoke("terminal:clear", { sessionId });
  },
  closeTerminal(sessionId = "") {
    return ipcRenderer.invoke("terminal:close", { sessionId });
  },
  onEvent(callback) {
    return listen("protocol:event", callback);
  },
  onConnectionState(callback) {
    return listen("protocol:connection-state", callback);
  },
  onTerminalState(callback) {
    return listen("terminal:state", callback);
  },
  onTerminalData(callback) {
    return listen("terminal:data", callback);
  },
});
