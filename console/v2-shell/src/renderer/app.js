const shellHelpers = window.OrchestratorShellHelpers;
const bootstrapAddress = window.orchestratorConsole.bootstrap
  && typeof window.orchestratorConsole.bootstrap.defaultControlAddress === "string"
  && window.orchestratorConsole.bootstrap.defaultControlAddress.trim() !== ""
  ? window.orchestratorConsole.bootstrap.defaultControlAddress.trim()
  : "http://127.0.0.1:44777";
const bootstrapExpectedRepoPath = window.orchestratorConsole.bootstrap
  && typeof window.orchestratorConsole.bootstrap.expectedRepoPath === "string"
  ? window.orchestratorConsole.bootstrap.expectedRepoPath.trim()
  : "";
const persistedShellSession = shellHelpers.loadShellSession(window.localStorage, {
  defaultAddress: bootstrapAddress,
});
const defaultAddress = persistedShellSession.address || bootstrapAddress;
const defaultAutofillTargets = [
  ".orchestrator/brief.md",
  ".orchestrator/roadmap.md",
  ".orchestrator/constraints.md",
  ".orchestrator/decisions.md",
  ".orchestrator/goal.md",
  ".orchestrator/human-notes.md",
];
const softRefreshEvents = new Set([
  "run_started",
  "planner_turn_completed",
  "planner_intervention_turn_completed",
  "executor_turn_completed",
  "executor_turn_failed",
  "executor_approval_required",
  "model_health_tested",
  "model_health_failed",
  "verbosity_changed",
  "runtime_config_changed",
  "pending_action_updated",
  "pending_action_cleared",
  "control_message_consumed",
  "safe_point_intervention_pending",
  "run_completed",
  "approval_cleared",
  "side_chat_message_recorded",
  "side_chat_message_answered",
  "dogfood_issue_recorded",
  "contract_autofill_generated",
  "setup_action_completed",
  "snapshot_captured",
  "worker_created",
  "worker_dispatch_completed",
  "worker_removed",
  "worker_integration_completed",
]);

const state = {
  address: persistedShellSession.address || defaultAddress,
  expectedRepoPath: bootstrapExpectedRepoPath,
  connection: {
    connected: false,
    status: "disconnected",
    address: defaultAddress,
    message: "not connected",
    expectedRepoPath: bootstrapExpectedRepoPath,
  },
  snapshot: null,
  artifacts: { count: 0, latest_path: "", items: [], message: "No artifacts yet. Artifacts appear after planner/executor turns complete." },
  artifactContent: null,
  selectedArtifactPath: persistedShellSession.selectedArtifactPath || "",
  repoTree: { path: persistedShellSession.repoTreePath || "", parent_path: "", count: 0, items: [], message: "Repo tree is empty because the shell is disconnected or no repo root is loaded." },
  repoFile: null,
  selectedRepoPath: persistedShellSession.selectedRepoPath || "",
  contractFiles: { count: 0, files: [] },
  selectedContractPath: persistedShellSession.selectedContractPath || "",
  contractOpenFile: null,
  savedGoal: {
    content: "",
    modifiedAt: "",
    exists: false,
    dirty: false,
    status: "not_loaded",
  },
  setupHealth: null,
  setupActionInFlight: "",
  latestSnapshotCapture: null,
  autofill: {
    step: 0,
    answers: {
      project_summary: "",
      desired_outcome: "",
      users_platform: "",
      constraints: "",
      milestones: "",
      decisions: "",
      notes: "",
    },
    targets: [...defaultAutofillTargets],
    result: null,
    selectedDraftPath: "",
  },
  sideChat: { available: true, count: 0, items: [], message: "No side chat messages loaded yet." },
  dogfoodIssues: { available: true, count: 0, items: [], message: "No dogfood issues loaded yet." },
  selectedDogfoodIssueID: persistedShellSession.selectedDogfoodIssueID || "",
  workers: { count: 0, counts_by_status: {}, items: [], message: "No workers exist for the current run yet. Workers appear after the planner creates or dispatches them, or after you create one through this panel." },
  selectedWorkerID: persistedShellSession.selectedWorkerID || "",
  workerActionResult: null,
  terminal: {
    available: true,
    count: 0,
    active_session_id: "",
    active_session: null,
    sessions: [],
    status: "stopped",
    shell_label: "PowerShell",
    command: "",
    args: [],
    pid: null,
    cwd: "",
    buffered_output: "",
    message: "No terminal sessions yet.",
    exit_code: null,
  },
  events: [],
  activityFilters: {
    searchText: persistedShellSession.activityFilters.searchText || "",
    currentRunOnly: Boolean(persistedShellSession.activityFilters.currentRunOnly),
    autoScroll: persistedShellSession.activityFilters.autoScroll !== false,
    categories: { ...persistedShellSession.activityFilters.categories },
  },
  activeTab: persistedShellSession.activeTab || "home",
  localEventSequence: 0,
  lastRefreshedAt: "",
  connectionTiming: {
    connectedAt: "",
    connectingAt: "",
    disconnectedAt: new Date().toISOString(),
  },
  homeError: "",
  preparedCommand: "",
  runLaunch: {
    inFlight: false,
    message: "",
    error: "",
  },
  actionRequired: {
    inFlight: false,
    status: "",
    queuedAskHumanAnswer: null,
  },
  modelTests: {
    planner: null,
    executor: null,
    inFlight: "",
    error: "",
  },
  runtimeConfig: null,
  updateStatus: null,
  reconnect: {
    enabled: persistedShellSession.autoReconnect !== false,
    timer: null,
    attempts: 0,
    pending: false,
    delayMs: 0,
    inFlight: false,
  },
  lastIssue: null,
  refreshTimer: null,
  flash: {
    kind: "info",
    text: persistedShellSession.lastConnected
      ? "Reattach to the loopback control server to resume the current operator view."
      : "Start `orchestrator control serve`, then connect this early protocol shell.",
  },
};

const refs = {};

function initializeRefs() {
  refs.addressInput = document.getElementById("address-input");
  refs.autoReconnect = document.getElementById("auto-reconnect");
  refs.connectButton = document.getElementById("connect-button");
  refs.connectionBadge = document.getElementById("connection-badge");
  refs.connectionDetails = document.getElementById("connection-details");
  refs.issueBox = document.getElementById("issue-box");
  refs.issueBody = document.getElementById("issue-body");
  refs.modelHealthBody = document.getElementById("model-health-body");
  refs.testPlannerModelButton = document.getElementById("test-planner-model");
  refs.testExecutorModelButton = document.getElementById("test-executor-model");
  refs.copyModelHealthButton = document.getElementById("copy-model-health");
  refs.restartBackendButton = document.getElementById("restart-backend");
  refs.runtimeSettingsSummary = document.getElementById("runtime-settings-summary");
  refs.timeoutPreset = document.getElementById("timeout-preset");
  refs.permissionProfile = document.getElementById("permission-profile");
  refs.timeoutPlannerRequest = document.getElementById("timeout-planner-request");
  refs.timeoutExecutorTurn = document.getElementById("timeout-executor-turn");
  refs.timeoutExecutorIdle = document.getElementById("timeout-executor-idle");
  refs.timeoutSubagent = document.getElementById("timeout-subagent");
  refs.timeoutShellCommand = document.getElementById("timeout-shell-command");
  refs.timeoutInstall = document.getElementById("timeout-install");
  refs.timeoutHumanWait = document.getElementById("timeout-human-wait");
  refs.loadRuntimeConfigButton = document.getElementById("load-runtime-config");
  refs.saveRuntimeConfigButton = document.getElementById("save-runtime-config");
  refs.runtimeSettingsStatus = document.getElementById("runtime-settings-status");
  refs.updateStatusBody = document.getElementById("update-status-body");
  refs.checkUpdatesButton = document.getElementById("check-updates");
  refs.copyUpdateChangelogButton = document.getElementById("copy-update-changelog");
  refs.installUpdateButton = document.getElementById("install-update");
  refs.flash = document.getElementById("flash-message");
  refs.topStatusBar = document.getElementById("top-status-bar");
  refs.topRefreshButton = document.getElementById("top-refresh");
  refs.topReconnectButton = document.getElementById("top-reconnect");
  refs.disconnectedBanner = document.getElementById("disconnected-banner");
  refs.homePrimaryAction = document.getElementById("home-primary-action");
  refs.homeRefreshEverything = document.getElementById("home-refresh-everything");
  refs.homeOpenLiveOutput = document.getElementById("home-open-live-output");
  refs.homeCopyDebugBundle = document.getElementById("home-copy-debug-bundle");
  refs.homeRecoverBackend = document.getElementById("home-recover-backend");
  refs.homeClearStopContinue = document.getElementById("home-clear-stop-continue");
  refs.homeSafeStop = document.getElementById("home-safe-stop");
  refs.homeRecommendationTitle = document.getElementById("home-recommendation-title");
  refs.homeRecommendationDetail = document.getElementById("home-recommendation-detail");
  refs.homeRefreshMeta = document.getElementById("home-refresh-meta");
  refs.homeError = document.getElementById("home-error");
  refs.homeErrorBody = document.getElementById("home-error-body");
  refs.homeGoal = document.getElementById("home-goal");
  refs.goalSaveButton = document.getElementById("save-goal");
  refs.goalStatus = document.getElementById("goal-status");
  refs.savedGoalBody = document.getElementById("saved-goal-body");
  refs.projectSystemBody = document.getElementById("project-system-body");
  refs.setupHealthBody = document.getElementById("setup-health-body");
  refs.setupRefreshButton = document.getElementById("setup-refresh");
  refs.useAIGenerateButton = document.getElementById("use-ai-generate");
  refs.auroraGauge = document.getElementById("aurora-gauge");
  refs.auroraProgressLabel = document.getElementById("aurora-progress-label");
  refs.auroraProgressSubtitle = document.getElementById("aurora-progress-subtitle");
  refs.auroraSystemState = document.getElementById("aurora-system-state");
  refs.auroraRepoLabel = document.getElementById("aurora-repo-label");
  refs.auroraBranchLabel = document.getElementById("aurora-branch-label");
  refs.auroraRunId = document.getElementById("aurora-run-id");
  refs.auroraStage = document.getElementById("aurora-stage");
  refs.auroraAction = document.getElementById("aurora-action");
  refs.auroraStatusChips = document.getElementById("aurora-status-chips");
  refs.auroraTimers = document.getElementById("aurora-timers");
  refs.auroraMeta = document.getElementById("aurora-meta");
  refs.auroraTimelineBody = document.getElementById("aurora-timeline-body");
  refs.auroraTimelineFilter = document.getElementById("aurora-timeline-filter");
  refs.auroraEventsAutoScroll = document.getElementById("aurora-events-auto-scroll");
  refs.timelineViewLogsButton = document.getElementById("timeline-view-logs");
  refs.captureSnapshotButton = document.getElementById("capture-snapshot");
  refs.auroraPauseButton = document.getElementById("aurora-pause");
  refs.auroraStopButton = document.getElementById("aurora-stop");
  refs.auroraContinueButton = document.getElementById("aurora-continue");
  refs.auroraInjectNoteButton = document.getElementById("aurora-inject-note");
  refs.auroraViewLogsButton = document.getElementById("aurora-view-logs");
  refs.homeUseDefaultGoal = document.getElementById("home-use-default-goal");
  refs.homeStartRun = document.getElementById("home-start-run");
  refs.homeContinueRun = document.getElementById("home-continue-run");
  refs.homeRunControlNote = document.getElementById("home-run-control-note");
  refs.homePrepareStartCommand = document.getElementById("home-prepare-start-command");
  refs.homePrepareContinueCommand = document.getElementById("home-prepare-continue-command");
  refs.homeCommandPreview = document.getElementById("home-command-preview");
  refs.homeRepoBody = document.getElementById("home-repo-body");
  refs.homeRunBody = document.getElementById("home-run-body");
  refs.homeProgressBody = document.getElementById("home-progress-body");
  refs.homePlannerBody = document.getElementById("home-planner-body");
  refs.homeAttentionBody = document.getElementById("home-attention-body");
  refs.homeCodexBody = document.getElementById("home-codex-body");
  refs.homeArtifactBody = document.getElementById("home-artifact-body");
  refs.homeActivityBody = document.getElementById("home-activity-body");
  refs.homeOpenContracts = document.getElementById("home-open-contracts");
  refs.homeOpenLatestArtifact = document.getElementById("home-open-latest-artifact");
  refs.homeQuickLiveOutput = document.getElementById("home-quick-live-output");
  refs.homeQuickCopyDebugBundle = document.getElementById("home-quick-copy-debug-bundle");
  refs.homeAddDogfoodNote = document.getElementById("home-add-dogfood-note");
  refs.homeOpenTerminal = document.getElementById("home-open-terminal");
  refs.sideNavItems = Array.from(document.querySelectorAll("[data-tab-target]"));
  refs.panes = Array.from(document.querySelectorAll("[data-pane]"));
  refs.attentionBadge = document.getElementById("attention-badge");
  refs.refreshButton = document.getElementById("refresh-status");
  refs.refreshArtifactsButton = document.getElementById("refresh-artifacts");
  refs.safeStopButton = document.getElementById("safe-stop");
  refs.clearStopButton = document.getElementById("clear-stop");
  refs.verbositySelect = document.getElementById("verbosity-select");
  refs.verbosityHelp = document.getElementById("verbosity-help");
  refs.controlMessageInput = document.getElementById("control-message");
  refs.sendControlMessageButton = document.getElementById("send-control-message");
  refs.sideChatMessageInput = document.getElementById("side-chat-message");
  refs.sideChatContextPolicy = document.getElementById("side-chat-context-policy");
  refs.sendSideChatMessageButton = document.getElementById("send-side-chat-message");
  refs.sideChatBody = document.getElementById("side-chat-body");
  refs.sideChatWhatNow = document.getElementById("side-chat-what-now");
  refs.sideChatWhatChanged = document.getElementById("side-chat-what-changed");
  refs.sideChatExplainBlocker = document.getElementById("side-chat-explain-blocker");
  refs.sideChatAskPlannerReconsider = document.getElementById("side-chat-ask-planner-reconsider");
  refs.sideChatSafeStop = document.getElementById("side-chat-safe-stop");
  refs.sideChatCopyConversation = document.getElementById("side-chat-copy-conversation");
  refs.sideChatCopySupport = document.getElementById("side-chat-copy-support");
  refs.dogfoodTitleInput = document.getElementById("dogfood-title");
  refs.dogfoodNoteInput = document.getElementById("dogfood-note");
  refs.captureDogfoodIssueButton = document.getElementById("capture-dogfood-issue");
  refs.dogfoodBody = document.getElementById("dogfood-body");
  refs.dogfoodDetail = document.getElementById("dogfood-detail");
  refs.progressBody = document.getElementById("progress-body");
  refs.statusBody = document.getElementById("status-body");
  refs.pendingBody = document.getElementById("pending-body");
  refs.approvalBody = document.getElementById("approval-body");
  refs.askHumanActionsRow = document.getElementById("ask-human-actions-row");
  refs.askHumanAnswer = document.getElementById("ask-human-answer");
  refs.sendAnswerContinueButton = document.getElementById("send-answer-continue");
  refs.sendAnswerOnlyButton = document.getElementById("send-answer-only");
  refs.continueQueuedAnswerButton = document.getElementById("continue-queued-answer");
  refs.askHumanStatus = document.getElementById("ask-human-status");
  refs.approvalActionsRow = document.getElementById("approval-actions-row");
  refs.approveButton = document.getElementById("approve-executor");
  refs.denyButton = document.getElementById("deny-executor");
  refs.copyApprovalDetailsButton = document.getElementById("copy-approval-details");
  refs.summaryBody = document.getElementById("summary-body");
  refs.copyDebugBundleButton = document.getElementById("copy-debug-bundle");
  refs.copyLatestErrorButton = document.getElementById("copy-latest-error");
  refs.summaryOpenLiveOutputButton = document.getElementById("summary-open-live-output");
  refs.refreshWorkersButton = document.getElementById("refresh-workers");
  refs.workerCreateName = document.getElementById("worker-create-name");
  refs.workerCreateScope = document.getElementById("worker-create-scope");
  refs.workerCreateButton = document.getElementById("worker-create");
  refs.workerDetailBody = document.getElementById("worker-detail-body");
  refs.workerDispatchPrompt = document.getElementById("worker-dispatch-prompt");
  refs.workerDispatchButton = document.getElementById("worker-dispatch");
  refs.workerRemoveButton = document.getElementById("worker-remove");
  refs.workerIntegrateIds = document.getElementById("worker-integrate-ids");
  refs.workerIntegrateButton = document.getElementById("worker-integrate");
  refs.workerActionResult = document.getElementById("worker-action-result");
  refs.workersBody = document.getElementById("workers-body");
  refs.eventsMeta = document.getElementById("events-meta");
  refs.eventsFilterText = document.getElementById("events-filter-text");
  refs.eventsCurrentRunOnly = document.getElementById("events-current-run-only");
  refs.eventsAutoScroll = document.getElementById("events-auto-scroll");
  refs.eventsCategoryFilters = Array.from(document.querySelectorAll("[data-event-category]"));
  refs.eventsBody = document.getElementById("events-body");
  refs.artifactsMeta = document.getElementById("artifacts-meta");
  refs.artifactList = document.getElementById("artifact-list");
  refs.artifactViewer = document.getElementById("artifact-viewer");
  refs.repoTreeMeta = document.getElementById("repo-tree-meta");
  refs.repoTreeList = document.getElementById("repo-tree-list");
  refs.repoFileMeta = document.getElementById("repo-file-meta");
  refs.repoFileViewer = document.getElementById("repo-file-viewer");
  refs.repoRootButton = document.getElementById("repo-root");
  refs.repoUpButton = document.getElementById("repo-up");
  refs.repoRefreshButton = document.getElementById("repo-refresh");
  refs.repoOpenInContractButton = document.getElementById("repo-open-in-contract");
  refs.contractFileList = document.getElementById("contract-file-list");
  refs.contractEditorMeta = document.getElementById("contract-editor-meta");
  refs.contractEditor = document.getElementById("contract-editor");
  refs.openSelectedContractButton = document.getElementById("open-selected-contract");
  refs.saveContractButton = document.getElementById("save-contract");
  refs.autofillStepLabel = document.getElementById("autofill-step-label");
  refs.autofillMeta = document.getElementById("autofill-meta");
  refs.autofillProjectSummary = document.getElementById("autofill-project-summary");
  refs.autofillDesiredOutcome = document.getElementById("autofill-desired-outcome");
  refs.autofillUsersPlatform = document.getElementById("autofill-users-platform");
  refs.autofillConstraints = document.getElementById("autofill-constraints");
  refs.autofillMilestones = document.getElementById("autofill-milestones");
  refs.autofillDecisions = document.getElementById("autofill-decisions");
  refs.autofillNotes = document.getElementById("autofill-notes");
  refs.autofillBackButton = document.getElementById("autofill-back");
  refs.autofillNextButton = document.getElementById("autofill-next");
  refs.autofillRunButton = document.getElementById("run-autofill");
  refs.autofillDraftList = document.getElementById("autofill-file-list");
  refs.autofillPreview = document.getElementById("autofill-preview");
  refs.saveAutofillDraftButton = document.getElementById("save-autofill-draft");
  refs.terminalMeta = document.getElementById("terminal-meta");
  refs.terminalTabs = document.getElementById("terminal-tabs");
  refs.terminalOutput = document.getElementById("terminal-output");
  refs.terminalInput = document.getElementById("terminal-input");
  refs.terminalNewButton = document.getElementById("terminal-new");
  refs.terminalCloseButton = document.getElementById("terminal-close");
  refs.terminalStopButton = document.getElementById("terminal-stop");
  refs.terminalClearButton = document.getElementById("terminal-clear");
  refs.terminalSendButton = document.getElementById("terminal-send");
}

function activeRunID() {
  return state.snapshot && state.snapshot.run ? state.snapshot.run.id || "" : "";
}

function activeRepoPath() {
  return state.snapshot && state.snapshot.runtime ? state.snapshot.runtime.repo_root || "" : "";
}

function viewModelOptions(extra = {}) {
  return {
    ...extra,
    expectedRepoPath: state.expectedRepoPath,
  };
}

function currentRepoBinding() {
  return window.OrchestratorViewModel.buildRepoBindingViewModel(state.snapshot, viewModelOptions({
    connection: state.connection,
  }));
}

function latestArtifactPath() {
  if (state.artifacts && typeof state.artifacts.latest_path === "string" && state.artifacts.latest_path.trim() !== "") {
    return state.artifacts.latest_path.trim();
  }
  if (state.snapshot && state.snapshot.artifacts && typeof state.snapshot.artifacts.latest_path === "string" && state.snapshot.artifacts.latest_path.trim() !== "") {
    return state.snapshot.artifacts.latest_path.trim();
  }
  const items = state.artifacts && Array.isArray(state.artifacts.items) ? state.artifacts.items : [];
  const latest = items.find((item) => item.latest) || items[0];
  return latest && latest.path ? latest.path : "";
}

function applyModelHealthNormalization() {
  if (!state.snapshot) {
    return;
  }
  state.snapshot = window.OrchestratorViewModel.normalizeModelHealthSnapshot(state.snapshot, state.modelTests);
}

function setActiveTab(tabName, options = {}) {
  let targetTab = String(tabName || "home").trim() || "home";
  if (!refs.panes.some((pane) => pane.getAttribute("data-pane") === targetTab)) {
    targetTab = "home";
  }
  state.activeTab = targetTab;
  refs.sideNavItems.forEach((item) => {
    item.classList.toggle("active", item.getAttribute("data-tab-target") === targetTab);
  });
  refs.panes.forEach((pane) => {
    pane.hidden = pane.getAttribute("data-pane") !== targetTab;
  });
  persistShellSession();

  if (!options.noScroll) {
    const firstPane = refs.panes.find((pane) => pane.getAttribute("data-pane") === targetTab);
    if (firstPane) {
      firstPane.scrollIntoView({ behavior: options.instant ? "auto" : "smooth", block: "start" });
    }
  }
}

function tabForSection(targetID) {
  const element = document.getElementById(targetID);
  if (!element) {
    return "home";
  }
  return element.getAttribute("data-pane") || (element.closest("[data-pane]") && element.closest("[data-pane]").getAttribute("data-pane")) || "home";
}

function scrollToSection(targetID) {
  setActiveTab(tabForSection(targetID), { noScroll: true });
  const element = document.getElementById(targetID);
  if (element) {
    element.scrollIntoView({ behavior: "smooth", block: "start" });
  }
}

function quotePowerShellArgument(value) {
  return `"${String(value || "").replaceAll("`", "``").replaceAll('"', '`"')}"`;
}

