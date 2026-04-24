const shellHelpers = window.OrchestratorShellHelpers;
const bootstrapAddress = window.orchestratorConsole.bootstrap
  && typeof window.orchestratorConsole.bootstrap.defaultControlAddress === "string"
  && window.orchestratorConsole.bootstrap.defaultControlAddress.trim() !== ""
  ? window.orchestratorConsole.bootstrap.defaultControlAddress.trim()
  : "http://127.0.0.1:44777";
const persistedShellSession = shellHelpers.loadShellSession(window.localStorage, {
  defaultAddress: bootstrapAddress,
});
const defaultAddress = persistedShellSession.address || bootstrapAddress;
const defaultAutofillTargets = [
  ".orchestrator/brief.md",
  ".orchestrator/roadmap.md",
  ".orchestrator/decisions.md",
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
  "pending_action_updated",
  "pending_action_cleared",
  "control_message_consumed",
  "safe_point_intervention_pending",
  "run_completed",
  "approval_cleared",
  "side_chat_message_recorded",
  "dogfood_issue_recorded",
  "contract_autofill_generated",
  "worker_created",
  "worker_dispatch_completed",
  "worker_removed",
  "worker_integration_completed",
]);

const state = {
  address: persistedShellSession.address || defaultAddress,
  connection: {
    connected: false,
    status: "disconnected",
    address: defaultAddress,
    message: "not connected",
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
  modelTests: {
    planner: null,
    executor: null,
    inFlight: "",
    error: "",
  },
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
  refs.flash = document.getElementById("flash-message");
  refs.topStatusBar = document.getElementById("top-status-bar");
  refs.topRefreshButton = document.getElementById("top-refresh");
  refs.topReconnectButton = document.getElementById("top-reconnect");
  refs.disconnectedBanner = document.getElementById("disconnected-banner");
  refs.homePrimaryAction = document.getElementById("home-primary-action");
  refs.homeRefreshEverything = document.getElementById("home-refresh-everything");
  refs.homeSafeStop = document.getElementById("home-safe-stop");
  refs.homeRecommendationTitle = document.getElementById("home-recommendation-title");
  refs.homeRecommendationDetail = document.getElementById("home-recommendation-detail");
  refs.homeRefreshMeta = document.getElementById("home-refresh-meta");
  refs.homeError = document.getElementById("home-error");
  refs.homeErrorBody = document.getElementById("home-error-body");
  refs.homeGoal = document.getElementById("home-goal");
  refs.homeUseDefaultGoal = document.getElementById("home-use-default-goal");
  refs.homeStartRun = document.getElementById("home-start-run");
  refs.homeContinueRun = document.getElementById("home-continue-run");
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
  refs.dogfoodTitleInput = document.getElementById("dogfood-title");
  refs.dogfoodNoteInput = document.getElementById("dogfood-note");
  refs.captureDogfoodIssueButton = document.getElementById("capture-dogfood-issue");
  refs.dogfoodBody = document.getElementById("dogfood-body");
  refs.dogfoodDetail = document.getElementById("dogfood-detail");
  refs.progressBody = document.getElementById("progress-body");
  refs.statusBody = document.getElementById("status-body");
  refs.pendingBody = document.getElementById("pending-body");
  refs.approvalBody = document.getElementById("approval-body");
  refs.approveButton = document.getElementById("approve-executor");
  refs.denyButton = document.getElementById("deny-executor");
  refs.copyApprovalDetailsButton = document.getElementById("copy-approval-details");
  refs.summaryBody = document.getElementById("summary-body");
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

function renderTopStatus() {
  if (!refs.topStatusBar) {
    return;
  }
  const vm = window.OrchestratorViewModel.buildTopStatusViewModel(state.snapshot, {
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
  });

  refs.topStatusBar.innerHTML = `
    <div class="top-status-item top-status-hero ${escapeHTML(vm.connectionClass)}"><span>${escapeHTML(vm.connectionLabel)}</span><strong>${escapeHTML(vm.connectionDurationLabel)}</strong><small>${escapeHTML(vm.connectionDetail)}</small></div>
    <div class="top-status-item top-status-hero ${escapeHTML(vm.loopClass)}"><span>${escapeHTML(vm.loopLabel)}</span><strong>${escapeHTML(vm.loopDetail)}</strong><small>${escapeHTML(vm.loopStage)} | ${escapeHTML(vm.loopTurn)} | updated ${escapeHTML(vm.loopLastUpdate)}</small></div>
    <div class="top-status-item"><span>Engine Address</span><strong title="${escapeHTML(vm.address)}">${escapeHTML(vm.address)}</strong></div>
    <div class="top-status-item top-status-wide"><span>Repo</span><strong title="${escapeHTML(vm.repoRoot)}">${escapeHTML(vm.repoRoot)}</strong></div>
    <div class="top-status-item"><span>Run</span><strong title="${escapeHTML(vm.runID)}">${escapeHTML(vm.runID)}</strong></div>
    <div class="top-status-item top-status-wide"><span>Stop / Blocker</span><strong title="${escapeHTML(vm.blocker)}">${escapeHTML(vm.blocker)}</strong></div>
    <div class="top-status-item"><span>Verbosity</span><strong>${escapeHTML(vm.verbosity)}</strong></div>
  `;
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
  const codex = window.OrchestratorViewModel.buildCodexReadinessViewModel(state.snapshot);
  const count = (vm.badgeCount || 0) + (codex.needsAttention ? 1 : 0);
  refs.attentionBadge.textContent = String(count);
  refs.attentionBadge.hidden = count === 0;
}

function renderVerbosityHelp() {
  if (!refs.verbosityHelp || !refs.verbositySelect) {
    return;
  }
  const vm = window.OrchestratorViewModel.buildVerbosityViewModel(refs.verbositySelect.value);
  refs.verbosityHelp.textContent = `${vm.label}: ${vm.description} Changes apply immediately through the engine protocol.`;
}

function maybeFocusActionRequired(options = {}) {
  const approval = window.OrchestratorViewModel.buildApprovalViewModel(state.snapshot);
  const codex = window.OrchestratorViewModel.buildCodexReadinessViewModel(state.snapshot);
  const askHuman = state.snapshot && state.snapshot.run
    ? window.OrchestratorViewModel.buildRecommendedActionViewModel(state.snapshot, {
      connection: state.connection,
      goalEntered: refs.homeGoal && refs.homeGoal.value.trim() !== "",
    }).state === "ask_human"
    : false;
  if (!approval.needsAttention && !codex.needsAttention && !askHuman) {
    return;
  }
  if (options.force || state.activeTab === "home" || state.activeTab === "run") {
    setActiveTab((approval.needsAttention || codex.needsAttention) ? "attention" : "chat", { noScroll: false });
  }
}

function renderHomeDashboard() {
  if (!refs.homeRecommendationTitle) {
    return;
  }
  const vm = window.OrchestratorViewModel.buildHomeDashboardViewModel(state.snapshot, {
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
  });
  const action = vm.recommendation.primaryAction || {};
  const progressLabel = vm.progress.progressPercent === null ? "Unavailable" : `${vm.progress.progressPercent}%`;
  const artifactUnavailable = vm.latestArtifactPath === "Unavailable" || vm.latestArtifactPath === "";
  const codex = vm.codex;
  const attentionText = vm.approval.present
    ? vm.approval.summary
    : (codex.needsAttention
      ? "Codex readiness needs attention before a serious autonomous build."
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
  const canStartRun = state.connection.connected && !state.runLaunch.inFlight && startGoal !== "";
  const canContinueRun = state.connection.connected
    && !state.runLaunch.inFlight
    && Boolean(currentRun && !currentRun.completed && currentRun.resumable !== false);
  const actionBlockedByRunLaunch = (action.id === "start_run" && !canStartRun)
    || (action.id === "continue_run" && !canContinueRun);

  refs.homePrimaryAction.textContent = state.runLaunch.inFlight ? "Launching run..." : (action.label || "Update Dashboard");
  refs.homePrimaryAction.disabled = action.enabled === false || state.runLaunch.inFlight || actionBlockedByRunLaunch;
  refs.homePrimaryAction.dataset.action = action.id || "refresh_status";
  refs.homeStartRun.disabled = !canStartRun;
  refs.homeContinueRun.disabled = !canContinueRun;
  refs.homeCommandPreview.textContent = [
    state.runLaunch.inFlight ? "Protocol run action is being submitted..." : "",
    state.runLaunch.message,
    state.runLaunch.error ? `Error: ${state.runLaunch.error}` : "",
    state.preparedCommand ? `Terminal backup prepared: ${state.preparedCommand}` : "Terminal backup: no command prepared.",
  ].filter(Boolean).join("\n");
  refs.homeOpenLatestArtifact.disabled = artifactUnavailable;

  if (vm.homeError) {
    refs.homeError.hidden = false;
    refs.homeErrorBody.textContent = vm.homeError;
  } else {
    refs.homeError.hidden = true;
    refs.homeErrorBody.textContent = "";
  }

  refs.homeRepoBody.innerHTML = [
    homeRow("Repo Root", vm.repo.root),
    homeRow("Contract Status", vm.repo.message),
    `<div class="contract-pill-row">${contractRows || `<span class="panel-note">${escapeHTML(vm.contractStatus.message)}</span>`}</div>`,
  ].join("");

  refs.homeRunBody.innerHTML = [
    homeRow("Run ID", vm.status.runID),
    homeRow("Goal", vm.status.goal),
    homeRow("Run State", vm.status.completed ? "completed" : vm.status.stopReason),
    homeRow("Elapsed", vm.status.elapsedLabel),
    homeRow("Pending Held", vm.status.pendingHeld ? "true" : "false"),
    vm.status.executorLastError !== "None" ? homeRow("Executor Error", vm.status.executorLastError) : "",
  ].join("");

  refs.homeProgressBody.innerHTML = `
    <div class="home-progress-meter">
      <div class="progress-bar-track"><div class="progress-bar-fill" style="width:${escapeHTML(vm.progress.progressBarWidth)}"></div></div>
      <strong>${escapeHTML(progressLabel)}</strong>
      <span>${escapeHTML(vm.progress.progressConfidence)} confidence</span>
    </div>
    <p>${escapeHTML(vm.progress.progressBasis)}</p>
    ${homeRow("Current Focus", vm.progress.currentFocus)}
    ${homeRow("Next Step", vm.progress.nextIntendedStep)}
  `;

  refs.homePlannerBody.innerHTML = `
    <p class="home-large-text">${escapeHTML(vm.latestPlannerMessage)}</p>
    ${homeRow("Why This Step", vm.progress.whyThisStep)}
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
    codex.lastError !== "None" ? homeRow("Last Error", codex.lastError) : "",
    `<p class="panel-note">${escapeHTML(codex.recommendedAction)}</p>`,
  ].join("");

  refs.homeArtifactBody.innerHTML = artifactUnavailable
    ? `<p class="panel-note">${escapeHTML(vm.emptyStates.noArtifacts)}</p>`
    : [
      homeRow("Latest", vm.latestArtifactPath),
      `<button class="button" data-home-open-artifact="${escapeHTML(vm.latestArtifactPath)}">Open Latest Artifact</button>`,
    ].join("");

  refs.homeActivityBody.innerHTML = vm.recentActivity.length === 0
    ? `<div class="event-empty">No recent activity yet. Events appear here after the shell connects and the engine emits protocol events.</div>`
    : vm.recentActivity.map((event) => `
      <div class="home-activity-item">
        <div class="event-chip event-chip-${escapeHTML(event.category)}">${escapeHTML(event.categoryLabel)}</div>
        <strong>${escapeHTML(event.summary)}</strong>
        <span>${escapeHTML(event.timestampLabel)}</span>
      </div>
    `).join("");
}

function renderConnection() {
  refs.addressInput.value = state.address;
  refs.autoReconnect.checked = state.reconnect.enabled;
  const vm = window.OrchestratorViewModel.buildConnectionStatusViewModel(state.snapshot, {
    connection: state.connection,
    address: state.address,
    reconnecting: state.reconnect.pending,
    elapsedSeconds: elapsedSecondsSince(
      state.connection.connected
        ? state.connectionTiming.connectedAt
        : (state.connection.status === "connecting" ? state.connectionTiming.connectingAt : state.connectionTiming.disconnectedAt),
    ),
  });
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
  const vm = window.OrchestratorViewModel.buildConnectionStatusViewModel(state.snapshot, {
    connection: state.connection,
    address: state.address,
    reconnecting: state.reconnect.pending,
    elapsedSeconds: elapsedSecondsSince(
      state.connection.connected
        ? state.connectionTiming.connectedAt
        : (state.connection.status === "connecting" ? state.connectionTiming.connectingAt : state.connectionTiming.disconnectedAt),
    ),
  });
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
      <div class="progress-grid">
        <div class="detail-row">
          <div class="detail-label">Progress Basis</div>
          <div class="detail-value">${escapeHTML(vm.progressBasis)}</div>
        </div>
        <div class="detail-row">
          <div class="detail-label">Current Focus</div>
          <div class="detail-value">${escapeHTML(vm.currentFocus)}</div>
        </div>
        <div class="detail-row">
          <div class="detail-label">Next Intended Step</div>
          <div class="detail-value">${escapeHTML(vm.nextIntendedStep)}</div>
        </div>
        <div class="detail-row">
          <div class="detail-label">Why This Step</div>
          <div class="detail-value">${escapeHTML(vm.whyThisStep)}</div>
        </div>
        <div class="detail-row">
          <div class="detail-label">Roadmap Source</div>
          <div class="detail-value">${escapeHTML(vm.roadmapPath)} | ${escapeHTML(vm.roadmapModifiedAt)}</div>
        </div>
        <div class="detail-row">
          <div class="detail-label">Roadmap Alignment / Context</div>
          <div class="detail-value">${escapeHTML(vm.roadmapAlignmentText)}</div>
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
    ["Elapsed", vm.elapsedLabel],
    ["Started At", vm.startedAt],
    ["Stopped / Last Updated", vm.stoppedAt],
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
    ["Codex Last Error", executor.last_error || "None"],
    ["Codex Last Test", executorTest ? JSON.stringify(executorTest.executor || executorTest, null, 2) : "Not tested from this shell session."],
    ["Recommended Action", executor.recommended_action || planner.recommended_action || "Use the test buttons before long unattended runs."],
  ];
  refs.modelHealthBody.innerHTML = rows
    .map(([label, value]) => `<div class="detail-row"><div class="detail-label">${escapeHTML(label)}</div><div class="detail-value">${escapeHTML(value)}</div></div>`)
    .join("");
  refs.testPlannerModelButton.disabled = state.modelTests.inFlight !== "";
  refs.testExecutorModelButton.disabled = state.modelTests.inFlight !== "";
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
  if (!vm.needsAttention && !codex.needsAttention) {
    refs.approvalBody.innerHTML = `
      <div class="attention-empty">
        <strong>No action required right now.</strong>
        <p>The latest status snapshot does not show a Codex approval, planner question, or worker approval waiting for you.</p>
      </div>
    `;
  } else if (!vm.needsAttention && codex.needsAttention) {
    refs.approvalBody.innerHTML = `
      <div class="detail-row"><div class="detail-label">Action Required</div><div class="detail-value">${escapeHTML(codex.title)}</div></div>
      <div class="detail-row"><div class="detail-label">Access Mode</div><div class="detail-value">${escapeHTML(codex.accessMode)}</div></div>
      <div class="detail-row"><div class="detail-label">Model / Effort</div><div class="detail-value">${escapeHTML(codex.model)} / ${escapeHTML(codex.effort)}</div></div>
      <div class="detail-row"><div class="detail-label">Verification</div><div class="detail-value">${escapeHTML(codex.verificationState)}</div></div>
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

  const canApprove = vm.availableActions.includes("approve");
  const canDeny = vm.availableActions.includes("deny");
  refs.approveButton.disabled = !canApprove;
  refs.denyButton.disabled = !canDeny;
  renderAttentionBadge();
}

function renderRunSummary() {
  const vm = window.OrchestratorViewModel.buildRunSummaryViewModel(state.snapshot, state.artifacts, state.events);
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
    ["Recent Events", vm.recentEvents.join(" | ")],
  ];

  refs.summaryBody.innerHTML = rows
    .map(([label, value]) => `<div class="summary-row"><div class="summary-label">${escapeHTML(label)}</div><div class="summary-value">${escapeHTML(value)}</div></div>`)
    .join("");
}

function renderSideChat() {
  const vm = window.OrchestratorViewModel.buildSideChatViewModel(state.sideChat);
  if (vm.items.length === 0) {
    refs.sideChatBody.innerHTML = `<div class="event-empty">${escapeHTML(vm.message)}</div>`;
    return;
  }

  refs.sideChatBody.innerHTML = vm.items
    .map((item) => `
      <div class="side-chat-item">
        <div class="side-chat-header">
          <div class="side-chat-role">${escapeHTML(item.source)}</div>
          <div class="side-chat-meta">${escapeHTML(item.createdAt)}</div>
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
  const vm = window.OrchestratorViewModel.buildActivityTimelineViewModel(state.events, {
    searchText: state.activityFilters.searchText,
    currentRunOnly: state.activityFilters.currentRunOnly,
    currentRunID: activeRunID(),
    categories: state.activityFilters.categories,
    verbosity: refs.verbositySelect ? refs.verbositySelect.value : "normal",
  });

  refs.eventsMeta.textContent = `${vm.filteredCount} shown / ${vm.totalCount} total | verbosity: ${vm.verbosity} | current run filter: ${vm.currentRunOnly ? "on" : "off"} | run: ${vm.currentRunID}`;

  if (vm.items.length === 0) {
    refs.eventsBody.innerHTML = `<div class="event-empty">${escapeHTML(vm.emptyMessage)}</div>`;
    return;
  }

  refs.eventsBody.innerHTML = vm.items
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
  renderProgressPanel();
  renderStatus();
  renderModelHealth();
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
    state.connectionTiming.connectedAt = new Date().toISOString();
    state.snapshot = response.snapshot;
    state.lastRefreshedAt = new Date().toISOString();
    await hydrateProtocolBackedPanels(true);
    clearReconnectTimer();
    state.reconnect.attempts = 0;
    clearIssue();
    renderAll();
    maybeFocusActionRequired();
    setFlash("success", options.automatic ? "Reconnected to the control server." : "Connected to the control server.");
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
  const [artifactsResult, sideChatResult, dogfoodResult, workersResult] = await Promise.allSettled([
    window.orchestratorConsole.listRecentArtifacts(runID, "", 12, state.address),
    window.orchestratorConsole.listSideChatMessages(activeRepoPath(), 20, state.address),
    window.orchestratorConsole.listDogfoodIssues(activeRepoPath(), 20, state.address),
    window.orchestratorConsole.listWorkers(runID, 20, state.address),
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
    } catch (error) {
      reportIssue("contract files", error, "Canonical contract files could not be listed.");
    }
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

async function refreshStatus(options = {}) {
  try {
    state.snapshot = await window.orchestratorConsole.getStatusSnapshot("", state.address);
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
    state.snapshot = { ...(state.snapshot || {}), model_health: result };
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
    state.snapshot = { ...(state.snapshot || {}), model_health: result };
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

async function sendControlMessage() {
  const message = refs.controlMessageInput.value.trim();
  if (message === "") {
    setFlash("error", "Enter a control message before sending.");
    return;
  }

  try {
    const queued = await window.orchestratorConsole.injectControlMessage({
      runId: activeRunID(),
      message,
      source: "control_chat",
      reason: "operator_intervention_from_shell",
      address: state.address,
    });
    refs.controlMessageInput.value = "";
    await refreshStatus({ quiet: true });
    setFlash("success", `Queued control message ${queued.id || ""}`.trim());
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
    setFlash("info", response.message || "Side chat backend is not implemented in this slice.");
  } catch (error) {
    reportIssue("side chat", error);
  }
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
  try {
    const result = await window.orchestratorConsole.approveExecutor(activeRunID(), state.address);
    await refreshStatus({ quiet: true });
    setFlash("success", result.summary || "Executor approval granted.");
  } catch (error) {
    reportIssue("approve executor", error);
  }
}

async function denyExecutor() {
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
  const codex = window.OrchestratorViewModel.buildCodexReadinessViewModel(state.snapshot);
  const latestExecutorTest = state.modelTests.executor && state.modelTests.executor.executor
    ? state.modelTests.executor.executor
    : null;
  if (latestExecutorTest && latestExecutorTest.verification_state && latestExecutorTest.verification_state !== "invalid" && latestExecutorTest.verification_state !== "unavailable") {
    return "";
  }
  if (!codex.modelInvalid) {
    return "";
  }
  return "Configured Codex model is unavailable. Change or test the configured Codex model before starting or continuing; the engine will not silently fall back to a weaker model.";
}

async function startRunFromHome() {
  const goal = refs.homeGoal.value.trim();
  if (goal === "") {
    setFlash("error", "Enter a goal before starting a protocol-backed run.");
    renderHomeDashboard();
    return;
  }
  const modelBlocker = knownBlockingModelIssue();
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
    recordLocalActivity("run_launch_requested", {
      action: "start_run",
      run_id: result && result.run_id ? result.run_id : "",
    }, "start_run accepted through protocol");
    setFlash("success", state.runLaunch.message);
    await refreshStatus({ refreshContracts: true, quiet: true });
  } catch (error) {
    const activeMessage = /already active/i.test(error.message || "")
      ? "A run is already active for this repo. Watch progress or safe stop it first."
      : error.message;
    state.runLaunch = { inFlight: false, message: "", error: activeMessage };
    reportIssue("start run", error, "If another run is active, wait for a safe point or use Safe Stop before starting a new run.");
  }
}

async function continueRunFromHome() {
  const runID = activeRunID();
  const modelBlocker = knownBlockingModelIssue();
  if (modelBlocker) {
    state.runLaunch = { inFlight: false, message: "", error: modelBlocker };
    setActiveTab("settings");
    renderHomeDashboard();
    setFlash("error", modelBlocker);
    return;
  }
  state.runLaunch = { inFlight: true, message: "Continuing run through the engine protocol...", error: "" };
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
    case "open_control_chat":
      scrollToSection("control-chat-panel");
      refs.controlMessageInput.focus();
      return;
    case "open_latest_artifact":
      void openLatestArtifactFromHome();
      return;
    case "open_settings":
      setActiveTab("settings");
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
  if (event && (event.event === "executor_approval_required" || event.event === "approval_required" || event.event === "worker_approval_required" || event.event === "executor_turn_failed" || event.event === "fault_recorded")) {
    maybeFocusActionRequired({ force: true });
  }
}

function handleConnectionState(connection) {
  const previouslyConnected = state.connection.connected;
  const wasConnecting = state.connection.status === "connecting";
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
  refs.homeSafeStop.addEventListener("click", () => void safeStop());
  refs.homeUseDefaultGoal.addEventListener("click", fillSuggestedGoal);
  refs.homeStartRun.addEventListener("click", () => void startRunFromHome());
  refs.homeContinueRun.addEventListener("click", () => void continueRunFromHome());
  refs.homePrepareStartCommand.addEventListener("click", prepareStartRunCommand);
  refs.homePrepareContinueCommand.addEventListener("click", prepareContinueRunCommand);
  refs.homeGoal.addEventListener("input", () => renderHomeDashboard());
  refs.homeOpenContracts.addEventListener("click", () => scrollToSection("contracts-panel"));
  refs.homeOpenLatestArtifact.addEventListener("click", () => void openLatestArtifactFromHome());
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
  refs.safeStopButton.addEventListener("click", () => void safeStop());
  refs.clearStopButton.addEventListener("click", () => void clearStop());
  refs.sendControlMessageButton.addEventListener("click", () => void sendControlMessage());
  refs.sendSideChatMessageButton.addEventListener("click", () => void sendSideChatMessage());
  refs.captureDogfoodIssueButton.addEventListener("click", () => void captureDogfoodIssue());
  refs.sideChatContextPolicy.addEventListener("change", () => persistShellSession());
  refs.approveButton.addEventListener("click", () => void approveExecutor());
  refs.denyButton.addEventListener("click", () => void denyExecutor());
  refs.copyApprovalDetailsButton.addEventListener("click", () => void copyApprovalDetails());
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