function suggestedDefaultGoal() {
  const runGoal = state.snapshot && state.snapshot.run ? String(state.snapshot.run.goal || "").trim() : "";
  if (runGoal !== "") {
    return runGoal;
  }
  return "Build the next highest-value step from this repo's contract files.";
}

function buildStartRunCommand() {
  const goal = refs.homeGoal && refs.homeGoal.value.trim() !== ""
    ? refs.homeGoal.value.trim()
    : suggestedDefaultGoal();
  return `orchestrator run --goal ${quotePowerShellArgument(goal)}`;
}

async function prepareTerminalCommand(command) {
  const normalized = String(command || "").trim();
  if (normalized === "") {
    return;
  }
  state.preparedCommand = normalized;
  if (refs.terminalInput) {
    refs.terminalInput.value = normalized;
  }
  try {
    if (navigator.clipboard) {
      await navigator.clipboard.writeText(normalized);
    }
  } catch (_error) {
    // The command remains visible in the terminal input even if clipboard access is unavailable.
  }
  renderHomeDashboard();
  scrollToSection("terminal-panel");
  setFlash("info", "Backup command copied when clipboard access was available and prepared in the Operator Terminal input.");
}

async function copyText(text, successMessage) {
  try {
    await navigator.clipboard.writeText(String(text || ""));
    setFlash("success", successMessage || "Copied.");
  } catch (error) {
    reportIssue("clipboard", error, "Copying requires clipboard access.");
  }
}

function currentRepoTreePath() {
  return state.repoTree && typeof state.repoTree.path === "string" ? state.repoTree.path : "";
}

function activeTerminalSessionID() {
  return state.terminal && typeof state.terminal.active_session_id === "string"
    ? state.terminal.active_session_id
    : "";
}

function selectedAutofillDraft() {
  if (!state.autofill.result || !Array.isArray(state.autofill.result.files)) {
    return null;
  }
  return state.autofill.result.files.find((file) => file.path === state.autofill.selectedDraftPath) || null;
}

function ensureSelectedWorker() {
  const items = state.workers && Array.isArray(state.workers.items) ? state.workers.items : [];
  if (items.length === 0) {
    state.selectedWorkerID = "";
    return;
  }
  if (!items.some((item) => item.worker_id === state.selectedWorkerID)) {
    state.selectedWorkerID = items[0].worker_id || "";
  }
  if (!refs.workerIntegrateIds || refs.workerIntegrateIds.value.trim() !== "") {
    return;
  }
  if (state.selectedWorkerID) {
    refs.workerIntegrateIds.value = state.selectedWorkerID;
  }
}

function ensureSelectedDogfoodIssue() {
  const items = state.dogfoodIssues && Array.isArray(state.dogfoodIssues.items) ? state.dogfoodIssues.items : [];
  if (items.length === 0) {
    state.selectedDogfoodIssueID = "";
    return;
  }
  if (!items.some((item) => item.id === state.selectedDogfoodIssueID)) {
    state.selectedDogfoodIssueID = items[0].id || "";
  }
}

function parseWorkerIntegrateIDs(value) {
  return String(value || "")
    .split(",")
    .map((item) => item.trim())
    .filter(Boolean);
}

function autofillStepTitle(step) {
  switch (step) {
    case 0:
      return "Step 1 of 4: project summary";
    case 1:
      return "Step 2 of 4: desired outcome";
    case 2:
      return "Step 3 of 4: constraints and milestones";
    case 3:
      return "Step 4 of 4: decisions, notes, and target files";
    default:
      return "Step 1 of 4: project summary";
  }
}

function rememberActivityFiltersFromUI() {
  if (refs.eventsFilterText) {
    state.activityFilters.searchText = refs.eventsFilterText.value || "";
  }
  if (refs.eventsCurrentRunOnly) {
    state.activityFilters.currentRunOnly = refs.eventsCurrentRunOnly.checked;
  }
  if (refs.eventsAutoScroll) {
    state.activityFilters.autoScroll = refs.eventsAutoScroll.checked;
  }
  if (Array.isArray(refs.eventsCategoryFilters)) {
    refs.eventsCategoryFilters.forEach((element) => {
      const category = element.getAttribute("data-event-category");
      if (category) {
        state.activityFilters.categories[category] = element.checked;
      }
    });
  }
  persistShellSession();
}

function makeLocalActivity(eventName, payload = {}, summary = "") {
  state.localEventSequence += 1;
  return {
    type: "event",
    event: eventName,
    sequence: `local-${state.localEventSequence}`,
    at: new Date().toISOString(),
    payload,
    summary,
    local: true,
  };
}

function pushActivityEvent(event) {
  state.events = [event, ...state.events].slice(0, 120);
  renderHomeDashboard();
  renderAuroraDashboard();
  renderEvents();
  renderRunSummary();
}

function recordLocalActivity(eventName, payload = {}, summary = "") {
  pushActivityEvent(makeLocalActivity(eventName, payload, summary));
}

function persistShellSession() {
  const normalized = shellHelpers.saveShellSession(window.localStorage, {
    address: state.address,
    autoReconnect: state.reconnect.enabled,
    lastConnected: state.connection.connected,
    verbosity: refs.verbositySelect ? refs.verbositySelect.value : "normal",
    sideChatContextPolicy: refs.sideChatContextPolicy ? refs.sideChatContextPolicy.value : "repo_and_latest_run_summary",
    activeTab: state.activeTab,
    selectedArtifactPath: state.selectedArtifactPath,
    selectedContractPath: state.selectedContractPath,
    selectedRepoPath: state.selectedRepoPath,
    repoTreePath: currentRepoTreePath(),
    selectedWorkerID: state.selectedWorkerID,
    selectedDogfoodIssueID: state.selectedDogfoodIssueID,
    activityFilters: state.activityFilters,
  });
  state.address = normalized.address;
}

function clearReconnectTimer() {
  if (state.reconnect.timer) {
    window.clearTimeout(state.reconnect.timer);
    state.reconnect.timer = null;
  }
  state.reconnect.pending = false;
  state.reconnect.delayMs = 0;
}

function reportIssue(scope, error, hint = "") {
  state.lastIssue = shellHelpers.formatProtocolError(scope, error, hint);
  state.homeError = state.lastIssue.message;
  renderIssue();
  renderHomeDashboard();
  setFlash("error", state.lastIssue.message);
}

function isRepoContractReadinessError(error) {
  return /target repo contract is not ready/i.test(error && error.message ? error.message : "");
}

function friendlyRepoContractReadinessError(error) {
  const message = error && error.message ? error.message : "";
  const missingMatch = message.match(/missing\s+(.+?)(?:;|$)/i);
  const missing = missingMatch ? missingMatch[1].trim() : "";
  return `Target repo contract is not ready${missing ? `; missing ${missing}` : ""}. Run orchestrator init from the target repo, then refresh the dashboard and retry.`;
}

function displayErrorWithMessage(error, message) {
  const displayError = new Error(message);
  displayError.code = error && error.code;
  displayError.status = error && error.status;
  return displayError;
}

function clearIssue() {
  state.lastIssue = null;
  state.homeError = "";
  renderIssue();
  renderHomeDashboard();
}

function scheduleReconnect(trigger = "connection_lost") {
  if (!state.reconnect.enabled || state.reconnect.timer || state.reconnect.inFlight || state.connection.connected) {
    return;
  }
  state.reconnect.attempts += 1;
  state.reconnect.delayMs = shellHelpers.nextReconnectDelay(state.reconnect.attempts);
  state.reconnect.pending = true;
  renderConnection();

  state.reconnect.timer = window.setTimeout(() => {
    clearReconnectTimer();
    void connect({
      quiet: true,
      automatic: true,
      trigger,
    });
  }, state.reconnect.delayMs);
}

function setFlash(kind, text) {
  state.flash = { kind, text };
  renderFlash();
}

function elapsedSecondsSince(value) {
  const date = new Date(value || "");
  if (Number.isNaN(date.getTime())) {
    return 0;
  }
  return Math.max(0, Math.floor((Date.now() - date.getTime()) / 1000));
}

function latestActivityEvent() {
  return Array.isArray(state.events) && state.events.length > 0 ? state.events[0] : null;
}

function renderFlash() {
  refs.flash.className = `flash flash-${state.flash.kind}`;
  refs.flash.textContent = state.flash.text;
}

function renderIssue() {
  if (!state.lastIssue) {
    refs.issueBox.hidden = true;
    refs.issueBody.textContent = "";
    return;
  }

  refs.issueBox.hidden = false;
  refs.issueBody.textContent = `${state.lastIssue.message}\n${new Date(state.lastIssue.at).toLocaleString()}`;
}

function homeRow(label, value) {
  return `<div class="home-kv"><span>${escapeHTML(label)}</span><strong title="${escapeHTML(value)}">${escapeHTML(value)}</strong></div>`;
}

const projectFilePurposes = {
  ".orchestrator/brief.md": "Project brief and desired outcome.",
  ".orchestrator/roadmap.md": "Build plan and milestones.",
  ".orchestrator/constraints.md": "Technical and business guardrails.",
  ".orchestrator/decisions.md": "Stable decisions and rationale.",
  ".orchestrator/human-notes.md": "Extra user context and notes.",
  ".orchestrator/goal.md": "Current run/build objective.",
  "AGENTS.md": "Repo instructions for AI agents.",
};

const auroraTimelineCategorySets = {
  all: {},
  planner: { planner: true },
  executor: { executor: true, worker: true },
  human: { human: true, intervention: true, approval: true },
  files: { files: true },
  tests: { tests: true },
  system: { system: true, status: true, fault: true, terminal: true, setup: true, other: true },
};

function shortPathName(path) {
  const parts = String(path || "").split(/[\\/]/).filter(Boolean);
  return parts.length ? parts[parts.length - 1] : String(path || "");
}

function latestBranchLabel(snapshot) {
  const runtime = snapshot && snapshot.runtime ? snapshot.runtime : {};
  const run = snapshot && snapshot.run ? snapshot.run : {};
  return runtime.git_branch || runtime.current_branch || run.git_branch || "Unavailable";
}

function runStateLabelForAurora(loopVM) {
  switch (loopVM.state) {
    case "running":
      return "Active Run";
    case "attention":
      return "Human Input";
    case "completed":
      return "Complete";
    case "stopped":
      return "Paused";
    case "idle":
      return state.connection.connected ? "Waiting" : "Offline";
    default:
      return state.connection.connected ? "System Online" : "Offline";
  }
}

function chipHTML(label, active, tone = "neutral") {
  return `<span class="mission-chip mission-chip-${escapeHTML(tone)} ${active ? "active" : ""}">${escapeHTML(label)}</span>`;
}

function timelineCategoriesForAurora() {
  const selected = refs.auroraTimelineFilter ? refs.auroraTimelineFilter.value : "all";
  const selectedSet = auroraTimelineCategorySets[selected] || {};
  if (selected === "all") {
    return {};
  }
  const categories = {};
  ["planner", "executor", "worker", "approval", "intervention", "human", "files", "tests", "system", "setup", "fault", "terminal", "status", "other"].forEach((category) => {
    categories[category] = Boolean(selectedSet[category]);
  });
  return categories;
}

function renderProgressDetailSection(section) {
  const openAttribute = section.open ? " open" : "";
  const longLabel = section.isLong ? "Long text" : "Summary";
  return `
    <details class="progress-detail-section"${openAttribute}>
      <summary>
        <span>${escapeHTML(section.label)}</span>
        <small>${escapeHTML(longLabel)}</small>
      </summary>
      <div class="progress-detail-body">${escapeHTML(section.value)}</div>
    </details>
  `;
}

function renderTopStatus() {
  if (!refs.topStatusBar) {
    return;
  }
  const vm = window.OrchestratorViewModel.buildTopStatusViewModel(state.snapshot, viewModelOptions({
    connection: state.connection,
    address: state.address,
    reconnecting: state.reconnect.pending || state.connection.status === "connecting",
    lastRefreshedAt: state.lastRefreshedAt,
    connectionElapsedSeconds: elapsedSecondsSince(
      state.connection.connected
        ? state.connectionTiming.connectedAt
        : (state.connection.status === "connecting" ? state.connectionTiming.connectingAt : state.connectionTiming.disconnectedAt),
    ),
    launching: state.runLaunch.inFlight,
    latestEvent: latestActivityEvent(),
    lastUpdateLabel: state.lastRefreshedAt ? new Date(state.lastRefreshedAt).toLocaleTimeString() : "",
  }));

  refs.topStatusBar.innerHTML = `
    <div class="top-status-item top-status-hero ${escapeHTML(vm.connectionClass)}"><span>${escapeHTML(vm.connectionLabel)}</span><strong>${escapeHTML(vm.connectionDurationLabel)}</strong><small>${escapeHTML(vm.connectionDetail)}</small></div>
    <div class="top-status-item top-status-hero ${escapeHTML(vm.loopClass)}"><span>${escapeHTML(vm.loopLabel)}</span><strong>${escapeHTML(vm.loopDetail)}</strong><small>${escapeHTML(vm.loopStage)} | ${escapeHTML(vm.loopTurn)} | updated ${escapeHTML(vm.loopLastUpdate)}</small></div>
    <div class="top-status-item"><span>Engine Address</span><strong title="${escapeHTML(vm.address)}">${escapeHTML(vm.address)}</strong></div>
    <div class="top-status-item top-status-wide"><span>Repo</span><strong title="${escapeHTML(vm.repoRoot)}">${escapeHTML(vm.repoRoot)}</strong></div>
    <div class="top-status-item"><span>Run</span><strong title="${escapeHTML(vm.runID)}">${escapeHTML(vm.runID)}</strong></div>
    <div class="top-status-item top-status-wide"><span>Stop / Blocker</span><strong title="${escapeHTML(vm.blocker)}">${escapeHTML(vm.blocker)}</strong></div>
    <div class="top-status-item"><span>Verbosity</span><strong>${escapeHTML(vm.verbosity)}</strong></div>
  `;
  updateStickyLayoutOffset();
}

function updateStickyLayoutOffset() {
  const strip = refs.topStatusBar && refs.topStatusBar.closest(".top-status-strip");
  if (!strip || !document.documentElement) {
    return;
  }
  const height = Math.max(96, Math.ceil(strip.getBoundingClientRect().height));
  document.documentElement.style.setProperty("--sticky-top-offset", `${height}px`);
}

function renderDisconnectedBanner() {
  if (!refs.disconnectedBanner) {
    return;
  }
  refs.disconnectedBanner.hidden = Boolean(state.connection.connected);
}

function renderAttentionBadge() {
  if (!refs.attentionBadge) {
    return;
  }
  const vm = window.OrchestratorViewModel.buildApprovalViewModel(state.snapshot);
  const modelBlocking = Boolean(state.snapshot && state.snapshot.model_health && state.snapshot.model_health.blocking);
  const count = (vm.badgeCount || 0) + (modelBlocking ? 1 : 0);
  refs.attentionBadge.textContent = String(count);
  refs.attentionBadge.hidden = count === 0;
}

function renderVerbosityHelp() {
  if (!refs.verbosityHelp || !refs.verbositySelect) {
    return;
  }
  const vm = window.OrchestratorViewModel.buildVerbosityViewModel(refs.verbositySelect.value);
  refs.verbosityHelp.textContent = `${vm.label}: ${vm.description} This controls what appears in Live Output. Changes apply immediately through the engine protocol.`;
}

function maybeFocusActionRequired(options = {}) {
  const approval = window.OrchestratorViewModel.buildApprovalViewModel(state.snapshot);
  const modelBlocking = Boolean(state.snapshot && state.snapshot.model_health && state.snapshot.model_health.blocking);
  if (!approval.needsAttention && !modelBlocking) {
    return;
  }
  if (options.force || state.activeTab === "home" || state.activeTab === "run") {
    setActiveTab("attention", { noScroll: false });
  }
}

function renderHomeDashboard() {
  if (!refs.homeRecommendationTitle) {
    return;
  }
  const vm = window.OrchestratorViewModel.buildHomeDashboardViewModel(state.snapshot, viewModelOptions({
    connection: state.connection,
    address: state.address,
    reconnecting: state.reconnect.pending || state.connection.status === "connecting",
    artifacts: state.artifacts,
    contractFiles: state.contractFiles,
    events: state.events,
    lastRefreshedAt: state.lastRefreshedAt ? new Date(state.lastRefreshedAt).toLocaleString() : "",
    homeError: state.homeError,
    preparedCommand: state.preparedCommand,
    goalEntered: refs.homeGoal && refs.homeGoal.value.trim() !== "",
  }));
  const action = vm.recommendation.primaryAction || {};
  const progressLabel = vm.progress.progressPercent === null ? "Unavailable" : `${vm.progress.progressPercent}%`;
  const artifactUnavailable = vm.latestArtifactPath === "Unavailable" || vm.latestArtifactPath === "";
  const codex = vm.codex;
  const modelBlocking = Boolean(state.snapshot && state.snapshot.model_health && state.snapshot.model_health.blocking);
  const attentionText = vm.approval.needsAttention
    ? vm.approval.summary
    : (modelBlocking
      ? "Model or Codex configuration is blocking autonomous work. Open Settings and run the model checks."
      : (vm.pending.present ? `Pending: ${vm.pending.summary}` : "No approval or held pending action needs attention."));
  const contractRows = vm.contractStatus.loaded
    ? vm.contractStatus.files
      .map((file) => `<button class="contract-pill ${file.exists ? "contract-pill-ready" : "contract-pill-missing"}" data-home-contract="${escapeHTML(file.path)}">${escapeHTML(file.path)} - ${file.exists ? "found" : "missing"}</button>`)
      .join("")
    : "";

  refs.homeRecommendationTitle.textContent = vm.recommendation.title;
  refs.homeRecommendationDetail.textContent = vm.recommendation.detail;
  refs.homeRefreshMeta.textContent = `Last refreshed: ${vm.refreshedLabel}`;
  const startGoal = refs.homeGoal ? refs.homeGoal.value.trim() : "";
  const currentRun = state.snapshot && state.snapshot.run ? state.snapshot.run : null;
  const runControl = window.OrchestratorViewModel.buildRunControlStateViewModel(state.snapshot, viewModelOptions({
    connection: state.connection,
    connected: state.connection.connected,
    goalEntered: startGoal !== "",
    launchInFlight: state.runLaunch.inFlight,
    modelHealthChecking: Boolean(state.modelTests.inFlight),
  }));
  const canStartRun = runControl.startEnabled;
  const canContinueRun = runControl.continueEnabled;
  const actionBlockedByRunLaunch = (action.id === "start_run" && !canStartRun)
    || (action.id === "continue_run" && !canContinueRun);

  refs.homePrimaryAction.textContent = state.runLaunch.inFlight ? "Launching run..." : (action.label || "Update Dashboard");
  refs.homePrimaryAction.disabled = action.enabled === false || state.runLaunch.inFlight || actionBlockedByRunLaunch;
  refs.homePrimaryAction.dataset.action = action.id || "refresh_status";
  refs.homeStartRun.textContent = runControl.startLabel || "Start Build";
  refs.homeContinueRun.textContent = runControl.continueLabel || "Continue Build";
  refs.homeStartRun.disabled = !canStartRun;
  refs.homeStartRun.title = runControl.startDisabledReason;
  refs.homeContinueRun.disabled = !canContinueRun;
  refs.homeContinueRun.title = runControl.continueDisabledReason;
  refs.homeRunControlNote.textContent = runControl.note;
  refs.homeCommandPreview.textContent = [
    state.runLaunch.inFlight ? "Protocol run action is being submitted..." : "",
    state.runLaunch.message,
    state.runLaunch.error ? `Error: ${state.runLaunch.error}` : "",
    state.preparedCommand ? `Terminal backup prepared: ${state.preparedCommand}` : "Terminal backup: no command prepared.",
  ].filter(Boolean).join("\n");
  refs.homeOpenLatestArtifact.disabled = artifactUnavailable;
  refs.homeCopyDebugBundle.disabled = !state.snapshot;
  refs.homeQuickCopyDebugBundle.disabled = !state.snapshot;
  if (refs.homeClearStopContinue) {
    refs.homeClearStopContinue.hidden = !(vm.approval.safeStop && vm.approval.safeStop.present);
    refs.homeClearStopContinue.disabled = state.runLaunch.inFlight;
  }

  const visibleHomeError = vm.repo && vm.repo.mismatch ? vm.repo.message : vm.homeError;
  if (visibleHomeError) {
    refs.homeError.hidden = false;
    refs.homeErrorBody.textContent = visibleHomeError;
  } else {
    refs.homeError.hidden = true;
    refs.homeErrorBody.textContent = "";
  }

  refs.homeRepoBody.innerHTML = [
    vm.repo.expected ? homeRow("Expected Repo", vm.repo.expected) : "",
    homeRow("Repo Root", vm.repo.root),
    vm.repo.expected ? homeRow("Repo Match", vm.repo.matches ? "yes" : "no") : "",
    homeRow("Contract Status", vm.repo.message),
    `<div class="contract-pill-row">${contractRows || `<span class="panel-note">${escapeHTML(vm.contractStatus.message)}</span>`}</div>`,
  ].join("");

  refs.homeRunBody.innerHTML = [
    homeRow("Loop State", vm.topStatus.loopLabel),
    homeRow("What It Means", vm.topStatus.loopDetail),
    homeRow("Run ID", vm.status.runID),
    homeRow("Goal", vm.status.goal),
    homeRow("Run State", vm.status.completed ? "completed" : vm.status.stopReason),
    homeRow("Checkpoint", `${vm.status.checkpointStage} / ${vm.status.checkpointLabel} / safe_pause=${vm.status.checkpointSafePause ? "true" : "false"}`),
    homeRow("Planner Outcome", vm.status.latestPlannerOutcome),
    homeRow("Executor Status", vm.status.executorTurnStatus),
    homeRow("Next Operator Action", vm.status.nextOperatorAction),
    homeRow("Total Build Time", state.snapshot && state.snapshot.build_time ? state.snapshot.build_time.total_build_time_label || "Unavailable" : "Unavailable"),
    homeRow("Current Run Time", state.snapshot && state.snapshot.build_time ? state.snapshot.build_time.current_run_time_label || "Unavailable" : "Unavailable"),
    homeRow("Current Step", state.snapshot && state.snapshot.build_time ? state.snapshot.build_time.current_step_label || "Unavailable" : "Unavailable"),
    homeRow("Current Step Time", state.snapshot && state.snapshot.build_time ? state.snapshot.build_time.current_step_time_label || "Unavailable" : "Unavailable"),
    homeRow("Run Elapsed", vm.status.elapsedLabel),
    homeRow("What Happened", vm.whatHappened.stop.title),
    homeRow("Next Action", vm.whatHappened.stop.nextAction),
    homeRow("Pending Held", vm.status.pendingHeld ? "true" : "false"),
    vm.status.executorLastError !== "None" ? homeRow("Executor Error", vm.status.executorLastError) : "",
  ].join("");

  refs.homeProgressBody.innerHTML = `
    <div class="home-progress-meter">
      <div class="progress-bar-track"><div class="progress-bar-fill" style="width:${escapeHTML(vm.progress.progressBarWidth)}"></div></div>
      <strong>${escapeHTML(progressLabel)}</strong>
      <span>${escapeHTML(vm.progress.progressConfidence)} confidence</span>
    </div>
    <p>${escapeHTML(vm.progress.progressBasisPreview)}</p>
    ${homeRow("Current Focus", vm.progress.currentFocusPreview)}
    ${homeRow("Next Step", vm.progress.nextIntendedStepPreview)}
  `;

  refs.homePlannerBody.innerHTML = `
    <p class="home-large-text">${escapeHTML(vm.latestPlannerMessage)}</p>
    ${homeRow("Why This Step", vm.progress.whyThisStepPreview)}
  `;

  refs.homeAttentionBody.innerHTML = [
    homeRow("Action Required", vm.approval.needsAttention ? vm.approval.summary : "No action required right now."),
    homeRow("Pending Action", vm.pending.present ? vm.pending.summary : vm.emptyStates.noPendingAction),
    `<p class="panel-note">${escapeHTML(attentionText)}</p>`,
  ].join("");

  refs.homeCodexBody.innerHTML = [
    homeRow("Status", codex.title),
    homeRow("Verification", codex.verificationState),
    homeRow("Full Access Ready", codex.fullAccessReady),
    homeRow("Model / Effort", `${codex.model} / ${codex.effort}`),
    homeRow("Codex Binary", codex.codexPath),
    homeRow("Codex Version", codex.codexVersion),
    codex.lastError !== "None" ? homeRow("Last Error", codex.lastError) : "",
    `<p class="panel-note">${escapeHTML(codex.recommendedAction)}</p>`,
  ].join("");

  refs.homeArtifactBody.innerHTML = artifactUnavailable
    ? `<p class="panel-note">${escapeHTML(vm.emptyStates.noArtifacts)}</p>`
    : [
      homeRow("Latest", vm.latestArtifactPath),
      `<button class="button" data-home-open-artifact="${escapeHTML(vm.latestArtifactPath)}">Open Latest Artifact</button>`,
    ].join("");

  refs.homeActivityBody.innerHTML = vm.recentActivity.length === 0 && !vm.liveOutput.latestError.present
    ? `<div class="event-empty">No recent activity yet. Open Live Output after starting or continuing a run; planner/Codex/model errors appear there as soon as events arrive.</div>`
    : [
      vm.liveOutput.latestError.present ? `
        <div class="pinned-error">
          <strong>Latest error</strong>
          <span>${escapeHTML(vm.liveOutput.latestError.summary)}</span>
        </div>
      ` : "",
      ...(vm.recentActivity.length === 0 ? [`<div class="event-empty">No recent activity events are loaded yet, but the latest status snapshot includes the error above.</div>`] : vm.recentActivity.map((event) => `
      <div class="home-activity-item">
        <div class="event-chip event-chip-${escapeHTML(event.category)}">${escapeHTML(event.categoryLabel)}</div>
        <strong>${escapeHTML(event.summary)}</strong>
        <span>${escapeHTML(event.timestampLabel)}</span>
      </div>
    `)),
      `<p class="panel-note">${escapeHTML(vm.liveOutput.detail)}</p>`,
    ].join("");
}

function renderAuroraDashboard() {
  if (!refs.auroraGauge) {
    return;
  }

  const loopVM = window.OrchestratorViewModel.buildLoopStatusViewModel(state.snapshot, {
    connection: state.connection,
    latestEvent: latestActivityEvent(),
    launching: state.runLaunch.inFlight,
    lastUpdateLabel: state.lastRefreshedAt ? new Date(state.lastRefreshedAt).toLocaleTimeString() : "",
  });
  const statusVM = window.OrchestratorViewModel.buildStatusViewModel(state.snapshot);
  const progressVM = window.OrchestratorViewModel.buildProgressPanelViewModel(state.snapshot);
  const pendingVM = window.OrchestratorViewModel.buildPendingActionViewModel(state.snapshot);
  const approvalVM = window.OrchestratorViewModel.buildApprovalViewModel(state.snapshot);
  const run = state.snapshot && state.snapshot.run ? state.snapshot.run : {};
  const runtime = state.snapshot && state.snapshot.runtime ? state.snapshot.runtime : {};
  const buildTime = state.snapshot && state.snapshot.build_time ? state.snapshot.build_time : {};
  const progressKnown = progressVM.progressPercent !== null;
  const progressValue = progressKnown ? progressVM.progressPercent : 0;
  const actionLabel = state.runLaunch.inFlight
    ? "Submitting explicit run action"
    : (pendingVM.present ? pendingVM.summary : (statusVM.nextOperatorAction || loopVM.detail || "Waiting for explicit operator action"));
  const currentStage = statusVM.checkpointStage !== "Unavailable"
    ? statusVM.checkpointStage
    : (run.activity_state || loopVM.state || "waiting");

  refs.auroraGauge.style.setProperty("--gauge-progress", String(progressValue));
  refs.auroraProgressLabel.textContent = progressKnown ? `${progressValue}%` : "Phase";
  refs.auroraProgressSubtitle.textContent = progressKnown
    ? `${progressVM.progressConfidence} confidence from planner status`
    : `${loopVM.label}: ${loopVM.detail}`;
  refs.auroraSystemState.textContent = runStateLabelForAurora(loopVM);
  refs.auroraRepoLabel.textContent = shortPathName(runtime.repo_root || activeRepoPath() || "No repo loaded");
  refs.auroraBranchLabel.textContent = latestBranchLabel(state.snapshot);
  refs.auroraRunId.textContent = statusVM.runID || "No active run";
  refs.auroraStage.textContent = currentStage;
  refs.auroraAction.textContent = actionLabel;

  const executorActive = ["executor", "codex"].some((word) => String(currentStage).toLowerCase().includes(word))
    || String(run.activity_state || "").toLowerCase().includes("executor");
  const plannerActive = String(currentStage).toLowerCase().includes("planner")
    || String(statusVM.latestPlannerOutcome || "").toLowerCase() !== "unavailable";
  refs.auroraStatusChips.innerHTML = [
    chipHTML("Planner", plannerActive && !executorActive, "planner"),
    chipHTML("Executor / Codex", executorActive, "executor"),
    chipHTML("Waiting", loopVM.state === "idle" || loopVM.state === "stopped", "waiting"),
    chipHTML("Human Input", approvalVM.needsAttention || statusVM.stopReason === "planner_ask_human", "human"),
    chipHTML("Paused", loopVM.state === "stopped", "paused"),
    chipHTML("Complete", loopVM.state === "completed" || statusVM.completed, "complete"),
  ].join("");

  refs.auroraTimers.innerHTML = [
    homeRow("Total Build Time", buildTime.total_build_time_label || "Unavailable"),
    homeRow("Current Step Time", buildTime.current_step_time_label || "Unavailable"),
    homeRow("Current Step", buildTime.current_step_label || currentStage),
  ].join("");

  refs.auroraMeta.innerHTML = [
    homeRow("Cycle", String(run.cycle || run.cycle_number || statusVM.cycle || "Unavailable")),
    homeRow("Latest Checkpoint", `${statusVM.checkpointStage} / ${statusVM.checkpointLabel}`),
    homeRow("Stop Reason", statusVM.stopReason || "None"),
    homeRow("Planner Outcome", statusVM.latestPlannerOutcome || "Unavailable"),
    homeRow("Executor Status", statusVM.executorTurnStatus || "Unavailable"),
  ].join("");

  renderProjectSystem();
  renderSetupHealth();
  renderSavedGoal();
  renderAuroraTimeline();
}

function renderProjectSystem() {
  if (!refs.projectSystemBody) {
    return;
  }
  const files = state.contractFiles && Array.isArray(state.contractFiles.files) ? state.contractFiles.files : [];
  const preferred = [
    ".orchestrator/brief.md",
    ".orchestrator/roadmap.md",
    ".orchestrator/constraints.md",
    ".orchestrator/decisions.md",
    ".orchestrator/human-notes.md",
    ".orchestrator/goal.md",
  ];
  const byPath = new Map(files.map((file) => [file.path, file]));
  const cards = preferred
    .filter((path) => byPath.has(path) || path !== ".orchestrator/human-notes.md")
    .map((path) => {
      const file = byPath.get(path) || { path, exists: false, modified_at: "" };
      const status = file.exists ? "saved" : "missing";
      return `
        <button class="project-file-card ${file.exists ? "is-saved" : "is-missing"}" data-project-file="${escapeHTML(path)}">
          <span class="project-file-name">${escapeHTML(shortPathName(path))}</span>
          <span class="project-file-purpose">${escapeHTML(projectFilePurposes[path] || "Project system file.")}</span>
          <span class="project-file-meta">${escapeHTML(status)}${file.modified_at ? ` | ${escapeHTML(file.modified_at)}` : ""}</span>
        </button>
      `;
    });
  refs.projectSystemBody.innerHTML = cards.length === 0
    ? `<div class="event-empty">Project files have not been loaded yet. Connect or refresh setup checks.</div>`
    : cards.join("");
}

function renderSavedGoal() {
  if (!refs.savedGoalBody || !refs.goalStatus) {
    return;
  }
  const statusText = state.savedGoal.dirty
    ? "Unsaved edits"
    : state.savedGoal.exists
      ? "Goal saved"
      : "No saved goal";
  refs.goalStatus.textContent = statusText;
  refs.goalStatus.className = `goal-status ${state.savedGoal.dirty ? "is-dirty" : state.savedGoal.exists ? "is-saved" : "is-missing"}`;
  refs.savedGoalBody.innerHTML = state.savedGoal.exists
    ? `<div class="saved-goal-card"><strong>Saved Goal</strong><p>${escapeHTML(state.savedGoal.content || "Saved goal file is empty.")}</p><span>${escapeHTML(state.savedGoal.modifiedAt || "Last updated unavailable")}</span></div>`
    : `<div class="saved-goal-card empty"><strong>No saved goal yet</strong><p>Write or generate a goal, then save it explicitly before starting a run.</p></div>`;
}

function renderSetupHealth() {
  if (!refs.setupHealthBody) {
    return;
  }
  const checks = state.setupHealth && Array.isArray(state.setupHealth.checks) ? state.setupHealth.checks : [];
  if (checks.length === 0) {
    refs.setupHealthBody.innerHTML = `<div class="event-empty">Setup checks have not run yet. Connect to the control server or click Refresh Checks.</div>`;
    return;
  }
  refs.setupHealthBody.innerHTML = checks.map((check) => {
    const action = check.action ? `<button class="button button-mini" data-setup-action="${escapeHTML(check.action)}">${escapeHTML(check.action_label || "Fix")}</button>` : "";
    return `
      <div class="setup-check setup-check-${escapeHTML(check.status || "unknown")}">
        <div>
          <strong>${escapeHTML(check.label || check.id)}</strong>
          <span>${escapeHTML(check.detail || "")}</span>
        </div>
        <div class="setup-check-side">
          <span class="setup-status">${escapeHTML(check.status || "unknown")}</span>
          ${action}
        </div>
      </div>
    `;
  }).join("");
}

function renderAuroraTimeline() {
  if (!refs.auroraTimelineBody) {
    return;
  }
  const vm = window.OrchestratorViewModel.buildActivityTimelineViewModel(state.events, {
    currentRunOnly: false,
    categories: timelineCategoriesForAurora(),
    verbosity: refs.verbositySelect ? refs.verbositySelect.value : "normal",
  });
  const items = vm.items.slice(0, 18);
  if (items.length === 0) {
    refs.auroraTimelineBody.innerHTML = `<div class="event-empty">${escapeHTML(vm.emptyMessage)}</div>`;
    return;
  }
  const icons = {
    planner: "PL",
    executor: "CX",
    worker: "WK",
    approval: "OK",
    intervention: "HU",
    human: "HU",
    files: "FI",
    tests: "TS",
    setup: "SU",
    system: "SY",
    terminal: "SH",
    fault: "!",
    status: "ST",
  };
  refs.auroraTimelineBody.innerHTML = items.map((event, index) => `
    <div class="timeline-row timeline-row-${escapeHTML(event.severity)}">
      <span class="timeline-icon">${escapeHTML(icons[event.category] || "EV")}</span>
      <div class="timeline-main">
        <div class="timeline-title">${escapeHTML(event.summary)}</div>
        <div class="timeline-meta">
          <span>${escapeHTML(event.timestampLabel)}</span>
          <span class="event-chip event-chip-${escapeHTML(event.category)}">${escapeHTML(event.categoryLabel)}</span>
          <span>${escapeHTML(event.sourceLabel)}</span>
        </div>
        <details><summary>Details</summary><pre>${escapeHTML(event.payloadText)}</pre></details>
      </div>
      <button class="button button-mini event-copy" data-event-index="${escapeHTML(String(index))}">Copy</button>
    </div>
  `).join("");
}

function renderConnection() {
  refs.addressInput.value = state.address;
  refs.autoReconnect.checked = state.reconnect.enabled;
  const vm = window.OrchestratorViewModel.buildConnectionStatusViewModel(state.snapshot, viewModelOptions({
    connection: state.connection,
    address: state.address,
    reconnecting: state.reconnect.pending,
    elapsedSeconds: elapsedSecondsSince(
      state.connection.connected
        ? state.connectionTiming.connectedAt
        : (state.connection.status === "connecting" ? state.connectionTiming.connectingAt : state.connectionTiming.disconnectedAt),
    ),
  }));
  refs.connectionBadge.className = `badge ${vm.className}`;
  refs.connectionBadge.textContent = `${vm.label} | ${vm.durationLabel}`;
  refs.connectionDetails.textContent = shellHelpers.buildConnectionDetails(state.connection, state.reconnect);
  renderTopStatus();
  renderDisconnectedBanner();
  renderAttentionBadge();
  persistShellSession();
}

function renderConnectionTimers() {
  if (!refs.connectionBadge) {
    return;
  }
  const vm = window.OrchestratorViewModel.buildConnectionStatusViewModel(state.snapshot, viewModelOptions({
    connection: state.connection,
    address: state.address,
    reconnecting: state.reconnect.pending,
    elapsedSeconds: elapsedSecondsSince(
      state.connection.connected
        ? state.connectionTiming.connectedAt
        : (state.connection.status === "connecting" ? state.connectionTiming.connectingAt : state.connectionTiming.disconnectedAt),
    ),
  }));
  refs.connectionBadge.className = `badge ${vm.className}`;
  refs.connectionBadge.textContent = `${vm.label} | ${vm.durationLabel}`;
  renderTopStatus();
}

function renderProgressPanel() {
  const vm = window.OrchestratorViewModel.buildProgressPanelViewModel(state.snapshot);
  const progressLabel = vm.progressPercent === null ? "Unavailable" : `${vm.progressPercent}%`;
  const confidenceClass = `badge badge-inline badge-${String(vm.progressConfidence || "unavailable").toLowerCase()}`;
  refs.progressBody.innerHTML = `
    <div class="progress-shell">
      <div class="progress-head">
        <div>
          <div class="summary-label">Operator Message</div>
          <div class="progress-operator">${escapeHTML(vm.operatorMessage)}</div>
        </div>
        <div class="progress-metrics">
          <span class="progress-value">${escapeHTML(progressLabel)}</span>
          <span class="${confidenceClass}">${escapeHTML(vm.progressConfidence)}</span>
        </div>
      </div>
      <div class="progress-bar-track">
        <div class="progress-bar-fill" style="width:${escapeHTML(vm.progressBarWidth)}"></div>
      </div>
      <div class="progress-detail-sections">
        ${vm.sections.map(renderProgressDetailSection).join("")}
      </div>
      <div class="progress-source-row">
        <div class="detail-row">
          <div class="detail-label">Roadmap Source</div>
          <div class="detail-value">${escapeHTML(vm.roadmapPath)} | ${escapeHTML(vm.roadmapModifiedAt)}</div>
        </div>
      </div>
    </div>
  `;
}

function renderStatus() {
  const vm = window.OrchestratorViewModel.buildStatusViewModel(state.snapshot);
  const rows = [
    ["Run ID", vm.runID],
    ["Goal", vm.goal],
    ["Run State", vm.completed ? "completed" : vm.stopReason],
    ["Stop Reason", vm.stopReason],
    ["Run Elapsed", vm.elapsedLabel],
    ["Started At", vm.startedAt],
    ["Stopped / Last Updated", vm.stoppedAt],
    ["Total Build Time", state.snapshot && state.snapshot.build_time ? state.snapshot.build_time.total_build_time_label || "Unavailable" : "Unavailable"],
    ["Current Step", state.snapshot && state.snapshot.build_time ? state.snapshot.build_time.current_step_label || "Unavailable" : "Unavailable"],
    ["Current Step Time", state.snapshot && state.snapshot.build_time ? state.snapshot.build_time.current_step_time_label || "Unavailable" : "Unavailable"],
    ["Completed", vm.completed ? "true" : "false"],
    ["Verbosity", vm.verbosity],
    ["Planner Model", `${vm.plannerModelConfigured} (${vm.plannerModelVerification})`],
    ["Codex Model", `${vm.executorModelRequested} (${vm.executorModelVerification})`],
    ["Codex Model Error", vm.executorModelError],
    ["Executor Failure Stage", vm.executorFailureStage],
    ["Executor Last Error", vm.executorLastError],
    ["Model Health", vm.modelHealthMessage],
    ["Pending Action", vm.pendingActionSummary],
    ["Pending Held", vm.pendingHeld ? "true" : "false"],
  ];

  refs.statusBody.innerHTML = rows
    .map(([label, value]) => `<div class="status-row"><div class="status-label">${escapeHTML(label)}</div><div class="status-value">${escapeHTML(value)}</div></div>`)
    .join("");
}

function renderModelHealth() {
  if (!refs.modelHealthBody) {
    return;
  }
  const health = state.snapshot && state.snapshot.model_health ? state.snapshot.model_health : {};
  const planner = health.planner || {};
  const executor = health.executor || {};
  const backend = state.snapshot && state.snapshot.backend ? state.snapshot.backend : {};
  const plannerTest = state.modelTests.planner || null;
  const executorTest = state.modelTests.executor || null;
  const rows = [
    ["Overall", health.message || "Model health has not been checked yet."],
    ["Needs Attention", health.needs_attention ? "yes" : "no"],
    ["Blocking", health.blocking ? "yes" : "no"],
    ["Planner Configured", planner.configured_model || "Unavailable"],
    ["Planner Requested", planner.requested_model || "Unavailable"],
    ["Planner Verified", planner.verified_model || planner.verification_state || "not_verified"],
    ["Planner Last Test", plannerTest ? JSON.stringify(plannerTest.planner || plannerTest, null, 2) : "Not tested from this shell session."],
    ["Codex Configured", executor.configured_model || "external Codex configuration"],
    ["Codex Requested", executor.requested_model || "Not reported yet"],
    ["Codex Verification", executor.verification_state || "not_verified"],
    ["Codex Access Mode", executor.access_mode || "Not reported"],
    ["Codex Effort", executor.effort || "Not reported"],
    ["Codex Binary", executor.codex_executable_path || "Not detected"],
    ["Codex Version", executor.codex_version || "Not detected"],
    ["Codex Config", executor.codex_config_source || "Not detected"],
    ["Codex Model Verified", executor.codex_model_verified ? "yes" : "no"],
    ["Codex Full Access Verified", executor.codex_permission_mode_verified ? "yes" : "no"],
    ["Codex Last Error", executor.last_error || "None"],
    ["Codex Last Test", executorTest ? JSON.stringify(executorTest.executor || executorTest, null, 2) : "Not tested from this shell session."],
    ["Backend PID", backend.pid || "Unavailable"],
    ["Backend Started", backend.started_at || "Unavailable"],
    ["Backend Binary", backend.binary_path || "Unavailable"],
    ["Backend Stale", backend.stale ? `yes: ${backend.stale_reason || "restart recommended"}` : "no"],
    ["Recommended Action", executor.recommended_action || planner.recommended_action || "Use the test buttons before long unattended runs."],
  ];
  refs.modelHealthBody.innerHTML = rows
    .map(([label, value]) => `<div class="detail-row"><div class="detail-label">${escapeHTML(label)}</div><div class="detail-value">${escapeHTML(value)}</div></div>`)
    .join("");
  refs.testPlannerModelButton.disabled = state.modelTests.inFlight !== "";
  refs.testExecutorModelButton.disabled = state.modelTests.inFlight !== "";
  refs.copyModelHealthButton.disabled = !state.snapshot;
  refs.restartBackendButton.disabled = state.modelTests.inFlight !== "";
}

function renderRuntimeSettings() {
  const cfg = state.runtimeConfig || {};
  const timeouts = (cfg.timeouts || (state.snapshot && state.snapshot.timeouts)) || {};
  const permissions = (cfg.permissions || (state.snapshot && state.snapshot.permissions)) || {};
  const timeoutValue = (name, fallback = "") => {
    const entry = timeouts[name] || {};
    return entry.value || fallback;
  };
  if (refs.timeoutPlannerRequest) refs.timeoutPlannerRequest.value = timeoutValue("planner_request_timeout", refs.timeoutPlannerRequest.value || "2m");
  if (refs.timeoutExecutorTurn) refs.timeoutExecutorTurn.value = timeoutValue("executor_turn_timeout", refs.timeoutExecutorTurn.value || "unlimited");
  if (refs.timeoutExecutorIdle) refs.timeoutExecutorIdle.value = timeoutValue("executor_idle_timeout", refs.timeoutExecutorIdle.value || "unlimited");
  if (refs.timeoutSubagent) refs.timeoutSubagent.value = timeoutValue("subagent_timeout", refs.timeoutSubagent.value || "unlimited");
  if (refs.timeoutShellCommand) refs.timeoutShellCommand.value = timeoutValue("shell_command_timeout", refs.timeoutShellCommand.value || "30m");
  if (refs.timeoutInstall) refs.timeoutInstall.value = timeoutValue("install_timeout", refs.timeoutInstall.value || "2h");
  if (refs.timeoutHumanWait) refs.timeoutHumanWait.value = timeoutValue("human_wait_timeout", refs.timeoutHumanWait.value || "unlimited");
  if (refs.permissionProfile && permissions.profile) {
    refs.permissionProfile.value = permissions.profile;
  }
  if (refs.runtimeSettingsSummary) {
    const rows = [
      ["Executor turn timeout", timeoutValue("executor_turn_timeout", "unlimited")],
      ["Executor idle timeout", timeoutValue("executor_idle_timeout", "unlimited")],
      ["Sub-agent timeout", timeoutValue("subagent_timeout", "unlimited")],
      ["Human wait timeout", timeoutValue("human_wait_timeout", "unlimited")],
      ["Install timeout", timeoutValue("install_timeout", "2h")],
      ["Permission profile", permissions.profile || "balanced"],
      ["Applies", timeouts.message || "Saved settings apply to future operations; active transports use changes where technically possible."],
    ];
    refs.runtimeSettingsSummary.innerHTML = rows
      .map(([label, value]) => `<div class="detail-row"><div class="detail-label">${escapeHTML(label)}</div><div class="detail-value">${escapeHTML(value)}</div></div>`)
      .join("");
  }
}

function renderUpdateStatus() {
  const status = state.updateStatus || (state.snapshot && state.snapshot.update_status) || {};
  if (!refs.updateStatusBody) {
    return;
  }
  const rows = [
    ["Current Version", status.current_version || "Unavailable"],
    ["Latest Version", status.latest_version || "Not checked"],
    ["Update Available", status.update_available ? "yes" : "no"],
    ["Channel", status.channel || (status.settings && status.settings.update_channel) || "stable"],
    ["Release URL", status.release_url || "Unavailable"],
    ["Install Supported", status.install_supported ? "yes" : "no"],
    ["Install Message", status.install_message || "Install is deferred until signed/checksummed Windows assets exist."],
    ["Last Error", status.error || "None"],
  ];
  refs.updateStatusBody.innerHTML = rows
    .map(([label, value]) => `<div class="detail-row"><div class="detail-label">${escapeHTML(label)}</div><div class="detail-value">${escapeHTML(value)}</div></div>`)
    .join("");
  if (refs.installUpdateButton) {
    refs.installUpdateButton.disabled = !status.install_supported;
  }
}

function renderPendingAction() {
  const vm = window.OrchestratorViewModel.buildPendingActionViewModel(state.snapshot);
  if (!vm.present) {
    refs.pendingBody.innerHTML = `
      <div class="detail-row">
        <div class="detail-label">Pending Action</div>
        <div class="detail-value">${escapeHTML(vm.message)}</div>
      </div>
    `;
    return;
  }
  const rows = [
    ["Planner Outcome", vm.plannerOutcome],
    ["Summary", vm.summary],
    ["Held", vm.held ? "true" : "false"],
    ["Hold Reason", vm.holdReason],
    ["Executor Prompt Summary", vm.executorPromptSummary],
    ["Dispatch Target", vm.dispatchTarget],
    ["Updated At", vm.updatedAt],
  ];

  if (vm.executorPrompt) {
    rows.push(["Full Executor Prompt", vm.executorPrompt]);
  }

  refs.pendingBody.innerHTML = rows
    .map(([label, value]) => `<div class="detail-row"><div class="detail-label">${escapeHTML(label)}</div><div class="detail-value">${escapeHTML(value)}</div></div>`)
    .join("");
}

function renderApproval() {
  const vm = window.OrchestratorViewModel.buildApprovalViewModel(state.snapshot);
  const codex = window.OrchestratorViewModel.buildCodexReadinessViewModel(state.snapshot);
  const modelBlocking = Boolean(state.snapshot && state.snapshot.model_health && state.snapshot.model_health.blocking);
  const askHuman = vm.askHuman || { present: false };
  const safeStop = vm.safeStop || { present: false };
  if (!vm.needsAttention && !modelBlocking) {
    refs.approvalBody.innerHTML = `
      <div class="attention-empty">
        <strong>No approval needed.</strong>
        <p>The latest status snapshot does not show an actionable Codex approval, planner question, blocking model issue, or worker approval waiting for you.</p>
      </div>
    `;
  } else if (askHuman.present) {
    const rows = [
      ["Planner Question / Blocker", askHuman.question],
      ["Context", askHuman.blocker],
      ["Action Summary", askHuman.actionSummary],
      ["Run ID", askHuman.runID],
      ["Planner Outcome", askHuman.plannerOutcome],
      ["Response ID", askHuman.responseID],
      ["Source", askHuman.source],
      ["Updated At", askHuman.updatedAt],
    ];
    refs.approvalBody.innerHTML = `
      <div class="attention-empty attention-ask-human">
        <strong>Planner needs your answer.</strong>
        <p>${escapeHTML(askHuman.message)}</p>
      </div>
      ${rows
        .map(([label, value]) => `<div class="detail-row"><div class="detail-label">${escapeHTML(label)}</div><div class="detail-value">${escapeHTML(value)}</div></div>`)
        .join("")}
    `;
  } else if (safeStop.present) {
    const rows = [
      ["What happened?", "Safe stop was requested."],
      ["Recommended Action", "Use Clear Stop and Continue when you are ready to resume."],
      ["Run ID", safeStop.runID],
      ["Stop Flag Present", safeStop.flagPresent ? "yes" : "no"],
      ["Reason", safeStop.reason],
      ["Applies At", safeStop.appliesAt],
      ["Stop Flag Path", safeStop.path],
    ];
    refs.approvalBody.innerHTML = `
      <div class="attention-empty attention-safe-stop">
        <strong>Safe stop was requested.</strong>
        <p>${escapeHTML(safeStop.message)}</p>
      </div>
      ${rows
        .map(([label, value]) => `<div class="detail-row"><div class="detail-label">${escapeHTML(label)}</div><div class="detail-value">${escapeHTML(value)}</div></div>`)
        .join("")}
      <div class="button-row"><button class="button button-primary" data-summary-action="clear_stop_continue">Clear Stop and Continue</button></div>
    `;
  } else if (vm.staleGuard && vm.staleGuard.present) {
    refs.approvalBody.innerHTML = `
      <div class="attention-empty attention-stale-run">
        <strong>Recovery needed.</strong>
        <p>${escapeHTML(vm.staleGuard.message)}</p>
      </div>
      <div class="detail-row"><div class="detail-label">Recommended Action</div><div class="detail-value">Use Recover Backend / Unlock Repo. It mechanically clears stale active-run state without deleting history or artifacts, and only restarts dogfood-owned backend processes when needed.</div></div>
      <div class="detail-row"><div class="detail-label">Run ID</div><div class="detail-value">${escapeHTML(vm.staleGuard.runID)}</div></div>
      <div class="detail-row"><div class="detail-label">Stale Backend PID</div><div class="detail-value">${escapeHTML(vm.staleGuard.backendPID)}</div></div>
      <div class="detail-row"><div class="detail-label">Session ID</div><div class="detail-value">${escapeHTML(vm.staleGuard.sessionID)}</div></div>
      <div class="detail-row"><div class="detail-label">Why</div><div class="detail-value">${escapeHTML(vm.staleGuard.reason)}</div></div>
      <div class="button-row"><button class="button button-danger-soft" data-summary-action="recover_backend">Recover Backend / Unlock Repo</button></div>
    `;
  } else if (!vm.needsAttention && modelBlocking) {
    refs.approvalBody.innerHTML = `
      <div class="detail-row"><div class="detail-label">Action Required</div><div class="detail-value">${escapeHTML(codex.title)}</div></div>
      <div class="detail-row"><div class="detail-label">Access Mode</div><div class="detail-value">${escapeHTML(codex.accessMode)}</div></div>
      <div class="detail-row"><div class="detail-label">Model / Effort</div><div class="detail-value">${escapeHTML(codex.model)} / ${escapeHTML(codex.effort)}</div></div>
      <div class="detail-row"><div class="detail-label">Verification</div><div class="detail-value">${escapeHTML(codex.verificationState)}</div></div>
      <div class="detail-row"><div class="detail-label">Codex Binary</div><div class="detail-value">${escapeHTML(codex.codexPath)}</div></div>
      <div class="detail-row"><div class="detail-label">Codex Version</div><div class="detail-value">${escapeHTML(codex.codexVersion)}</div></div>
      <div class="detail-row"><div class="detail-label">Config Source</div><div class="detail-value">${escapeHTML(codex.codexConfigSource)}</div></div>
      <div class="detail-row"><div class="detail-label">Plain-English Issue</div><div class="detail-value">${escapeHTML(codex.plainEnglish)}</div></div>
      <div class="detail-row"><div class="detail-label">Last Model Error</div><div class="detail-value">${escapeHTML(codex.lastError)}</div></div>
      <div class="detail-row"><div class="detail-label">Full Access Ready</div><div class="detail-value">${escapeHTML(codex.fullAccessReady)}</div></div>
      <div class="detail-row"><div class="detail-label">How to Fix</div><div class="detail-value">${escapeHTML(codex.recommendedAction)}</div></div>
    `;
  } else {
    const rows = [
      ["What is being requested?", vm.plainEnglish.requested],
      ["Why is it needed?", vm.plainEnglish.why],
      ["If approved", vm.plainEnglish.approveEffect],
      ["If denied", vm.plainEnglish.denyEffect],
      ["Scope", vm.plainEnglish.scope],
      ["Approval State", vm.state],
      ["Kind", vm.kind],
      ["Run ID", vm.runID],
      ["Executor Turn ID", vm.executorTurnID],
      ["Executor Thread ID", vm.executorThreadID],
      ["Command", vm.command],
      ["CWD", vm.cwd],
      ["Grant Root", vm.grantRoot],
      ["Last Control Action", vm.lastControlAction],
      ["Worker Approvals", String(vm.workerApprovalRequired)],
    ];

    refs.approvalBody.innerHTML = rows
      .map(([label, value]) => `<div class="detail-row"><div class="detail-label">${escapeHTML(label)}</div><div class="detail-value">${escapeHTML(value)}</div></div>`)
      .join("");
  }

  if (refs.askHumanActionsRow) {
    refs.askHumanActionsRow.hidden = !askHuman.present;
  }
  if (refs.askHumanStatus) {
    refs.askHumanStatus.textContent = state.actionRequired.status || (askHuman.present
      ? "Type the raw answer, then Send Answer and Continue. The shell queues it with inject_control_message and resumes with continue_run."
      : "No planner answer is waiting.");
  }
  if (refs.sendAnswerContinueButton) {
    refs.sendAnswerContinueButton.disabled = !askHuman.present || state.actionRequired.inFlight;
  }
  if (refs.sendAnswerOnlyButton) {
    refs.sendAnswerOnlyButton.disabled = !askHuman.present || state.actionRequired.inFlight;
  }
  if (refs.continueQueuedAnswerButton) {
    refs.continueQueuedAnswerButton.disabled = !askHuman.present || state.actionRequired.inFlight || !state.actionRequired.queuedAskHumanAnswer;
  }

  const canApprove = vm.canApprove;
  const canDeny = vm.canDeny;
  refs.approveButton.disabled = !canApprove;
  refs.denyButton.disabled = !canDeny;
  refs.approveButton.hidden = !vm.present;
  refs.denyButton.hidden = !vm.present;
  refs.copyApprovalDetailsButton.hidden = !vm.needsAttention || askHuman.present;
  if (refs.approvalActionsRow) {
    refs.approvalActionsRow.hidden = !vm.needsAttention || askHuman.present;
  }
  renderAttentionBadge();
}

function renderRunSummary() {
  const vm = window.OrchestratorViewModel.buildRunSummaryViewModel(state.snapshot, state.artifacts, state.events);
  const actionButtons = (vm.primaryActions || [])
    .map((action) => `<button class="button button-mini summary-action" data-summary-action="${escapeHTML(action.id)}">${escapeHTML(action.label)}</button>`)
    .join("");
  const rows = [
    ["Run State", vm.runState],
      ["Why It Stopped", vm.stopExplanation],
      ["Recommended Next Action", vm.nextAction],
      ["Elapsed", vm.elapsed],
      ["Planner Message", vm.operatorMessage],
    ["Progress", vm.progress],
    ["Pending Action", vm.pendingAction],
    ["Approval", vm.approval],
    ["Workers", vm.workerSummary],
    ["Latest Artifact", vm.latestArtifact],
    ["Latest Error", vm.latestError],
    ["Recent Events", vm.recentEvents.join(" | ")],
  ];

  refs.copyLatestErrorButton.disabled = vm.latestError === "No latest error is available.";
  refs.summaryBody.innerHTML = `
    <div class="what-happened-card what-happened-card-${escapeHTML(vm.stopSeverity || "neutral")}">
      <div>
        <div class="summary-label">Stopped-run diagnosis</div>
        <strong>${escapeHTML(vm.stopExplanation)}</strong>
        <p>${escapeHTML(vm.nextAction)}</p>
      </div>
      <div class="button-row">${actionButtons}</div>
    </div>
    ${rows
    .map(([label, value]) => `<div class="summary-row"><div class="summary-label">${escapeHTML(label)}</div><div class="summary-value">${escapeHTML(value)}</div></div>`)
    .join("")}
  `;
}

function renderSideChat() {
  const vm = window.OrchestratorViewModel.buildSideChatViewModel(state.sideChat);
  const banner = `<div class="side-chat-mode-note">${escapeHTML(vm.modeDescription)}</div>`;
  if (vm.items.length === 0) {
    refs.sideChatBody.innerHTML = `${banner}<div class="event-empty">${escapeHTML(vm.message)}</div>`;
    return;
  }

  refs.sideChatBody.innerHTML = banner + vm.items
    .map((item, index) => `
      <div class="side-chat-item">
        <div class="side-chat-header">
          <div class="side-chat-role">${escapeHTML(item.source)}</div>
          <div class="side-chat-meta">${escapeHTML(item.createdAt)}</div>
          <button class="button button-mini side-chat-copy" data-side-chat-index="${escapeHTML(String(index))}">Copy</button>
        </div>
        <div class="side-chat-text">${escapeHTML(item.rawText)}</div>
        <div class="side-chat-meta">status: ${escapeHTML(item.status)} | backend: ${escapeHTML(item.backendState)} | run: ${escapeHTML(item.runID)}</div>
        <div class="side-chat-meta">context: ${escapeHTML(item.contextPolicy)}</div>
        <div class="side-chat-response">${escapeHTML(item.responseMessage)}</div>
      </div>
    `)
    .join("");
}

function renderDogfoodIssues() {
  const vm = window.OrchestratorViewModel.buildDogfoodIssuesViewModel(state.dogfoodIssues, state.selectedDogfoodIssueID);
  state.selectedDogfoodIssueID = vm.selectedIssueID;

  if (vm.items.length === 0) {
    refs.dogfoodBody.innerHTML = `<div class="event-empty">${escapeHTML(vm.message)}</div>`;
    refs.dogfoodDetail.textContent = "Capture a friction note, bug, or daily-use annoyance here. Notes are timestamped and tied to the repo and latest run when available.";
    persistShellSession();
    return;
  }

  refs.dogfoodBody.innerHTML = vm.items
    .map((item) => `
      <div class="artifact-item ${item.selected ? "active" : ""}" data-dogfood-issue-id="${escapeHTML(item.id)}">
        <div class="artifact-path">${escapeHTML(item.title)}</div>
        <div class="artifact-meta">${escapeHTML(item.createdAt)} | ${escapeHTML(item.source)} | run: ${escapeHTML(item.runID)}</div>
        <div class="artifact-preview">${escapeHTML(item.note)}</div>
      </div>
    `)
    .join("");

  if (!vm.selectedIssue) {
    refs.dogfoodDetail.textContent = "Select a captured dogfood issue to inspect it.";
  } else {
    refs.dogfoodDetail.textContent = [
      `Title: ${vm.selectedIssue.title}`,
      `Created: ${vm.selectedIssue.createdAt}`,
      `Source: ${vm.selectedIssue.source}`,
      `Run: ${vm.selectedIssue.runID}`,
      "",
      vm.selectedIssue.note,
    ].join("\n");
  }
  persistShellSession();
}

function renderWorkers() {
  const vm = window.OrchestratorViewModel.buildWorkerPanelViewModel(state.workers, state.selectedWorkerID);
  state.selectedWorkerID = vm.selectedWorkerID;
  const counts = vm.countsByStatus;
  const countsText = `count=${vm.count} | creating=${counts.creating} | pending=${counts.pending} | assigned=${counts.assigned} | active=${counts.executor_active} | approval_required=${counts.approval_required} | idle=${counts.idle} | completed=${counts.completed} | failed=${counts.failed}`;

  refs.workersBody.innerHTML = `
    <div class="worker-counts">${escapeHTML(countsText)}</div>
    ${vm.items.length === 0
      ? `<div class="worker-empty">${escapeHTML(vm.message)}</div>`
      : vm.items.map((item) => `
        <div class="worker-card ${item.selected ? "active" : ""}" data-worker-id="${escapeHTML(item.workerID)}">
          <div class="worker-header">
            <div class="worker-name">${escapeHTML(item.workerName)}</div>
            <div class="worker-meta">${escapeHTML(item.status)}</div>
          </div>
          <div class="worker-summary">${escapeHTML(item.scope)}</div>
          <div class="worker-note">id: ${escapeHTML(item.workerID)}</div>
          <div class="worker-note">worktree: ${escapeHTML(item.worktreePath)}</div>
          <div class="worker-note">approval_required: ${item.approvalRequired ? "true" : "false"} | thread: ${escapeHTML(item.executorThreadID)} | turn: ${escapeHTML(item.executorTurnID)}</div>
          <div class="worker-note">task: ${escapeHTML(item.workerTaskSummary)}</div>
          <div class="worker-note">result: ${escapeHTML(item.workerResultSummary)}</div>
        </div>
      `).join("")}
  `;

  const selected = vm.selectedWorker;
  if (!selected) {
    refs.workerDetailBody.innerHTML = `<div class="worker-empty">${escapeHTML(vm.message || "Select a worker to inspect detail.")}</div>`;
    refs.workerDispatchButton.disabled = true;
    refs.workerRemoveButton.disabled = true;
  } else {
    if (!refs.workerDispatchPrompt.value.trim()) {
      refs.workerDispatchPrompt.value = selected.workerTaskSummary !== "Unavailable"
        ? selected.workerTaskSummary
        : "";
    }
    refs.workerDetailBody.innerHTML = `
      <div class="detail-row"><div class="detail-label">Worker</div><div class="detail-value">${escapeHTML(selected.workerName)} (${escapeHTML(selected.workerID)})</div></div>
      <div class="detail-row"><div class="detail-label">Status</div><div class="detail-value">${escapeHTML(selected.status)}</div></div>
      <div class="detail-row"><div class="detail-label">Scope</div><div class="detail-value">${escapeHTML(selected.scope)}</div></div>
      <div class="detail-row"><div class="detail-label">Worktree</div><div class="detail-value">${escapeHTML(selected.worktreePath)}</div></div>
      <div class="detail-row"><div class="detail-label">Approval</div><div class="detail-value">${selected.approvalRequired ? "required" : "not required"} | ${escapeHTML(selected.approvalKind)} | ${escapeHTML(selected.approvalPreview)}</div></div>
      <div class="detail-row"><div class="detail-label">Thread / Turn</div><div class="detail-value">${escapeHTML(selected.executorThreadID)} / ${escapeHTML(selected.executorTurnID)}</div></div>
      <div class="detail-row"><div class="detail-label">Control Flags</div><div class="detail-value">interruptible=${selected.interruptible ? "true" : "false"} | steerable=${selected.steerable ? "true" : "false"} | last=${escapeHTML(selected.lastControlAction)}</div></div>
      <div class="detail-row"><div class="detail-label">Task</div><div class="detail-value">${escapeHTML(selected.workerTaskSummary)}</div></div>
      <div class="detail-row"><div class="detail-label">Result</div><div class="detail-value">${escapeHTML(selected.workerResultSummary)}</div></div>
      <div class="detail-row"><div class="detail-label">Error</div><div class="detail-value">${escapeHTML(selected.workerErrorSummary)}</div></div>
      <div class="detail-row"><div class="detail-label">Updated At</div><div class="detail-value">${escapeHTML(selected.updatedAt)}</div></div>
    `;
    refs.workerDispatchButton.disabled = false;
    refs.workerRemoveButton.disabled = false;
    if (!refs.workerIntegrateIds.value.trim()) {
      refs.workerIntegrateIds.value = selected.workerID;
    }
  }

  if (state.workerActionResult) {
    refs.workerActionResult.textContent = JSON.stringify(state.workerActionResult, null, 2);
  } else {
    refs.workerActionResult.textContent = "No worker action run yet. Create, dispatch, remove, or integrate through the real engine protocol.";
  }
  persistShellSession();
}

function renderEvents() {
  if (refs.auroraEventsAutoScroll) {
    refs.auroraEventsAutoScroll.checked = state.activityFilters.autoScroll;
  }
  const vm = window.OrchestratorViewModel.buildActivityTimelineViewModel(state.events, {
    searchText: state.activityFilters.searchText,
    currentRunOnly: state.activityFilters.currentRunOnly,
    currentRunID: activeRunID(),
    categories: state.activityFilters.categories,
    verbosity: refs.verbositySelect ? refs.verbositySelect.value : "normal",
  });

  refs.eventsMeta.textContent = `${vm.filteredCount} shown / ${vm.totalCount} total | verbosity: ${vm.verbosity} controls this output | current run filter: ${vm.currentRunOnly ? "on" : "off"} | run: ${vm.currentRunID}`;
  const latestStatusError = window.OrchestratorViewModel.buildLatestErrorViewModel(state.snapshot, state.events);
  const pinnedError = latestStatusError.present ? `
    <div class="pinned-error pinned-error-large">
      <strong>Latest error</strong>
      <span>${escapeHTML(latestStatusError.summary)}</span>
      <small>Copy Debug Bundle from Home or What Happened when asking for help.</small>
    </div>
  ` : "";

  if (vm.items.length === 0) {
    refs.eventsBody.innerHTML = `${pinnedError}<div class="event-empty">${escapeHTML(vm.emptyMessage)}</div>`;
    return;
  }

  refs.eventsBody.innerHTML = pinnedError + vm.items
    .map((event, index) => `
      <div class="event-card event-card-${escapeHTML(event.severity)}">
        <div class="event-rail event-rail-${escapeHTML(event.category)}"></div>
        <div class="event-card-body">
          <div class="event-header">
            <div class="event-heading-stack">
              <div class="event-name">${escapeHTML(event.summary)}</div>
              <div class="event-label-row">
                <span class="event-chip event-chip-${escapeHTML(event.category)}">${escapeHTML(event.categoryLabel)}</span>
                <span class="event-chip event-chip-source">${escapeHTML(event.sourceLabel)}</span>
                <span class="event-time">${escapeHTML(event.timestampLabel)}</span>
              </div>
            </div>
            <div class="event-meta-stack">
              <span class="event-sequence">#${escapeHTML(String(event.sequence || ""))}</span>
              <button class="button button-mini event-copy" data-event-index="${escapeHTML(String(index))}">Copy Payload</button>
            </div>
          </div>
          <div class="event-subhead">${escapeHTML(event.eventName)}${event.runID ? ` | run=${escapeHTML(event.runID)}` : ""}</div>
          ${event.showPayload ? `<pre class="event-payload">${escapeHTML(event.payloadText)}</pre>` : `<details class="event-details"><summary>Technical details</summary><pre class="event-payload">${escapeHTML(event.payloadText)}</pre></details>`}
        </div>
      </div>
    `)
    .join("");

  if (state.activityFilters.autoScroll) {
    refs.eventsBody.scrollTop = 0;
  }
}

function renderArtifacts() {
  const vm = window.OrchestratorViewModel.buildArtifactListViewModel(state.snapshot, state.artifacts);
  refs.artifactsMeta.textContent = `Latest: ${vm.latestPath}`;

  if (vm.items.length === 0) {
    refs.artifactList.innerHTML = `<div class="event-empty">${escapeHTML(vm.message)}</div>`;
  } else {
    refs.artifactList.innerHTML = vm.items
      .map((item) => `
        <div class="artifact-item ${item.path === state.selectedArtifactPath ? "active" : ""}" data-artifact-path="${escapeHTML(item.path)}">
          <div class="artifact-path">${escapeHTML(item.path)}</div>
          <div class="artifact-meta">${escapeHTML(item.category)} | ${escapeHTML(item.source)} | ${escapeHTML(item.at)}</div>
          <div class="artifact-preview">${escapeHTML(item.preview)}</div>
        </div>
      `)
      .join("");
  }

  if (!state.artifactContent || !state.selectedArtifactPath) {
    refs.artifactViewer.textContent = "No artifact selected. Artifacts appear here after planner/executor turns complete, then you can select one to inspect the raw text or JSON.";
    return;
  }

  if (state.artifactContent.available === false) {
    refs.artifactViewer.textContent = state.artifactContent.message || "Artifact unavailable.";
    return;
  }

  const header = [
    `Path: ${state.artifactContent.path || state.selectedArtifactPath}`,
    `Type: ${state.artifactContent.content_type || "text/plain"}`,
    `Bytes: ${state.artifactContent.byte_size || 0}`,
    `Truncated: ${state.artifactContent.truncated ? "true" : "false"}`,
  ].join("\n");
  refs.artifactViewer.textContent = `${header}\n\n${state.artifactContent.content || ""}`.trim();
  persistShellSession();
}

function renderRepoTree() {
  const vm = window.OrchestratorViewModel.buildRepoTreeViewModel(state.repoTree, state.repoFile);
  refs.repoTreeMeta.textContent = `${vm.path} | ${vm.count} item(s) | read-only`;
  refs.repoUpButton.disabled = !vm.parentPath;

  if (vm.items.length === 0) {
    refs.repoTreeList.innerHTML = `<div class="event-empty">${escapeHTML(vm.message)}</div>`;
  } else {
    refs.repoTreeList.innerHTML = vm.items
      .map((item) => `
        <div class="repo-tree-item ${item.path === state.selectedRepoPath ? "active" : ""}" data-repo-path="${escapeHTML(item.path)}" data-repo-kind="${escapeHTML(item.kind)}">
          <div class="repo-tree-name">${item.kind === "directory" ? "DIR" : "FILE"} | ${escapeHTML(item.name)}</div>
          <div class="repo-tree-meta">${escapeHTML(item.path)} | ${escapeHTML(item.modifiedAt)}</div>
          <div class="repo-tree-meta">${item.readOnly ? "read-only" : "editable"}${item.editableViaContractEditor ? " | openable in contract editor" : ""}${item.kind === "file" ? ` | ${escapeHTML(String(item.byteSize))} bytes` : ""}</div>
        </div>
      `)
      .join("");
  }

  if (!vm.openFile) {
    refs.repoFileMeta.textContent = "Select a repo file to view it.";
    refs.repoFileViewer.textContent = "Repo browsing is read-only in this slice. Canonical contract files can be opened in the contract editor.";
    refs.repoOpenInContractButton.disabled = true;
    return;
  }

  refs.repoFileMeta.textContent = `${vm.openFile.path} | ${vm.openFile.contentType} | ${vm.openFile.byteSize} bytes | truncated: ${vm.openFile.truncated ? "true" : "false"}`;
  refs.repoFileViewer.textContent = vm.openFile.available === false
    ? vm.openFile.message
    : vm.openFile.content;
  refs.repoOpenInContractButton.disabled = !vm.openFile.editableViaContractEditor;
  persistShellSession();
}

function renderAutofill() {
  const vm = window.OrchestratorViewModel.buildAutofillViewModel(state.autofill.result);
  refs.autofillProjectSummary.value = state.autofill.answers.project_summary || "";
  refs.autofillDesiredOutcome.value = state.autofill.answers.desired_outcome || "";
  refs.autofillUsersPlatform.value = state.autofill.answers.users_platform || "";
  refs.autofillConstraints.value = state.autofill.answers.constraints || "";
  refs.autofillMilestones.value = state.autofill.answers.milestones || "";
  refs.autofillDecisions.value = state.autofill.answers.decisions || "";
  refs.autofillNotes.value = state.autofill.answers.notes || "";
  document.querySelectorAll("[data-autofill-target]").forEach((input) => {
    input.checked = state.autofill.targets.includes(input.getAttribute("data-autofill-target"));
  });
  refs.autofillStepLabel.textContent = autofillStepTitle(state.autofill.step);
  document.querySelectorAll("[data-autofill-step]").forEach((element) => {
    const step = Number(element.getAttribute("data-autofill-step") || "0");
    element.hidden = step !== state.autofill.step;
  });

  refs.autofillBackButton.disabled = state.autofill.step === 0;
  refs.autofillNextButton.disabled = state.autofill.step >= 3;
  refs.autofillRunButton.disabled = state.autofill.step !== 3;

  refs.autofillMeta.textContent = state.autofill.result
    ? `${vm.message} | model: ${vm.model} | generated_at: ${vm.generatedAt}`
    : "Answer the guided questions, choose target files, then draft through the real engine protocol.";

  if (vm.files.length === 0) {
    refs.autofillDraftList.innerHTML = `<div class="event-empty">No autofill draft generated yet.</div>`;
    refs.autofillPreview.textContent = "Drafted contract content will preview here before any save.";
    refs.saveAutofillDraftButton.disabled = true;
    return;
  }

  refs.autofillDraftList.innerHTML = vm.files
    .map((file) => `
      <div class="artifact-item ${file.path === state.autofill.selectedDraftPath ? "active" : ""}" data-autofill-path="${escapeHTML(file.path)}">
        <div class="artifact-path">${escapeHTML(file.path)}</div>
        <div class="artifact-meta">${escapeHTML(file.summary)} | existing: ${file.existing ? "true" : "false"} | mtime: ${escapeHTML(file.existingMTime)}</div>
      </div>
    `)
    .join("");

  const draft = selectedAutofillDraft();
  if (!draft) {
    refs.autofillPreview.textContent = "Select a drafted file to preview it.";
    refs.saveAutofillDraftButton.disabled = true;
    return;
  }

  refs.autofillPreview.textContent = `Path: ${draft.path}\nSummary: ${draft.summary}\nExisting: ${draft.existing ? "true" : "false"}\n\n${draft.content}`.trim();
  refs.saveAutofillDraftButton.disabled = false;
}

function renderContractFiles() {
  const files = state.contractFiles && Array.isArray(state.contractFiles.files) ? state.contractFiles.files : [];
  if (files.length === 0) {
    refs.contractFileList.innerHTML = `<div class="event-empty">No contract file listing available yet.</div>`;
  } else {
    refs.contractFileList.innerHTML = files
      .map((file) => `
        <div class="contract-item ${file.path === state.selectedContractPath ? "active" : ""}" data-contract-path="${escapeHTML(file.path)}">
          <div class="contract-path">${escapeHTML(file.path)}</div>
          <div class="contract-meta">${file.exists ? "exists" : "missing"} | ${escapeHTML(file.modified_at || "unopened")}</div>
        </div>
      `)
      .join("");
  }

  if (!state.contractOpenFile) {
    refs.contractEditorMeta.textContent = state.selectedContractPath
      ? `${state.selectedContractPath} is not opened yet.`
      : "No contract file opened yet.";
    refs.contractEditor.value = "";
    return;
  }

  refs.contractEditorMeta.textContent = `${state.contractOpenFile.path} | ${state.contractOpenFile.modified_at || "no mtime"} | ${state.contractOpenFile.byte_size || 0} bytes`;
  refs.contractEditor.value = state.contractOpenFile.content || "";
  persistShellSession();
}

function renderTerminal() {
  const vm = window.OrchestratorViewModel.buildTerminalTabsViewModel(state.terminal);
  refs.terminalMeta.textContent = vm.activeSummary;
  refs.terminalTabs.innerHTML = vm.sessions.length === 0
    ? `<div class="terminal-tab terminal-tab-empty">No terminal tabs yet.</div>`
    : vm.sessions.map((session) => `
      <button class="terminal-tab ${session.selected ? "active" : ""}" data-terminal-tab="${escapeHTML(session.sessionID)}">
        <span class="terminal-tab-label">${escapeHTML(session.label)}</span>
        <span class="terminal-tab-status">${escapeHTML(session.status)}</span>
      </button>
    `).join("");
  refs.terminalOutput.textContent = vm.output;
  refs.terminalNewButton.disabled = false;
  refs.terminalCloseButton.disabled = !vm.canClose;
  refs.terminalStopButton.disabled = !vm.canStop;
  refs.terminalSendButton.disabled = !vm.canSend;
  refs.terminalOutput.scrollTop = refs.terminalOutput.scrollHeight;
}

function renderAll() {
  renderFlash();
  renderIssue();
  renderTopStatus();
  renderDisconnectedBanner();
  renderAttentionBadge();
  renderVerbosityHelp();
  renderConnection();
  renderHomeDashboard();
  renderAuroraDashboard();
  renderProgressPanel();
  renderStatus();
  renderModelHealth();
  renderRuntimeSettings();
  renderUpdateStatus();
  renderPendingAction();
  renderApproval();
  renderRunSummary();
  renderSideChat();
  renderDogfoodIssues();
  renderWorkers();
  renderArtifacts();
  renderRepoTree();
  renderAutofill();
  renderContractFiles();
  renderTerminal();
  renderEvents();
  setActiveTab(state.activeTab || "home", { noScroll: true });
}

async function connect(options = {}) {
  if (state.reconnect.inFlight) {
    return;
  }

  state.reconnect.inFlight = true;
  clearReconnectTimer();
  try {
    state.address = refs.addressInput.value.trim() || defaultAddress;
    persistShellSession();
    state.connectionTiming.connectingAt = new Date().toISOString();
    setFlash("info", options.automatic ? `Reconnecting to ${state.address} ...` : `Connecting to ${state.address} ...`);
    const response = await window.orchestratorConsole.connect(state.address);
    state.connection = response.connection;
    state.expectedRepoPath = response.expectedRepoPath || (response.connection && response.connection.expectedRepoPath) || state.expectedRepoPath;
    state.connectionTiming.connectedAt = new Date().toISOString();
    state.snapshot = response.snapshot;
    applyModelHealthNormalization();
    state.lastRefreshedAt = new Date().toISOString();
    await hydrateProtocolBackedPanels(true);
    clearReconnectTimer();
    state.reconnect.attempts = 0;
    clearIssue();
    renderAll();
    maybeFocusActionRequired();
    setFlash("success", options.automatic ? "Reconnected to the control server." : "Connected to the control server.");
    void autoTestModelHealth("connect");
  } catch (error) {
    state.connection = {
      connected: false,
      status: "error",
      address: state.address,
      message: error.message,
    };
    state.connectionTiming.disconnectedAt = new Date().toISOString();
    renderConnection();
    reportIssue("connection", error, options.automatic ? "Auto-reconnect will keep retrying while enabled." : "");
    if (state.reconnect.enabled) {
      scheduleReconnect(options.trigger || "connect_failed");
    }
  } finally {
    state.reconnect.inFlight = false;
  }
}

async function hydrateProtocolBackedPanels(refreshContracts = false) {
  if (!state.connection.connected && !state.snapshot) {
    return;
  }

  const runID = activeRunID();
  const [artifactsResult, sideChatResult, dogfoodResult, workersResult, runtimeConfigResult, updateStatusResult, setupHealthResult] = await Promise.allSettled([
    window.orchestratorConsole.listRecentArtifacts(runID, "", 12, state.address),
    window.orchestratorConsole.listSideChatMessages(activeRepoPath(), 20, state.address),
    window.orchestratorConsole.listDogfoodIssues(activeRepoPath(), 20, state.address),
    window.orchestratorConsole.listWorkers(runID, 20, state.address),
    window.orchestratorConsole.getRuntimeConfig(state.address),
    window.orchestratorConsole.getUpdateStatus(state.address),
    window.orchestratorConsole.getSetupHealth(state.address),
  ]);
  if (artifactsResult.status === "fulfilled") {
    state.artifacts = artifactsResult.value;
  } else {
    reportIssue("artifacts", artifactsResult.reason, "The shell stayed attached, but artifacts could not be refreshed.");
  }
  if (sideChatResult.status === "fulfilled") {
    state.sideChat = sideChatResult.value;
  } else {
    reportIssue("side chat", sideChatResult.reason, "Recorded side-chat history could not be refreshed.");
  }
  if (dogfoodResult.status === "fulfilled") {
    state.dogfoodIssues = dogfoodResult.value;
  } else {
    reportIssue("dogfood issues", dogfoodResult.reason, "Dogfood notes could not be refreshed.");
  }
  if (workersResult.status === "fulfilled") {
    state.workers = workersResult.value;
  } else {
    reportIssue("workers", workersResult.reason, "Worker visibility could not be refreshed.");
  }
  if (runtimeConfigResult.status === "fulfilled") {
    state.runtimeConfig = runtimeConfigResult.value;
  } else {
    reportIssue("runtime config", runtimeConfigResult.reason, "Runtime settings could not be refreshed.");
  }
  if (updateStatusResult.status === "fulfilled") {
    state.updateStatus = updateStatusResult.value;
  } else {
    reportIssue("updates", updateStatusResult.reason, "Update status could not be refreshed.");
  }
  if (setupHealthResult.status === "fulfilled") {
    state.setupHealth = setupHealthResult.value;
  } else {
    reportIssue("setup checks", setupHealthResult.reason, "Fresh-repo setup checks could not be refreshed.");
  }
  ensureSelectedWorker();
  ensureSelectedDogfoodIssue();

  const shouldRefreshContracts = refreshContracts || !state.contractFiles || !Array.isArray(state.contractFiles.files) || state.contractFiles.files.length === 0;
  if (shouldRefreshContracts) {
    try {
      state.contractFiles = await window.orchestratorConsole.listContractFiles(activeRepoPath(), state.address);
      if (!state.selectedContractPath) {
        const firstChoice = state.contractFiles.files.find((file) => file.exists) || state.contractFiles.files[0];
        if (firstChoice) {
          state.selectedContractPath = firstChoice.path;
        }
      } else if (!state.contractFiles.files.some((file) => file.path === state.selectedContractPath)) {
        const fallback = state.contractFiles.files.find((file) => file.exists) || state.contractFiles.files[0];
        state.selectedContractPath = fallback ? fallback.path : "";
        state.contractOpenFile = null;
      }
      if (state.selectedContractPath) {
        await openContractByPath(state.selectedContractPath, { quiet: true });
      }
      await loadSavedGoal({ quiet: true });
    } catch (error) {
      reportIssue("contract files", error, "Canonical contract files could not be listed.");
    }
  }
  if (state.savedGoal.status === "not_loaded" && activeRepoPath()) {
    await loadSavedGoal({ quiet: true });
  }

  if (state.selectedArtifactPath) {
    try {
      await openArtifactByPath(state.selectedArtifactPath, { quiet: true });
    } catch (_error) {
      const latest = state.artifacts && Array.isArray(state.artifacts.items)
        ? (state.artifacts.items.find((item) => item.latest) || state.artifacts.items[0] || null)
        : null;
      if (latest && latest.path) {
        state.selectedArtifactPath = latest.path;
        try {
          await openArtifactByPath(latest.path, { quiet: true });
        } catch (_secondError) {
          state.selectedArtifactPath = "";
          state.artifactContent = null;
        }
      } else {
        state.selectedArtifactPath = "";
        state.artifactContent = null;
      }
    }
  } else if (state.artifacts && Array.isArray(state.artifacts.items) && state.artifacts.items.length > 0) {
    state.selectedArtifactPath = (state.artifacts.items.find((item) => item.latest) || state.artifacts.items[0]).path;
    try {
      await openArtifactByPath(state.selectedArtifactPath, { quiet: true });
    } catch (_error) {
      // openArtifactByPath already reports truthfully
    }
  }

  const treePath = currentRepoTreePath();
  try {
    state.repoTree = await window.orchestratorConsole.listRepoTree(activeRepoPath(), treePath, 200, state.address);
    if (state.selectedRepoPath) {
      await openRepoFileByPath(state.selectedRepoPath, { quiet: true });
      if (state.repoFile && state.repoFile.available === false) {
        state.selectedRepoPath = "";
        state.repoFile = null;
      }
    }
  } catch (error) {
    reportIssue("repo browser", error, "Repo tree refresh failed after reconnect.");
  }
}

async function refreshSetupHealth(options = {}) {
  try {
    state.setupHealth = await window.orchestratorConsole.getSetupHealth(state.address);
    renderSetupHealth();
    if (!options.quiet) {
      setFlash("success", "Setup checks refreshed.");
    }
  } catch (error) {
    reportIssue("setup checks", error);
  }
}

async function runSetupHealthAction(action) {
  const normalized = String(action || "").trim();
  if (normalized === "") {
    return;
  }
  state.setupActionInFlight = normalized;
  renderSetupHealth();
  try {
    const result = await window.orchestratorConsole.runSetupAction(normalized, activeRepoPath(), state.address);
    recordLocalActivity("setup_action_completed", { action: normalized, status: result.status }, result.detail || `Setup action completed: ${normalized}.`);
    await refreshStatus({ quiet: true, refreshContracts: true });
    await refreshSetupHealth({ quiet: true });
    setFlash(result.manual ? "info" : "success", result.detail || `Setup action ${normalized} completed.`);
  } catch (error) {
    reportIssue("setup action", error);
  } finally {
    state.setupActionInFlight = "";
    renderSetupHealth();
  }
}

async function loadSavedGoal(options = {}) {
  try {
    const goalFile = await window.orchestratorConsole.openContractFile(".orchestrator/goal.md", activeRepoPath(), state.address);
    state.savedGoal = {
      content: goalFile.content || "",
      modifiedAt: goalFile.modified_at || "",
      exists: goalFile.exists !== false,
      dirty: false,
      status: "loaded",
    };
    if (refs.homeGoal && !refs.homeGoal.value.trim()) {
      refs.homeGoal.value = state.savedGoal.content || "";
    }
    renderSavedGoal();
    if (!options.quiet) {
      setFlash("success", "Saved goal loaded.");
    }
  } catch (_error) {
    state.savedGoal = {
      content: "",
      modifiedAt: "",
      exists: false,
      dirty: false,
      status: "missing",
    };
    renderSavedGoal();
  }
}

async function saveGoalFromHome() {
  const content = refs.homeGoal ? refs.homeGoal.value.trim() : "";
  if (content === "") {
    setFlash("error", "Write a goal before saving it.");
    return;
  }
  const replacingMeaningfulGoal = state.savedGoal.exists
    && state.savedGoal.content.trim() !== ""
    && state.savedGoal.content.trim() !== content;
  if (replacingMeaningfulGoal && !window.confirm("Replace the saved goal file with the edited goal?")) {
    return;
  }
  try {
    const result = await window.orchestratorConsole.saveContractFile({
      path: ".orchestrator/goal.md",
      repoPath: activeRepoPath(),
      address: state.address,
      content,
      expectedMTime: state.savedGoal.modifiedAt || "",
    });
    state.savedGoal = {
      content,
      modifiedAt: result.modified_at || "",
      exists: true,
      dirty: false,
      status: "saved",
    };
    state.contractFiles = await window.orchestratorConsole.listContractFiles(activeRepoPath(), state.address);
    renderAll();
    setFlash("success", "Goal saved.");
  } catch (error) {
    reportIssue("save goal", error, "If the goal changed externally, refresh and try again.");
  }
}

async function captureRunSnapshot() {
  try {
    const result = await window.orchestratorConsole.captureSnapshot(activeRunID(), activeRepoPath(), state.address);
    state.latestSnapshotCapture = result;
    recordLocalActivity("snapshot_captured", { artifact_path: result.artifact_path || result.artifactPath, run_id: result.run_id || result.runID }, result.message || "Snapshot captured.");
    await refreshArtifacts({ quiet: true });
    setFlash("success", `Snapshot captured: ${result.artifact_path || result.artifactPath || "report artifact"}.`);
  } catch (error) {
    reportIssue("capture snapshot", error);
  }
}

async function pauseAtSafePoint() {
  try {
    await window.orchestratorConsole.pauseAtSafePoint(activeRunID(), "operator_requested_pause_at_safe_point", state.address);
    await refreshStatus({ quiet: true });
    setFlash("success", "Pause requested for the next safe point.");
  } catch (error) {
    reportIssue("pause at safe point", error);
  }
}

function openSetupAutofill() {
  setActiveTab("files", { noScroll: true });
  scrollToSection("autofill-setup-panel");
  setFlash("info", "AI setup drafts project files and goal only. It previews generated content and does not start a build run.");
}

async function refreshSideChat(options = {}) {
  try {
    state.sideChat = await window.orchestratorConsole.listSideChatMessages(activeRepoPath(), 20, state.address);
    renderSideChat();
    if (!options.quiet) {
      setFlash("success", "Side chat messages refreshed.");
    }
  } catch (error) {
    reportIssue("side chat", error);
  }
}

async function refreshDogfoodIssues(options = {}) {
  try {
    state.dogfoodIssues = await window.orchestratorConsole.listDogfoodIssues(activeRepoPath(), 20, state.address);
    ensureSelectedDogfoodIssue();
    renderDogfoodIssues();
    if (!options.quiet) {
      setFlash("success", "Dogfood notes refreshed.");
    }
  } catch (error) {
    reportIssue("dogfood issues", error);
  }
}

async function refreshWorkers(options = {}) {
  try {
    state.workers = await window.orchestratorConsole.listWorkers(activeRunID(), 20, state.address);
    ensureSelectedWorker();
    renderWorkers();
    if (!options.quiet) {
      setFlash("success", "Workers refreshed.");
    }
  } catch (error) {
    reportIssue("workers", error);
  }
}

async function loadRuntimeConfig(options = {}) {
  try {
    state.runtimeConfig = await window.orchestratorConsole.getRuntimeConfig(state.address);
    renderRuntimeSettings();
    if (!options.quiet) {
      setFlash("success", "Runtime config loaded.");
    }
  } catch (error) {
    reportIssue("runtime config", error, "Runtime settings could not be loaded.");
  }
}

function timeoutPresetPatch(preset) {
  switch (String(preset || "normal")) {
    case "conservative":
      return {
        planner_request_timeout: "2m",
        executor_idle_timeout: "10m",
        executor_turn_timeout: "30m",
        subagent_timeout: "30m",
        shell_command_timeout: "10m",
        install_timeout: "45m",
        human_wait_timeout: "unlimited",
      };
    case "long_running":
      return {
        planner_request_timeout: "5m",
        executor_idle_timeout: "unlimited",
        executor_turn_timeout: "4h",
        subagent_timeout: "2h",
        shell_command_timeout: "1h",
        install_timeout: "4h",
        human_wait_timeout: "unlimited",
      };
    case "unlimited":
      return {
        planner_request_timeout: "unlimited",
        executor_idle_timeout: "unlimited",
        executor_turn_timeout: "unlimited",
        subagent_timeout: "unlimited",
        shell_command_timeout: "unlimited",
        install_timeout: "unlimited",
        human_wait_timeout: "unlimited",
      };
    default:
      return {
        planner_request_timeout: "2m",
        executor_idle_timeout: "unlimited",
        executor_turn_timeout: "unlimited",
        subagent_timeout: "unlimited",
        shell_command_timeout: "30m",
        install_timeout: "2h",
        human_wait_timeout: "unlimited",
      };
  }
}

async function saveRuntimeConfig() {
  try {
    const preset = refs.timeoutPreset ? refs.timeoutPreset.value : "custom";
    const presetTimeouts = preset === "custom" ? {} : timeoutPresetPatch(preset);
    const patch = {
      address: state.address,
      permission_profile: refs.permissionProfile ? refs.permissionProfile.value : "balanced",
      timeouts: {
        ...presetTimeouts,
        planner_request_timeout: refs.timeoutPlannerRequest ? refs.timeoutPlannerRequest.value : presetTimeouts.planner_request_timeout,
        executor_idle_timeout: refs.timeoutExecutorIdle ? refs.timeoutExecutorIdle.value : presetTimeouts.executor_idle_timeout,
        executor_turn_timeout: refs.timeoutExecutorTurn ? refs.timeoutExecutorTurn.value : presetTimeouts.executor_turn_timeout,
        subagent_timeout: refs.timeoutSubagent ? refs.timeoutSubagent.value : presetTimeouts.subagent_timeout,
        shell_command_timeout: refs.timeoutShellCommand ? refs.timeoutShellCommand.value : presetTimeouts.shell_command_timeout,
        install_timeout: refs.timeoutInstall ? refs.timeoutInstall.value : presetTimeouts.install_timeout,
        human_wait_timeout: refs.timeoutHumanWait ? refs.timeoutHumanWait.value : presetTimeouts.human_wait_timeout,
      },
    };
    state.runtimeConfig = await window.orchestratorConsole.setRuntimeConfig(patch);
    if (state.snapshot) {
      state.snapshot.timeouts = state.runtimeConfig.timeouts;
      state.snapshot.permissions = state.runtimeConfig.permissions;
    }
    renderRuntimeSettings();
    setFlash("success", "Runtime settings saved. Timeout changes apply to future operations or the next safe boundary where needed.");
  } catch (error) {
    reportIssue("runtime config", error, "Runtime settings could not be saved.");
  }
}

async function checkForUpdates() {
  try {
    state.updateStatus = await window.orchestratorConsole.checkForUpdates(false, state.address);
    renderUpdateStatus();
    setFlash(state.updateStatus.update_available ? "success" : "info", state.updateStatus.update_available ? `Update available: ${state.updateStatus.latest_version}` : "No update found for the selected channel.");
  } catch (error) {
    reportIssue("updates", error, "Update check failed.");
  }
}

async function copyUpdateChangelog() {
  const status = state.updateStatus || {};
  const text = status.changelog || "No update changelog has been loaded yet. Click Check for Updates first.";
  await copyText(text, "Update changelog copied.");
}

async function refreshStatus(options = {}) {
  try {
    state.snapshot = await window.orchestratorConsole.getStatusSnapshot("", state.address);
    applyModelHealthNormalization();
    state.lastRefreshedAt = new Date().toISOString();
    await hydrateProtocolBackedPanels(Boolean(options.refreshContracts));
    renderAll();
    maybeFocusActionRequired();
    if (!options.quiet) {
      setFlash("success", "Dashboard updated.");
    }
  } catch (error) {
    reportIssue("status", error, "Try reconnecting if the control server was restarted.");
  }
}

async function refreshArtifacts(options = {}) {
  try {
    state.artifacts = await window.orchestratorConsole.listRecentArtifacts(activeRunID(), "", 12, state.address);
    if (state.selectedArtifactPath) {
      await openArtifactByPath(state.selectedArtifactPath, { quiet: true });
      if (state.artifactContent && state.artifactContent.available === false) {
        state.selectedArtifactPath = "";
        state.artifactContent = null;
      }
    } else if (state.artifacts.items && state.artifacts.items.length > 0) {
      state.selectedArtifactPath = state.artifacts.items[0].path;
      await openArtifactByPath(state.selectedArtifactPath, { quiet: true });
    } else {
      state.artifactContent = null;
    }
    renderHomeDashboard();
    renderArtifacts();
    renderRunSummary();
    if (!options.quiet) {
      setFlash("success", "Outputs reloaded.");
    }
  } catch (error) {
    reportIssue("artifacts", error);
  }
}

async function refreshRepoTree(path = currentRepoTreePath(), options = {}) {
  try {
    state.repoTree = await window.orchestratorConsole.listRepoTree(activeRepoPath(), path, 200, state.address);
    if (state.repoFile && state.selectedRepoPath && !state.selectedRepoPath.startsWith(path || "")) {
      state.repoFile = null;
      state.selectedRepoPath = "";
    } else if (state.selectedRepoPath && !state.repoTree.items.some((item) => item.path === state.selectedRepoPath || state.selectedRepoPath.startsWith(`${item.path}/`))) {
      state.repoFile = null;
      state.selectedRepoPath = "";
    }
    renderRepoTree();
    if (!options.quiet) {
      setFlash("success", `Repo tree refreshed for ${path || "repo root"}.`);
    }
  } catch (error) {
    reportIssue("repo browser", error);
  }
}

async function applyVerbosity() {
  try {
    await window.orchestratorConsole.setVerbosity(refs.verbositySelect.value, state.address);
    recordLocalActivity("verbosity_changed", { verbosity: refs.verbositySelect.value }, `Verbosity changed to ${refs.verbositySelect.value}.`);
    await refreshStatus({ quiet: true });
    renderEvents();
    setFlash("success", `Verbosity updated to ${refs.verbositySelect.value}.`);
  } catch (error) {
    reportIssue("verbosity", error);
  }
}

async function testPlannerModelFromSettings() {
  state.modelTests.inFlight = "planner";
  state.modelTests.error = "";
  renderModelHealth();
  try {
    const result = await window.orchestratorConsole.testPlannerModel("", state.address);
    state.modelTests.planner = result;
    recordLocalActivity("model_health_tested", { component: "planner" }, "Planner model health was tested.");
    if (!state.snapshot) {
      state.snapshot = { model_health: result };
    }
    applyModelHealthNormalization();
    renderAll();
    setFlash("success", "Planner model test completed.");
  } catch (error) {
    state.modelTests.error = error.message;
    recordLocalActivity("model_health_failed", { component: "planner", error: error.message }, `Planner model health check failed: ${error.message}`);
    reportIssue("planner model test", error);
  } finally {
    state.modelTests.inFlight = "";
    renderModelHealth();
  }
}

async function testExecutorModelFromSettings() {
  state.modelTests.inFlight = "executor";
  state.modelTests.error = "";
  renderModelHealth();
  try {
    const result = await window.orchestratorConsole.testExecutorModel("", state.address);
    state.modelTests.executor = result;
    recordLocalActivity("model_health_tested", { component: "executor" }, "Codex configuration health was tested.");
    if (!state.snapshot) {
      state.snapshot = { model_health: result };
    }
    applyModelHealthNormalization();
    renderAll();
    setFlash("success", "Codex configuration test completed.");
  } catch (error) {
    state.modelTests.error = error.message;
    recordLocalActivity("model_health_failed", { component: "executor", error: error.message }, `Codex configuration health check failed: ${error.message}`);
    reportIssue("Codex config test", error);
  } finally {
    state.modelTests.inFlight = "";
    renderModelHealth();
  }
}

async function autoTestModelHealth(trigger = "connect") {
  if (!state.connection.connected || state.modelTests.inFlight !== "") {
    return;
  }
  state.modelTests.inFlight = "auto";
  state.modelTests.error = "";
  recordLocalActivity("model_health_check_started", { trigger }, "Checking planner model and Codex access...");
  renderModelHealth();
  renderHomeDashboard();

  const [plannerResult, executorResult] = await Promise.allSettled([
    window.orchestratorConsole.testPlannerModel("", state.address),
    window.orchestratorConsole.testExecutorModel("", state.address),
  ]);

  if (plannerResult.status === "fulfilled") {
    state.modelTests.planner = plannerResult.value;
    recordLocalActivity("model_health_tested", { component: "planner", trigger }, "Planner model health was tested automatically.");
  } else {
    state.modelTests.error = plannerResult.reason && plannerResult.reason.message ? plannerResult.reason.message : String(plannerResult.reason || "planner model test failed");
    recordLocalActivity("model_health_failed", { component: "planner", error: state.modelTests.error, trigger }, `Planner model health check failed: ${state.modelTests.error}`);
  }

  if (executorResult.status === "fulfilled") {
    state.modelTests.executor = executorResult.value;
    recordLocalActivity("model_health_tested", { component: "executor", trigger }, "Codex configuration health was tested automatically.");
  } else {
    const message = executorResult.reason && executorResult.reason.message ? executorResult.reason.message : String(executorResult.reason || "Codex config test failed");
    state.modelTests.error = state.modelTests.error ? `${state.modelTests.error}; ${message}` : message;
    recordLocalActivity("model_health_failed", { component: "executor", error: message, trigger }, `Codex configuration health check failed: ${message}`);
  }

  applyModelHealthNormalization();
  state.modelTests.inFlight = "";
  renderAll();
  if (state.modelTests.error) {
    setFlash("error", `Model health check needs attention: ${state.modelTests.error}`);
    maybeFocusActionRequired();
  } else {
    setFlash("success", "Planner and Codex model checks completed.");
  }
}

async function safeStop() {
  try {
    await window.orchestratorConsole.stopSafe(activeRunID(), "operator_requested_safe_stop_from_shell", state.address);
    await refreshStatus({ quiet: true });
    setFlash("success", "Safe stop flag written.");
  } catch (error) {
    reportIssue("safe stop", error);
  }
}

async function clearStop() {
  try {
    await window.orchestratorConsole.clearStop(activeRunID(), state.address);
    await refreshStatus({ quiet: true });
    setFlash("success", "Safe stop flag cleared.");
  } catch (error) {
    reportIssue("clear stop", error);
  }
}

async function clearStopAndContinue() {
  try {
    state.actionRequired.status = "Clearing safe stop and preparing to continue...";
    renderApproval();
    await window.orchestratorConsole.clearStop(activeRunID(), state.address);
    await refreshStatus({ quiet: true });
    state.actionRequired.status = "Safe stop cleared. Continuing the run...";
    setFlash("success", "Safe stop cleared. Continuing through the engine protocol...");
    await continueRunFromHome({ skipAskHumanGuard: false });
  } catch (error) {
    state.actionRequired.status = `Clear Stop and Continue failed: ${error.message}`;
    reportIssue("clear stop and continue", error);
  } finally {
    renderApproval();
  }
}

function currentAskHumanViewModel() {
  return window.OrchestratorViewModel.buildAskHumanViewModel(state.snapshot);
}

async function queueAskHumanAnswer(message, source = "action_required_answer") {
  const trimmed = String(message || "").trim();
  if (trimmed === "") {
    throw new Error("Type an answer before sending.");
  }
  const queued = await window.orchestratorConsole.injectControlMessage({
    runId: activeRunID(),
    message: trimmed,
    source,
    reason: "ask_human_answer",
    address: state.address,
  });
  state.actionRequired.queuedAskHumanAnswer = {
    id: queued && queued.id ? queued.id : "",
    runID: activeRunID(),
    queuedAt: new Date().toISOString(),
  };
  recordLocalActivity("control_message_queued", {
    run_id: activeRunID(),
    source,
    reason: "ask_human_answer",
  }, "Planner answer queued for the next safe point.");
  return queued;
}

async function sendAskHumanAnswer(options = {}) {
  const continueAfter = Boolean(options.continueAfter);
  const askHuman = currentAskHumanViewModel();
  if (!askHuman.present) {
    setFlash("info", "No planner question is waiting for an answer right now.");
    return;
  }
  const message = refs.askHumanAnswer.value.trim();
  if (message === "") {
    setFlash("error", "Type an answer before sending.");
    refs.askHumanAnswer.focus();
    return;
  }

  state.actionRequired.inFlight = true;
  state.actionRequired.status = continueAfter ? "Queueing answer, then continuing the run..." : "Queueing answer...";
  renderApproval();
  try {
    const queued = await queueAskHumanAnswer(message, "action_required_answer");
    refs.askHumanAnswer.value = "";
    state.actionRequired.status = queued && queued.id
      ? `Answer queued as ${queued.id}.`
      : "Answer queued.";
    await refreshStatus({ quiet: true });
    if (continueAfter) {
      state.actionRequired.status = "Answer queued. Continuing run...";
      renderApproval();
      await continueRunFromHome({ skipAskHumanGuard: true });
    } else {
      setFlash("success", `${state.actionRequired.status} Click Continue with Queued Answer when you are ready.`);
    }
  } catch (error) {
    state.actionRequired.status = `Failed: ${error.message || error}`;
    reportIssue("ask_human answer", error);
  } finally {
    state.actionRequired.inFlight = false;
    renderApproval();
    renderHomeDashboard();
  }
}

async function continueQueuedAskHumanAnswer() {
  const askHuman = currentAskHumanViewModel();
  if (!askHuman.present || !state.actionRequired.queuedAskHumanAnswer) {
    setFlash("info", "No queued planner answer is ready to continue.");
    return;
  }
  state.actionRequired.status = "Continuing run with queued answer...";
  renderApproval();
  try {
    await continueRunFromHome({ skipAskHumanGuard: true });
  } catch (error) {
    state.actionRequired.status = `Continue failed: ${error.message || error}`;
    reportIssue("continue queued ask_human answer", error);
    renderApproval();
  }
}

async function sendControlMessage() {
  const message = refs.controlMessageInput.value.trim();
  if (message === "") {
    setFlash("error", "Enter a control message before sending.");
    return;
  }

  const askHuman = currentAskHumanViewModel();
  try {
    const queued = askHuman.present
      ? await queueAskHumanAnswer(message, "control_chat")
      : await window.orchestratorConsole.injectControlMessage({
        runId: activeRunID(),
        message,
        source: "control_chat",
        reason: "operator_intervention_from_shell",
        address: state.address,
      });
    refs.controlMessageInput.value = "";
    await refreshStatus({ quiet: true });
    if (askHuman.present) {
      state.actionRequired.status = queued && queued.id
        ? `Control Chat answer queued as ${queued.id}. Click Continue with Queued Answer in Action Required.`
        : "Control Chat answer queued. Click Continue with Queued Answer in Action Required.";
      setActiveTab("attention");
      renderApproval();
      setFlash("success", state.actionRequired.status);
    } else {
      setFlash("success", `Queued control message ${queued.id || ""}`.trim());
    }
  } catch (error) {
    reportIssue("control chat", error);
  }
}

async function sendSideChatMessage() {
  const message = refs.sideChatMessageInput.value.trim();
  if (message === "") {
    setFlash("error", "Enter a side chat message before sending.");
    return;
  }

  try {
    const response = await window.orchestratorConsole.sendSideChatMessage({
      repoPath: activeRepoPath(),
      message,
      contextPolicy: refs.sideChatContextPolicy.value,
      address: state.address,
    });
    refs.sideChatMessageInput.value = "";
    await refreshSideChat({ quiet: true });
    setFlash("info", response.message || "Side note recorded. It did not affect the active run.");
  } catch (error) {
    reportIssue("side chat", error);
  }
}

async function askSideChatQuick(message) {
  if (refs.sideChatMessageInput) {
    refs.sideChatMessageInput.value = message;
  }
  try {
    const response = await window.orchestratorConsole.sendSideChatMessage({
      repoPath: activeRepoPath(),
      message,
      contextPolicy: refs.sideChatContextPolicy.value,
      address: state.address,
    });
    await refreshSideChat({ quiet: true });
    setFlash("info", response.message || "Side Chat answered from current runtime context.");
  } catch (error) {
    reportIssue("side chat quick action", error);
  }
}

async function requestSideChatAction(action, message, options = {}) {
  try {
    const response = await window.orchestratorConsole.sideChatActionRequest({
      repoPath: activeRepoPath(),
      runId: activeRunID(),
      action,
      message,
      source: options.source || "operator_quick_action",
      reason: options.reason || "side_chat_quick_action",
      approved: options.approved !== false,
      address: state.address,
    });
    if (action === "safe_stop" || action === "request_safe_stop") {
      await refreshStatus({ quiet: true });
    }
    setFlash(response.requires_approval ? "info" : "success", response.message || "Side Chat action recorded.");
  } catch (error) {
    reportIssue("side chat action", error);
  }
}

async function copySideChatConversation() {
  const vm = window.OrchestratorViewModel.buildSideChatViewModel(state.sideChat);
  const text = vm.items.length === 0
    ? "No side chat conversation is recorded yet."
    : vm.items.map((item) => [
      `[${item.createdAt}] ${item.source}`,
      item.rawText,
      `Reply: ${item.responseMessage}`,
      `status=${item.status}; backend=${item.backendState}; run=${item.runID}`,
    ].join("\n")).join("\n\n---\n\n");
  await copyText(text, "Side Chat conversation copied.");
}

async function copySideChatItem(index) {
  const vm = window.OrchestratorViewModel.buildSideChatViewModel(state.sideChat);
  const item = vm.items[index];
  if (!item) {
    setFlash("info", "That Side Chat item is no longer available.");
    return;
  }
  await copyText([
    `[${item.createdAt}] ${item.source}`,
    item.rawText,
    `Reply: ${item.responseMessage}`,
    `status=${item.status}; backend=${item.backendState}; run=${item.runID}; context=${item.contextPolicy}`,
  ].join("\n"), "Side Chat item copied.");
}

async function captureDogfoodIssue() {
  const title = refs.dogfoodTitleInput.value.trim();
  const note = refs.dogfoodNoteInput.value.trim();
  if (title === "" || note === "") {
    setFlash("error", "Enter both a short title and a note before capturing a dogfood issue.");
    return;
  }

  try {
    const response = await window.orchestratorConsole.captureDogfoodIssue({
      repoPath: activeRepoPath(),
      runId: activeRunID(),
      title,
      note,
      source: "operator_shell",
      address: state.address,
    });
    refs.dogfoodTitleInput.value = "";
    refs.dogfoodNoteInput.value = "";
    await refreshDogfoodIssues({ quiet: true });
    if (response && response.entry && response.entry.id) {
      state.selectedDogfoodIssueID = response.entry.id;
      renderDogfoodIssues();
    }
    setFlash("success", response.message || "Dogfood issue recorded.");
  } catch (error) {
    reportIssue("dogfood issue capture", error);
  }
}

async function createWorker() {
  const name = refs.workerCreateName.value.trim();
  const scope = refs.workerCreateScope.value.trim();
  if (name === "" || scope === "") {
    setFlash("error", "Worker name and scope are required before creating a worker.");
    return;
  }

  try {
    const result = await window.orchestratorConsole.createWorker({
      runId: activeRunID(),
      name,
      scope,
      address: state.address,
    });
    state.workerActionResult = result;
    state.selectedWorkerID = result && result.worker ? result.worker.worker_id || "" : state.selectedWorkerID;
    refs.workerCreateName.value = "";
    await refreshStatus({ quiet: true });
    if (result && result.worker && result.worker.worker_id && !refs.workerIntegrateIds.value.trim()) {
      refs.workerIntegrateIds.value = result.worker.worker_id;
    }
    setFlash("success", result.message || "Worker created.");
  } catch (error) {
    reportIssue("create worker", error);
  }
}

async function dispatchWorker() {
  const workerID = String(state.selectedWorkerID || "").trim();
  const prompt = refs.workerDispatchPrompt.value.trim();
  if (workerID === "") {
    setFlash("error", "Select a worker before dispatching.");
    return;
  }
  if (prompt === "") {
    setFlash("error", "Enter a bounded worker prompt before dispatching.");
    return;
  }

  try {
    const result = await window.orchestratorConsole.dispatchWorker({
      workerId: workerID,
      prompt,
      address: state.address,
    });
    state.workerActionResult = result;
    await refreshStatus({ quiet: true });
    setFlash("success", result.message || "Worker dispatch finished.");
  } catch (error) {
    reportIssue("dispatch worker", error);
  }
}

async function removeWorker() {
  const workerID = String(state.selectedWorkerID || "").trim();
  if (workerID === "") {
    setFlash("error", "Select a worker before removing it.");
    return;
  }

  try {
    const result = await window.orchestratorConsole.removeWorker(workerID, state.address);
    state.workerActionResult = result;
    if (state.selectedWorkerID === workerID) {
      state.selectedWorkerID = "";
    }
    refs.workerDispatchPrompt.value = "";
    await refreshStatus({ quiet: true });
    setFlash("success", result.message || "Worker removed.");
  } catch (error) {
    reportIssue("remove worker", error);
  }
}

async function integrateWorkers() {
  const workerIds = parseWorkerIntegrateIDs(refs.workerIntegrateIds.value);
  if (workerIds.length === 0) {
    setFlash("error", "Enter one or more worker IDs before building an integration preview.");
    return;
  }

  try {
    const result = await window.orchestratorConsole.integrateWorkers(workerIds, state.address);
    state.workerActionResult = result;
    await refreshStatus({ quiet: true });
    if (result && result.artifact_path) {
      state.selectedArtifactPath = result.artifact_path;
      await openArtifactByPath(result.artifact_path, { quiet: true });
    }
    setFlash("success", result.message || "Integration preview artifact generated.");
  } catch (error) {
    reportIssue("integrate workers", error);
  }
}

async function approveExecutor() {
  const vm = window.OrchestratorViewModel.buildApprovalViewModel(state.snapshot);
  if (!vm.canApprove) {
    setFlash("info", "No actionable executor approval is waiting right now.");
    return;
  }
  try {
    const result = await window.orchestratorConsole.approveExecutor(activeRunID(), state.address);
    await refreshStatus({ quiet: true });
    setFlash("success", result.summary || "Executor approval granted.");
  } catch (error) {
    reportIssue("approve executor", error);
  }
}

async function denyExecutor() {
  const vm = window.OrchestratorViewModel.buildApprovalViewModel(state.snapshot);
  if (!vm.canDeny) {
    setFlash("info", "No actionable executor approval is waiting right now.");
    return;
  }
  try {
    const result = await window.orchestratorConsole.denyExecutor(activeRunID(), state.address);
    await refreshStatus({ quiet: true });
    setFlash("success", result.summary || "Executor approval denied.");
  } catch (error) {
    reportIssue("deny executor", error);
  }
}

async function copyApprovalDetails() {
  const vm = window.OrchestratorViewModel.buildApprovalViewModel(state.snapshot);
  const details = {
    state: vm.state,
    kind: vm.kind,
    run_id: vm.runID,
    executor_thread_id: vm.executorThreadID,
    executor_turn_id: vm.executorTurnID,
    command: vm.command,
    cwd: vm.cwd,
    grant_root: vm.grantRoot,
    worker_approval_required: vm.workerApprovalRequired,
    last_control_action: vm.lastControlAction,
  };
  try {
    await navigator.clipboard.writeText(JSON.stringify(details, null, 2));
    setFlash("success", "Copied approval technical details.");
  } catch (error) {
    reportIssue("approval details", error, "Copying details requires clipboard access.");
  }
}

async function copyDebugBundle() {
  try {
    const text = window.OrchestratorViewModel.buildDebugBundleText(state.snapshot, state.artifacts, state.events, {
      now: new Date().toISOString(),
      connection: state.connection,
      address: state.address,
      expectedRepoPath: state.expectedRepoPath,
      sideChat: state.sideChat,
    });
    await navigator.clipboard.writeText(text);
    setFlash("success", "Copied safe run debug bundle. Secrets and full artifacts are excluded.");
    recordLocalActivity("debug_bundle_copied", { run_id: activeRunID() }, "Debug bundle copied for support.");
  } catch (error) {
    reportIssue("debug bundle", error, "Copying the debug bundle requires clipboard access.");
  }
}

async function copyModelHealth() {
  try {
    applyModelHealthNormalization();
    const text = window.OrchestratorViewModel.buildModelHealthBundleText(state.snapshot, {
      now: new Date().toISOString(),
      address: state.address,
      expectedRepoPath: state.expectedRepoPath,
    });
    await navigator.clipboard.writeText(text);
    setFlash("success", "Copied safe model-health bundle. Secrets are excluded.");
    recordLocalActivity("model_health_bundle_copied", { run_id: activeRunID() }, "Model health bundle copied for support.");
  } catch (error) {
    reportIssue("model health bundle", error, "Copying model health requires clipboard access.");
  }
}

function wait(ms) {
  return new Promise((resolve) => window.setTimeout(resolve, ms));
}

function staleActiveRunGuard() {
  const guard = state.snapshot && state.snapshot.active_run_guard ? state.snapshot.active_run_guard : null;
  if (!guard || guard.present !== true || guard.stale !== true) {
    return null;
  }
  return guard;
}

async function refreshAfterRecovery() {
  await refreshStatus({ quiet: true, refreshContracts: true });
  await Promise.allSettled([
    refreshArtifacts({ quiet: true }),
    refreshWorkers({ quiet: true }),
    refreshDogfoodIssues({ quiet: true }),
    refreshSideChat({ quiet: true }),
    autoTestModelHealth("backend_recovery"),
  ]);
}

async function recoverStaleRunGuard(reason = "operator_recovery") {
  const guard = staleActiveRunGuard();
  const runID = (guard && (guard.run_id || guard.runID)) || activeRunID() || "";
  const recovery = await window.orchestratorConsole.recoverStaleRun(runID, reason, true, state.address);
  recordLocalActivity(
    "stale_run_recovered",
    recovery,
    recovery && recovery.message ? recovery.message : "Stale active-run guard recovery completed.",
  );
  if (!recovery || recovery.active_guard_cleared !== true) {
    throw new Error(recovery && recovery.message ? recovery.message : "recover_stale_run completed, but the active-run guard was not cleared.");
  }
  return recovery;
}

async function restartBackend() {
  try {
    const guardBefore = staleActiveRunGuard();
    const backend = state.snapshot && state.snapshot.backend ? state.snapshot.backend : {};
    if (guardBefore && backend.stale !== true) {
      setFlash("info", "Clearing stale active-run guard...");
      const recovery = await recoverStaleRunGuard("operator_recovery");
      await refreshAfterRecovery();
      if (staleActiveRunGuard()) {
        setFlash("error", "recover_stale_run finished, but the stale active-run guard is still present. Check Live Output for details.");
        return;
      }
      setFlash("success", recovery.message || "Recovered stale active run guard. Continue Build is now available.");
      return;
    }

    setFlash("info", "Recovering backend and unlocking repo...");
    const result = await window.orchestratorConsole.restartBackend(state.address);
    if (!result || result.restarted !== true) {
      if (guardBefore) {
        try {
          const recovery = await recoverStaleRunGuard("operator_recovery");
          await refreshAfterRecovery();
          setFlash("success", recovery.message || "Recovered stale active run guard. Continue Build is now available.");
          return;
        } catch (recoveryError) {
          setFlash("error", `Backend restart was unavailable, and recover_stale_run failed: ${recoveryError.message}`);
          return;
        }
      }
      setFlash("error", result && result.message ? result.message : "Backend restart is unavailable in this launch mode.");
      return;
    }
    state.connection = {
      connected: false,
      status: "reconnecting",
      address: state.address,
      message: result.message || "backend restarted",
    };
    renderConnection();
    recordLocalActivity("backend_recovery_started", { pid: result.pid }, result.message || "Owned backend restarted for recovery.");
    await wait(1200);
    await connect({ quiet: true, automatic: true, trigger: "backend_recovered" });
    let recovery = null;
    if (staleActiveRunGuard()) {
      try {
        recovery = await recoverStaleRunGuard("operator_recovery");
      } catch (recoveryError) {
        recordLocalActivity("stale_run_recovery_failed", { error: recoveryError.message }, "Backend restarted, but stale active-run recovery failed.");
        throw recoveryError;
      }
    }
    await refreshAfterRecovery();
    if (staleActiveRunGuard()) {
      setFlash("error", "Backend restarted, but the stale active-run guard is still present. recover_stale_run did not clear it.");
      return;
    }
    setFlash("success", recovery && recovery.message ? recovery.message : "Backend recovered and dashboard refreshed.");
  } catch (error) {
    reportIssue("recover backend", error, "Recover Backend is only available for a dogfood-owned backend process. Unknown processes are never killed automatically.");
  }
}

async function copyLatestError() {
  const latest = window.OrchestratorViewModel.buildLatestErrorViewModel(state.snapshot, state.events);
  if (!latest.present) {
    setFlash("info", "No latest error is available to copy.");
    return;
  }
  try {
    await navigator.clipboard.writeText(latest.message);
    setFlash("success", "Copied latest error.");
  } catch (error) {
    reportIssue("latest error", error, "Copying the latest error requires clipboard access.");
  }
}

function openLiveOutput() {
  setActiveTab("activity");
}

function handleSummaryAction(action) {
  switch (action) {
    case "copy_debug_bundle":
      void copyDebugBundle();
      return;
    case "test_model_health":
      setActiveTab("settings");
      void testExecutorModelFromSettings();
      return;
    case "continue_run":
      void continueRunFromHome();
      return;
    case "open_latest_artifact":
      void openLatestArtifactFromHome();
      return;
    case "open_live_output":
      openLiveOutput();
      return;
    case "recover_backend":
      void restartBackend();
      return;
    case "clear_stop_continue":
      void clearStopAndContinue();
      return;
    case "start_run":
      setActiveTab("home");
      if (refs.homeGoal) {
        refs.homeGoal.focus();
      }
      return;
    default:
      setFlash("info", "That summary action is not wired yet.");
  }
}

function syncAutofillAnswersFromFields() {
  state.autofill.answers = {
    project_summary: refs.autofillProjectSummary.value.trim(),
    desired_outcome: refs.autofillDesiredOutcome.value.trim(),
    users_platform: refs.autofillUsersPlatform.value.trim(),
    constraints: refs.autofillConstraints.value.trim(),
    milestones: refs.autofillMilestones.value.trim(),
    decisions: refs.autofillDecisions.value.trim(),
    notes: refs.autofillNotes.value.trim(),
  };
  state.autofill.targets = Array.from(document.querySelectorAll("[data-autofill-target]"))
    .filter((input) => input.checked)
    .map((input) => input.getAttribute("data-autofill-target"))
    .filter(Boolean);
}

async function runAutofill() {
  syncAutofillAnswersFromFields();
  if (state.autofill.targets.length === 0) {
    setFlash("error", "Select at least one contract file target before drafting.");
    return;
  }

  try {
    state.autofill.result = await window.orchestratorConsole.runAIAutofill({
      repoPath: activeRepoPath(),
      targets: state.autofill.targets,
      answers: state.autofill.answers,
      address: state.address,
    });
    state.autofill.selectedDraftPath = state.autofill.result.files && state.autofill.result.files.length > 0
      ? state.autofill.result.files[0].path
      : "";
    renderAutofill();
    setFlash("success", "Autofill draft generated. Review it before saving.");
  } catch (error) {
    reportIssue("autofill", error);
  }
}

async function saveSelectedAutofillDraft() {
  const draft = selectedAutofillDraft();
  if (!draft) {
    setFlash("error", "Select a drafted contract file before saving.");
    return;
  }

  try {
    await window.orchestratorConsole.saveContractFile({
      path: draft.path,
      repoPath: activeRepoPath(),
      address: state.address,
      content: draft.content,
      expectedMTime: "",
    });
    await refreshStatus({ quiet: true, refreshContracts: true });
    await openContractByPath(draft.path, { quiet: true });
    await refreshRepoTree(currentRepoTreePath(), { quiet: true });
    setFlash("success", `Saved autofill draft to ${draft.path}.`);
  } catch (error) {
    reportIssue("save autofill draft", error);
  }
}

async function openRepoFileByPath(path, options = {}) {
  if (!path) {
    return;
  }

  try {
    state.selectedRepoPath = path;
    state.repoFile = await window.orchestratorConsole.openRepoFile(path, activeRepoPath(), state.address);
    renderRepoTree();
    if (!options.quiet) {
      setFlash("success", `Opened repo file ${path}.`);
    }
  } catch (error) {
    reportIssue("repo file", error);
  }
}

async function openRepoEntry(path, kind) {
  if (kind === "directory") {
    state.selectedRepoPath = path;
    state.repoFile = null;
    await refreshRepoTree(path);
    return;
  }
  await openRepoFileByPath(path);
}

async function openRepoSelectionInContractEditor() {
  if (!state.repoFile || !state.repoFile.editable_via_contract_editor) {
    setFlash("error", "The selected repo file is not editable through the contract editor in this slice.");
    return;
  }
  await openContractByPath(state.repoFile.path);
}

async function createTerminalTab() {
  try {
    state.terminal = await window.orchestratorConsole.startTerminal(activeRepoPath());
    renderTerminal();
    setFlash("success", "Terminal tab started.");
  } catch (error) {
    reportIssue("terminal", error);
  }
}

async function activateTerminalTab(sessionID) {
  try {
    state.terminal = await window.orchestratorConsole.activateTerminalSession(sessionID);
    renderTerminal();
  } catch (error) {
    reportIssue("terminal", error);
  }
}

async function stopTerminal() {
  try {
    state.terminal = await window.orchestratorConsole.stopTerminal(activeTerminalSessionID());
    renderTerminal();
    setFlash("success", "Terminal session stopped.");
  } catch (error) {
    reportIssue("terminal", error);
  }
}

async function closeTerminalTab() {
  try {
    const sessionID = activeTerminalSessionID();
    if (!sessionID) {
      setFlash("error", "No terminal tab is selected.");
      return;
    }
    state.terminal = await window.orchestratorConsole.closeTerminal(sessionID);
    renderTerminal();
    setFlash("success", "Terminal tab closed.");
  } catch (error) {
    reportIssue("terminal", error);
  }
}

async function clearTerminal() {
  try {
    state.terminal = await window.orchestratorConsole.clearTerminal(activeTerminalSessionID());
    renderTerminal();
    setFlash("success", "Terminal output cleared.");
  } catch (error) {
    reportIssue("terminal", error);
  }
}

async function sendTerminalInput() {
  const input = refs.terminalInput.value;
  if (!input.trim()) {
    setFlash("error", "Enter a shell command or input before sending.");
    return;
  }

  try {
    state.terminal = await window.orchestratorConsole.sendTerminalInput(`${input}\n`, activeTerminalSessionID());
    refs.terminalInput.value = "";
    renderTerminal();
  } catch (error) {
    reportIssue("terminal", error);
  }
}

async function copyEventPayload(index) {
  const vm = window.OrchestratorViewModel.buildActivityTimelineViewModel(state.events, {
    searchText: state.activityFilters.searchText,
    currentRunOnly: state.activityFilters.currentRunOnly,
    currentRunID: activeRunID(),
    categories: state.activityFilters.categories,
  });
  const event = vm.items[index];
  if (!event) {
    setFlash("error", "Activity event is unavailable.");
    return;
  }

  try {
    await navigator.clipboard.writeText(event.payloadText);
    setFlash("success", "Copied event payload.");
  } catch (error) {
    reportIssue("activity timeline", error, "Copying the payload requires clipboard access.");
  }
}

async function copyAuroraEventPayload(index) {
  const vm = window.OrchestratorViewModel.buildActivityTimelineViewModel(state.events, {
    currentRunOnly: false,
    categories: timelineCategoriesForAurora(),
    verbosity: refs.verbositySelect ? refs.verbositySelect.value : "normal",
  });
  const event = vm.items[index];
  if (!event) {
    setFlash("error", "Timeline event is unavailable.");
    return;
  }

  try {
    await navigator.clipboard.writeText(event.payloadText);
    setFlash("success", "Copied timeline details.");
  } catch (error) {
    reportIssue("timeline", error, "Copying the payload requires clipboard access.");
  }
}

async function openArtifactByPath(path, options = {}) {
  if (!path) {
    return;
  }

  try {
    state.selectedArtifactPath = path;
    state.artifactContent = await window.orchestratorConsole.getArtifact(path, state.address);
    renderHomeDashboard();
    renderArtifacts();
    if (!options.quiet) {
      setFlash("success", `Opened artifact ${path}.`);
    }
  } catch (error) {
    reportIssue("artifact viewer", error);
  }
}

async function openContractByPath(path, options = {}) {
  if (!path) {
    return;
  }

  try {
    state.selectedContractPath = path;
    state.contractOpenFile = await window.orchestratorConsole.openContractFile(path, activeRepoPath(), state.address);
    renderContractFiles();
    if (!options.quiet) {
      setFlash("success", `Opened contract file ${path}.`);
    }
  } catch (error) {
    reportIssue("contract file", error);
  }
}

async function saveCurrentContract() {
  if (!state.selectedContractPath) {
    setFlash("error", "Select a contract file before saving.");
    return;
  }

  try {
    const result = await window.orchestratorConsole.saveContractFile({
      path: state.selectedContractPath,
      repoPath: activeRepoPath(),
      address: state.address,
      content: refs.contractEditor.value,
      expectedMTime: state.contractOpenFile && state.contractOpenFile.modified_at ? state.contractOpenFile.modified_at : "",
    });
    state.contractOpenFile = {
      path: result.path,
      exists: true,
      content: refs.contractEditor.value,
      modified_at: result.modified_at,
      byte_size: result.byte_size,
    };
    state.contractFiles = await window.orchestratorConsole.listContractFiles(activeRepoPath(), state.address);
    await refreshRepoTree(currentRepoTreePath(), { quiet: true });
    renderHomeDashboard();
    renderContractFiles();
    setFlash("success", `Saved ${result.path}.`);
  } catch (error) {
    reportIssue("save contract", error, "If the file changed externally, reopen it and try again.");
  }
}

function scheduleSoftRefresh() {
  if (state.refreshTimer) {
    window.clearTimeout(state.refreshTimer);
  }
  state.refreshTimer = window.setTimeout(() => {
    state.refreshTimer = null;
    void refreshStatus({ quiet: true });
  }, 300);
}

function fillSuggestedGoal() {
  refs.homeGoal.value = suggestedDefaultGoal();
  renderHomeDashboard();
  setFlash("info", "Suggested default goal filled. Edit it before starting the run if needed.");
}

async function openLatestArtifactFromHome() {
  const artifactPath = latestArtifactPath();
  if (!artifactPath) {
    setFlash("error", "No latest artifact is available yet. Refresh artifacts after planner/executor turns complete.");
    return;
  }
  await openArtifactByPath(artifactPath);
  scrollToSection("artifacts-panel");
}

function prepareStartRunCommand() {
  void prepareTerminalCommand(buildStartRunCommand());
}

function prepareContinueRunCommand() {
  void prepareTerminalCommand("orchestrator continue");
}

function knownBlockingModelIssue() {
  applyModelHealthNormalization();
  const health = state.snapshot && state.snapshot.model_health ? state.snapshot.model_health : {};
  const planner = health.planner || {};
  const executor = health.executor || {};
  const codex = window.OrchestratorViewModel.buildCodexReadinessViewModel(state.snapshot);
  const latestPlannerTest = state.modelTests.planner && state.modelTests.planner.planner
    ? state.modelTests.planner.planner
    : null;
  const latestExecutorTest = state.modelTests.executor && state.modelTests.executor.executor
    ? state.modelTests.executor.executor
    : null;
  const plannerVerified = (latestPlannerTest && latestPlannerTest.verification_state === "verified")
    || planner.verification_state === "verified";
  const executorVerified = (latestExecutorTest && latestExecutorTest.verification_state === "verified")
    || executor.verification_state === "verified";
  const fullAccessVerified = Boolean((latestExecutorTest && latestExecutorTest.codex_permission_mode_verified)
    || executor.codex_permission_mode_verified);

  if (health.blocking) {
    return health.message || "Model or Codex requirements are blocking this run. Open Settings and run the model health checks.";
  }
  if (planner.verification_state === "invalid") {
    return "Planner model is below the required gpt-5.4 minimum or unavailable. Set a valid planner model and run Test Planner Model before starting or continuing.";
  }
  if (!plannerVerified) {
    return "Planner model has not been verified in this shell session. Run Test Planner Model before starting or continuing a serious autonomous build.";
  }
  if (!codex.modelInvalid) {
    if (!executorVerified || !fullAccessVerified) {
      return "Codex gpt-5.5 full-access mode has not been verified in this shell session. Run Test Codex Config before starting or continuing.";
    }
    return "";
  }
  return "Configured Codex model is unavailable. Change or test the configured Codex model before starting or continuing; the engine will not silently fall back to a weaker model.";
}

async function ensureModelHealthPreflight() {
  const before = knownBlockingModelIssue();
  if (!before) {
    return "";
  }
  state.runLaunch = { inFlight: true, message: "Checking planner model and Codex access before launching...", error: "" };
  renderHomeDashboard();
  await autoTestModelHealth("preflight");
  state.runLaunch = { inFlight: false, message: "", error: "" };
  return knownBlockingModelIssue();
}

async function startRunFromHome() {
  const goal = refs.homeGoal.value.trim();
  const repoBinding = currentRepoBinding();
  if (repoBinding.mismatch) {
    state.runLaunch = { inFlight: false, message: "", error: repoBinding.message };
    renderHomeDashboard();
    setFlash("error", "Wrong repo backend. Restart Backend for Target Repo before starting a run.");
    return;
  }
  if (goal === "") {
    setFlash("error", "Enter a goal before starting a protocol-backed run.");
    renderHomeDashboard();
    return;
  }
  const modelBlocker = await ensureModelHealthPreflight();
  if (modelBlocker) {
    state.runLaunch = { inFlight: false, message: "", error: modelBlocker };
    setActiveTab("settings");
    renderHomeDashboard();
    setFlash("error", modelBlocker);
    return;
  }

  state.runLaunch = { inFlight: true, message: "Launching run through the engine protocol...", error: "" };
  renderHomeDashboard();
  try {
    const result = await window.orchestratorConsole.startRun({
      goal,
      repoPath: activeRepoPath(),
      address: state.address,
    });
    state.runLaunch = {
      inFlight: false,
      message: `Loop running${result && result.run_id ? ` for ${result.run_id}` : ""}. Watch progress in Live Activity.`,
      error: "",
    };
    state.actionRequired.queuedAskHumanAnswer = null;
    state.actionRequired.status = "";
    recordLocalActivity("run_launch_requested", {
      action: "start_run",
      run_id: result && result.run_id ? result.run_id : "",
    }, "start_run accepted through protocol");
    setFlash("success", state.runLaunch.message);
    await refreshStatus({ refreshContracts: true, quiet: true });
  } catch (error) {
    const errorMessage = error && error.message ? error.message : "";
    const contractReadinessError = isRepoContractReadinessError(error);
    const activeMessage = contractReadinessError
      ? friendlyRepoContractReadinessError(error)
      : /already active/i.test(errorMessage)
      ? "A run is already active for this repo. Watch progress or safe stop it first."
      : errorMessage;
    state.runLaunch = { inFlight: false, message: "", error: activeMessage };
    reportIssue(
      "start run",
      contractReadinessError ? displayErrorWithMessage(error, activeMessage) : error,
      contractReadinessError
        ? "Run orchestrator init from the target repo, then refresh the dashboard."
        : "If another run is active, wait for a safe point or use Safe Stop before starting a new run.",
    );
  }
}

async function continueRunFromHome(options = {}) {
  const runID = activeRunID();
  const repoBinding = currentRepoBinding();
  if (repoBinding.mismatch) {
    state.runLaunch = { inFlight: false, message: "", error: repoBinding.message };
    renderHomeDashboard();
    setFlash("error", "Wrong repo backend. Restart Backend for Target Repo before continuing a run.");
    return;
  }
  const askHuman = currentAskHumanViewModel();
  if (askHuman.present && !options.skipAskHumanGuard) {
    state.runLaunch = {
      inFlight: false,
      message: "",
      error: "The planner is waiting for your answer. Use Action Required to send the answer and continue.",
    };
    setActiveTab("attention");
    renderHomeDashboard();
    refs.askHumanAnswer.focus();
    setFlash("info", "This run is waiting for your answer. Use Send Answer and Continue.");
    return;
  }
  const modelBlocker = await ensureModelHealthPreflight();
  if (modelBlocker) {
    state.runLaunch = { inFlight: false, message: "", error: modelBlocker };
    setActiveTab("settings");
    renderHomeDashboard();
    setFlash("error", modelBlocker);
    return;
  }
  state.runLaunch = { inFlight: true, message: "Continuing run through the engine protocol...", error: "" };
  recordLocalActivity("executor_dispatch_requested", {
    action: "continue_run",
    run_id: runID,
  }, "Dispatching Codex executor...");
  renderHomeDashboard();
  try {
    const result = await window.orchestratorConsole.continueRun({
      runId: runID,
      repoPath: activeRepoPath(),
      address: state.address,
    });
    state.runLaunch = {
      inFlight: false,
      message: `Loop running${result && result.run_id ? ` for ${result.run_id}` : ""}. Watch progress in Live Activity.`,
      error: "",
    };
    state.actionRequired.queuedAskHumanAnswer = null;
    state.actionRequired.status = "";
    recordLocalActivity("run_launch_requested", {
      action: "continue_run",
      run_id: result && result.run_id ? result.run_id : runID,
    }, "continue_run accepted through protocol");
    setFlash("success", state.runLaunch.message);
    await refreshStatus({ refreshContracts: true, quiet: true });
  } catch (error) {
    const activeMessage = /already active/i.test(error.message || "")
      ? "A run is already active for this repo. Watch progress or safe stop it first."
      : error.message;
    state.runLaunch = { inFlight: false, message: "", error: activeMessage };
    reportIssue("continue run", error, "If another run is active, wait for it to reach a stop boundary before continuing.");
  }
}

function handleHomePrimaryAction() {
  const action = refs.homePrimaryAction.dataset.action || "refresh_status";
  switch (action) {
    case "connect":
      void connect();
      return;
    case "start_run":
      void startRunFromHome();
      return;
    case "continue_run":
      void continueRunFromHome();
      return;
    case "review_approval":
      scrollToSection("approval-panel");
      return;
    case "answer_ask_human":
      setActiveTab("attention");
      if (refs.askHumanAnswer) {
        refs.askHumanAnswer.focus();
      }
      return;
    case "open_control_chat":
      setActiveTab("home", { noScroll: true });
      refs.controlMessageInput.focus();
      return;
    case "open_latest_artifact":
      void openLatestArtifactFromHome();
      return;
    case "open_settings":
      setActiveTab("settings");
      return;
    case "recover_backend":
      void restartBackend();
      return;
    case "clear_stop_continue":
      void clearStopAndContinue();
      return;
    case "refresh_status":
    default:
      void refreshStatus({ refreshContracts: true });
  }
}

function handleIncomingEvent(event) {
  pushActivityEvent(event);

  const payload = event && event.payload ? event.payload : {};
  if (payload.artifact_path || softRefreshEvents.has(event.event)) {
    scheduleSoftRefresh();
  }
  if (event && (event.event === "executor_approval_required" || event.event === "approval_required" || event.event === "worker_approval_required" || event.event === "executor_turn_failed" || event.event === "fault_recorded" || event.event === "stop_flag_detected" || event.event === "human.question.presented" || (event.event === "planner_turn_completed" && payload.planner_outcome === "ask_human"))) {
    maybeFocusActionRequired({ force: true });
  }
}

function handleConnectionState(connection) {
  const previouslyConnected = state.connection.connected;
  const wasConnecting = state.connection.status === "connecting";
  if (connection && connection.expectedRepoPath) {
    state.expectedRepoPath = connection.expectedRepoPath;
  }
  state.connection = connection;
  if (connection.connected) {
    if (!previouslyConnected || !state.connectionTiming.connectedAt) {
      state.connectionTiming.connectedAt = new Date().toISOString();
    }
    clearReconnectTimer();
    state.reconnect.attempts = 0;
    clearIssue();
  } else if (state.reconnect.enabled && (previouslyConnected || state.reconnect.attempts > 0)) {
    state.connectionTiming.disconnectedAt = new Date().toISOString();
    scheduleReconnect("connection_state_lost");
  } else if (connection.status === "connecting" && !wasConnecting) {
    state.connectionTiming.connectingAt = new Date().toISOString();
  }
  renderConnection();
  renderHomeDashboard();
}

function handleTerminalState(snapshot) {
  const previousSessions = Array.isArray(state.terminal.sessions) ? state.terminal.sessions : [];
  const previousByID = new Map(previousSessions.map((session) => [session.session_id, session]));
  const nextSessions = Array.isArray(snapshot && snapshot.sessions) ? snapshot.sessions : [];
  const nextIDs = new Set(nextSessions.map((session) => session.session_id));

  state.terminal = snapshot;
  renderTerminal();

  nextSessions.forEach((session) => {
    if (!previousByID.has(session.session_id)) {
      recordLocalActivity(
        "terminal_session_started",
        {
          session_id: session.session_id,
          label: session.label,
          cwd: session.cwd || "",
        },
        `terminal_session_started ${session.label || session.session_id}`,
      );
      return;
    }

    const previous = previousByID.get(session.session_id);
    if (previous && previous.status !== session.status && session.status === "stopped") {
      recordLocalActivity(
        "terminal_session_exited",
        {
          session_id: session.session_id,
          label: session.label,
          exit_code: session.exit_code,
        },
        `terminal_session_exited ${session.label || session.session_id}`,
      );
    }
  });

  previousSessions.forEach((session) => {
    if (!nextIDs.has(session.session_id)) {
      recordLocalActivity(
        "terminal_session_closed",
        {
          session_id: session.session_id,
          label: session.label,
        },
        `terminal_session_closed ${session.label || session.session_id}`,
      );
    }
  });
}

function handleTerminalData(payload) {
  if (!payload || !payload.snapshot) {
    return;
  }
  state.terminal = payload.snapshot;
  renderTerminal();
}

function wireEvents() {
  refs.connectButton.addEventListener("click", () => void connect());
  refs.topReconnectButton.addEventListener("click", () => void connect());
  refs.topRefreshButton.addEventListener("click", () => void refreshStatus({ refreshContracts: true }));
  refs.homePrimaryAction.addEventListener("click", handleHomePrimaryAction);
  refs.homeRefreshEverything.addEventListener("click", () => void refreshStatus({ refreshContracts: true }));
  refs.homeOpenLiveOutput.addEventListener("click", openLiveOutput);
  refs.homeCopyDebugBundle.addEventListener("click", () => void copyDebugBundle());
  refs.homeRecoverBackend.addEventListener("click", () => void restartBackend());
  refs.homeClearStopContinue.addEventListener("click", () => void clearStopAndContinue());
  refs.homeSafeStop.addEventListener("click", () => void safeStop());
  refs.homeUseDefaultGoal.addEventListener("click", fillSuggestedGoal);
  refs.homeStartRun.addEventListener("click", () => void startRunFromHome());
  refs.homeContinueRun.addEventListener("click", () => void continueRunFromHome());
  refs.homePrepareStartCommand.addEventListener("click", prepareStartRunCommand);
  refs.homePrepareContinueCommand.addEventListener("click", prepareContinueRunCommand);
  refs.homeGoal.addEventListener("input", () => {
    state.savedGoal.dirty = refs.homeGoal.value.trim() !== (state.savedGoal.content || "").trim();
    renderHomeDashboard();
    renderSavedGoal();
    renderAuroraDashboard();
  });
  refs.goalSaveButton.addEventListener("click", () => void saveGoalFromHome());
  refs.setupRefreshButton.addEventListener("click", () => void refreshSetupHealth());
  refs.useAIGenerateButton.addEventListener("click", openSetupAutofill);
  refs.timelineViewLogsButton.addEventListener("click", openLiveOutput);
  refs.captureSnapshotButton.addEventListener("click", () => void captureRunSnapshot());
  refs.auroraPauseButton.addEventListener("click", () => void pauseAtSafePoint());
  refs.auroraStopButton.addEventListener("click", () => void safeStop());
  refs.auroraContinueButton.addEventListener("click", () => void continueRunFromHome());
  refs.auroraInjectNoteButton.addEventListener("click", () => {
    refs.controlMessageInput.focus();
  });
  refs.auroraViewLogsButton.addEventListener("click", openLiveOutput);
  refs.auroraTimelineFilter.addEventListener("change", renderAuroraTimeline);
  refs.auroraEventsAutoScroll.addEventListener("change", () => {
    state.activityFilters.autoScroll = refs.auroraEventsAutoScroll.checked;
    if (refs.eventsAutoScroll) {
      refs.eventsAutoScroll.checked = refs.auroraEventsAutoScroll.checked;
    }
    persistShellSession();
  });
  refs.projectSystemBody.addEventListener("click", (event) => {
    const item = event.target.closest("[data-project-file]");
    if (item) {
      const contractPath = item.getAttribute("data-project-file") || "";
      void openContractByPath(contractPath).then(() => scrollToSection("contracts-panel"));
    }
  });
  refs.setupHealthBody.addEventListener("click", (event) => {
    const item = event.target.closest("[data-setup-action]");
    if (item) {
      void runSetupHealthAction(item.getAttribute("data-setup-action") || "");
    }
  });
  refs.auroraTimelineBody.addEventListener("click", (event) => {
    const button = event.target.closest("[data-event-index]");
    if (button) {
      void copyAuroraEventPayload(Number(button.getAttribute("data-event-index")));
    }
  });
  refs.homeOpenContracts.addEventListener("click", () => scrollToSection("contracts-panel"));
  refs.homeOpenLatestArtifact.addEventListener("click", () => void openLatestArtifactFromHome());
  refs.homeQuickLiveOutput.addEventListener("click", openLiveOutput);
  refs.homeQuickCopyDebugBundle.addEventListener("click", () => void copyDebugBundle());
  refs.homeAddDogfoodNote.addEventListener("click", () => {
    scrollToSection("dogfood-panel");
    refs.dogfoodTitleInput.focus();
  });
  refs.homeOpenTerminal.addEventListener("click", () => scrollToSection("terminal-panel"));
  refs.sideNavItems.forEach((item) => {
    item.addEventListener("click", () => setActiveTab(item.getAttribute("data-tab-target")));
  });
  refs.homeRepoBody.addEventListener("click", (event) => {
    const item = event.target.closest("[data-home-contract]");
    if (item) {
      const contractPath = item.getAttribute("data-home-contract") || "";
      state.selectedContractPath = contractPath;
      void openContractByPath(contractPath).then(() => scrollToSection("contracts-panel"));
    }
  });
  refs.homeArtifactBody.addEventListener("click", (event) => {
    const item = event.target.closest("[data-home-open-artifact]");
    if (item) {
      void openLatestArtifactFromHome();
    }
  });
  refs.addressInput.addEventListener("input", () => {
    state.address = refs.addressInput.value.trim() || defaultAddress;
    persistShellSession();
  });
  refs.autoReconnect.addEventListener("change", () => {
    state.reconnect.enabled = refs.autoReconnect.checked;
    if (!state.reconnect.enabled) {
      clearReconnectTimer();
    } else if (!state.connection.connected && state.connection.status !== "connecting") {
      scheduleReconnect("auto_reconnect_enabled");
    }
    renderConnection();
  });
  refs.refreshButton.addEventListener("click", () => void refreshStatus({ refreshContracts: true }));
  refs.refreshArtifactsButton.addEventListener("click", () => void refreshArtifacts());
  refs.repoRootButton.addEventListener("click", () => void refreshRepoTree(""));
  refs.repoUpButton.addEventListener("click", () => void refreshRepoTree(state.repoTree.parent_path || ""));
  refs.repoRefreshButton.addEventListener("click", () => void refreshRepoTree());
  refs.repoOpenInContractButton.addEventListener("click", () => void openRepoSelectionInContractEditor());
  refs.verbositySelect.addEventListener("change", () => {
    persistShellSession();
    renderVerbosityHelp();
    void applyVerbosity();
  });
  refs.testPlannerModelButton.addEventListener("click", () => void testPlannerModelFromSettings());
  refs.testExecutorModelButton.addEventListener("click", () => void testExecutorModelFromSettings());
  refs.copyModelHealthButton.addEventListener("click", () => void copyModelHealth());
  refs.restartBackendButton.addEventListener("click", () => void restartBackend());
  refs.loadRuntimeConfigButton.addEventListener("click", () => void loadRuntimeConfig());
  refs.saveRuntimeConfigButton.addEventListener("click", () => void saveRuntimeConfig());
  refs.timeoutPreset.addEventListener("change", () => {
    const patch = timeoutPresetPatch(refs.timeoutPreset.value);
    refs.timeoutPlannerRequest.value = patch.planner_request_timeout;
    refs.timeoutExecutorIdle.value = patch.executor_idle_timeout;
    refs.timeoutExecutorTurn.value = patch.executor_turn_timeout;
    refs.timeoutSubagent.value = patch.subagent_timeout;
    refs.timeoutShellCommand.value = patch.shell_command_timeout;
    refs.timeoutInstall.value = patch.install_timeout;
    refs.timeoutHumanWait.value = patch.human_wait_timeout;
  });
  refs.checkUpdatesButton.addEventListener("click", () => void checkForUpdates());
  refs.copyUpdateChangelogButton.addEventListener("click", () => void copyUpdateChangelog());
  refs.safeStopButton.addEventListener("click", () => void safeStop());
  refs.clearStopButton.addEventListener("click", () => void clearStop());
  refs.sendControlMessageButton.addEventListener("click", () => void sendControlMessage());
  refs.sendSideChatMessageButton.addEventListener("click", () => void sendSideChatMessage());
  refs.sideChatWhatNow.addEventListener("click", () => void askSideChatQuick("What is happening right now?"));
  refs.sideChatWhatChanged.addEventListener("click", () => void askSideChatQuick("What changed while I was gone?"));
  refs.sideChatExplainBlocker.addEventListener("click", () => void askSideChatQuick("Explain the current blocker, if there is one."));
  refs.sideChatAskPlannerReconsider.addEventListener("click", () => {
    const message = refs.sideChatMessageInput.value.trim() || "Operator asks the planner to reconsider the current direction using the latest observable runtime context.";
    void requestSideChatAction("ask_planner_reconsider", message, { approved: true });
  });
  refs.sideChatSafeStop.addEventListener("click", () => {
    void requestSideChatAction("safe_stop", "Operator requested Safe Stop from Side Chat.", { approved: true });
  });
  refs.sideChatCopyConversation.addEventListener("click", () => void copySideChatConversation());
  refs.sideChatCopySupport.addEventListener("click", () => void copyDebugBundle());
  refs.sideChatBody.addEventListener("click", (event) => {
    const button = event.target.closest("[data-side-chat-index]");
    if (!button) {
      return;
    }
    void copySideChatItem(Number(button.getAttribute("data-side-chat-index")));
  });
  refs.captureDogfoodIssueButton.addEventListener("click", () => void captureDogfoodIssue());
  refs.sideChatContextPolicy.addEventListener("change", () => persistShellSession());
  refs.sendAnswerContinueButton.addEventListener("click", () => void sendAskHumanAnswer({ continueAfter: true }));
  refs.sendAnswerOnlyButton.addEventListener("click", () => void sendAskHumanAnswer({ continueAfter: false }));
  refs.continueQueuedAnswerButton.addEventListener("click", () => void continueQueuedAskHumanAnswer());
  refs.approveButton.addEventListener("click", () => void approveExecutor());
  refs.denyButton.addEventListener("click", () => void denyExecutor());
  refs.copyApprovalDetailsButton.addEventListener("click", () => void copyApprovalDetails());
  refs.approvalBody.addEventListener("click", (event) => {
    const button = event.target.closest("[data-summary-action]");
    if (button) {
      handleSummaryAction(button.getAttribute("data-summary-action") || "");
    }
  });
  refs.copyDebugBundleButton.addEventListener("click", () => void copyDebugBundle());
  refs.copyLatestErrorButton.addEventListener("click", () => void copyLatestError());
  refs.summaryOpenLiveOutputButton.addEventListener("click", openLiveOutput);
  refs.refreshWorkersButton.addEventListener("click", () => void refreshWorkers());
  refs.workerCreateButton.addEventListener("click", () => void createWorker());
  refs.workerDispatchButton.addEventListener("click", () => void dispatchWorker());
  refs.workerRemoveButton.addEventListener("click", () => void removeWorker());
  refs.workerIntegrateButton.addEventListener("click", () => void integrateWorkers());
  refs.openSelectedContractButton.addEventListener("click", () => void openContractByPath(state.selectedContractPath));
  refs.saveContractButton.addEventListener("click", () => void saveCurrentContract());
  refs.autofillBackButton.addEventListener("click", () => {
    state.autofill.step = Math.max(0, state.autofill.step - 1);
    syncAutofillAnswersFromFields();
    renderAutofill();
  });
  refs.autofillNextButton.addEventListener("click", () => {
    state.autofill.step = Math.min(3, state.autofill.step + 1);
    syncAutofillAnswersFromFields();
    renderAutofill();
  });
  refs.autofillRunButton.addEventListener("click", () => void runAutofill());
  refs.saveAutofillDraftButton.addEventListener("click", () => void saveSelectedAutofillDraft());
  refs.terminalNewButton.addEventListener("click", () => void createTerminalTab());
  refs.terminalCloseButton.addEventListener("click", () => void closeTerminalTab());
  refs.terminalStopButton.addEventListener("click", () => void stopTerminal());
  refs.terminalClearButton.addEventListener("click", () => void clearTerminal());
  refs.terminalSendButton.addEventListener("click", () => void sendTerminalInput());
  refs.terminalInput.addEventListener("keydown", (event) => {
    if (event.key === "Enter" && !event.shiftKey) {
      event.preventDefault();
      void sendTerminalInput();
    }
  });

  refs.artifactList.addEventListener("click", (event) => {
    const item = event.target.closest("[data-artifact-path]");
    if (item) {
      void openArtifactByPath(item.getAttribute("data-artifact-path"));
    }
  });

  refs.contractFileList.addEventListener("click", (event) => {
    const item = event.target.closest("[data-contract-path]");
    if (item) {
      state.selectedContractPath = item.getAttribute("data-contract-path") || "";
      renderContractFiles();
    }
  });

  refs.dogfoodBody.addEventListener("click", (event) => {
    const item = event.target.closest("[data-dogfood-issue-id]");
    if (item) {
      state.selectedDogfoodIssueID = item.getAttribute("data-dogfood-issue-id") || "";
      renderDogfoodIssues();
    }
  });

  refs.repoTreeList.addEventListener("click", (event) => {
    const item = event.target.closest("[data-repo-path]");
    if (item) {
      void openRepoEntry(item.getAttribute("data-repo-path") || "", item.getAttribute("data-repo-kind") || "file");
    }
  });

  refs.workersBody.addEventListener("click", (event) => {
    const item = event.target.closest("[data-worker-id]");
    if (item) {
      state.selectedWorkerID = item.getAttribute("data-worker-id") || "";
      if (!refs.workerIntegrateIds.value.trim() || refs.workerIntegrateIds.value.trim() === state.selectedWorkerID) {
        refs.workerIntegrateIds.value = state.selectedWorkerID;
      }
      renderWorkers();
    }
  });

  refs.autofillDraftList.addEventListener("click", (event) => {
    const item = event.target.closest("[data-autofill-path]");
    if (item) {
      state.autofill.selectedDraftPath = item.getAttribute("data-autofill-path") || "";
      renderAutofill();
    }
  });

  [
    refs.autofillProjectSummary,
    refs.autofillDesiredOutcome,
    refs.autofillUsersPlatform,
    refs.autofillConstraints,
    refs.autofillMilestones,
    refs.autofillDecisions,
    refs.autofillNotes,
  ].forEach((element) => {
    element.addEventListener("input", () => syncAutofillAnswersFromFields());
  });
  document.querySelectorAll("[data-autofill-target]").forEach((element) => {
    element.addEventListener("change", () => syncAutofillAnswersFromFields());
  });

  refs.terminalTabs.addEventListener("click", (event) => {
    const tab = event.target.closest("[data-terminal-tab]");
    if (tab) {
      void activateTerminalTab(tab.getAttribute("data-terminal-tab") || "");
    }
  });

  refs.eventsBody.addEventListener("click", (event) => {
    const button = event.target.closest("[data-event-index]");
    if (button) {
      void copyEventPayload(Number(button.getAttribute("data-event-index")));
    }
  });

  refs.summaryBody.addEventListener("click", (event) => {
    const button = event.target.closest("[data-summary-action]");
    if (button) {
      handleSummaryAction(button.getAttribute("data-summary-action") || "");
    }
  });

  refs.eventsFilterText.addEventListener("input", () => {
    rememberActivityFiltersFromUI();
    renderEvents();
  });
  refs.eventsCurrentRunOnly.addEventListener("change", () => {
    rememberActivityFiltersFromUI();
    renderEvents();
  });
  refs.eventsAutoScroll.addEventListener("change", () => {
    rememberActivityFiltersFromUI();
    renderEvents();
  });
  refs.eventsCategoryFilters.forEach((element) => {
    element.addEventListener("change", () => {
      rememberActivityFiltersFromUI();
      renderEvents();
    });
  });
  window.addEventListener("resize", updateStickyLayoutOffset);

  window.orchestratorConsole.onEvent(handleIncomingEvent);
  window.orchestratorConsole.onConnectionState(handleConnectionState);
  window.orchestratorConsole.onTerminalState(handleTerminalState);
  window.orchestratorConsole.onTerminalData(handleTerminalData);
}

async function initialize() {
  initializeRefs();
  wireEvents();
  state.connection = await window.orchestratorConsole.getConnectionState();
  state.terminal = await window.orchestratorConsole.getTerminalState();
  refs.verbositySelect.value = persistedShellSession.verbosity || "normal";
  refs.sideChatContextPolicy.value = persistedShellSession.sideChatContextPolicy || "repo_and_latest_run_summary";
  refs.autoReconnect.checked = state.reconnect.enabled;
  refs.eventsFilterText.value = state.activityFilters.searchText;
  refs.eventsCurrentRunOnly.checked = state.activityFilters.currentRunOnly;
  refs.eventsAutoScroll.checked = state.activityFilters.autoScroll;
  refs.eventsCategoryFilters.forEach((element) => {
    const category = element.getAttribute("data-event-category");
    element.checked = category ? state.activityFilters.categories[category] !== false : true;
  });
  renderAll();
  window.setInterval(() => {
    renderConnectionTimers();
  }, 1000);
  if (persistedShellSession.lastConnected && state.reconnect.enabled) {
    void connect({ quiet: true, automatic: true, trigger: "startup_resume" });
  }
}

function escapeHTML(value) {
  return String(value || "")
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;");
}

window.addEventListener("DOMContentLoaded", () => {
  void initialize();
});

window.addEventListener("beforeunload", () => {
  persistShellSession();
});
