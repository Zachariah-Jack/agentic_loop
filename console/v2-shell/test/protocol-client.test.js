const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");

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
  streamControlEvents,
} = require("../src/protocol/client");
const {
  loadShellSession,
  saveShellSession,
  nextReconnectDelay,
  buildConnectionDetails,
  formatProtocolError,
} = require("../src/renderer/shell-helpers");
const {
  buildStatusViewModel,
  buildConnectionStatusViewModel,
  buildLoopStatusViewModel,
  buildVerbosityViewModel,
  buildRecommendedActionViewModel,
  buildTopStatusViewModel,
  buildHomeDashboardViewModel,
  translateStopReason,
  buildProgressPanelViewModel,
  buildActivityTimelineViewModel,
  buildTerminalTabsViewModel,
  buildPendingActionViewModel,
  buildArtifactListViewModel,
  buildApprovalViewModel,
  buildRunSummaryViewModel,
  buildWhatHappenedViewModel,
  buildCodexReadinessViewModel,
  modelUnavailableFromText,
  buildSideChatViewModel,
  buildDogfoodIssuesViewModel,
  buildWorkerPanelViewModel,
  buildAutofillViewModel,
  buildRepoTreeViewModel,
  classifyActivityCategory,
  formatEventSummary,
} = require("../src/renderer/view-model");

function memoryStorage(seed = {}) {
  const values = new Map(Object.entries(seed));
  return {
    getItem(key) {
      return values.has(key) ? values.get(key) : null;
    },
    setItem(key, value) {
      values.set(key, String(value));
    },
  };
}

test("normalizeControlBaseURL adds loopback scheme when omitted", () => {
  assert.equal(normalizeControlBaseURL("127.0.0.1:44777"), "http://127.0.0.1:44777");
  assert.equal(normalizeControlBaseURL("http://127.0.0.1:44777/"), "http://127.0.0.1:44777");
});

test("getStatusSnapshot posts the real control action envelope", async () => {
  let requestURL = "";
  let requestBody = "";
  const payload = await getStatusSnapshot("127.0.0.1:44777", "run_123", {
    fetchImpl: async (url, init) => {
      requestURL = url;
      requestBody = init.body;
      return new Response(JSON.stringify({
        type: "response",
        ok: true,
        payload: {
          run: { id: "run_123", goal: "Build the dashboard" },
          runtime: { verbosity: "normal" },
          planner_status: { present: true, operator_message: "Implementing dashboard shell" },
          pending_action: { available: true, present: false },
        },
      }), { status: 200 });
    },
  });

  assert.equal(requestURL, "http://127.0.0.1:44777/v2/control");
  assert.match(requestBody, /"action":"get_status_snapshot"/);
  assert.match(requestBody, /"run_id":"run_123"/);
  assert.equal(payload.run.id, "run_123");
});

test("startRun and continueRun use real protocol actions", async () => {
  const requestBodies = [];
  const fetchImpl = async (_url, init) => {
    requestBodies.push(init.body);
    return new Response(JSON.stringify({
      type: "response",
      ok: true,
      payload: { accepted: true, async: true, run_id: "run_protocol" },
    }), { status: 200 });
  };

  await startRun("127.0.0.1:44777", {
    goal: "Build the next highest-value slice.",
    repo_path: "D:/repo",
  }, { fetchImpl });
  await continueRun("127.0.0.1:44777", {
    run_id: "run_protocol",
    repo_path: "D:/repo",
  }, { fetchImpl });

  assert.match(requestBodies[0], /"action":"start_run"/);
  assert.match(requestBodies[0], /"goal":"Build the next highest-value slice\."/);
  assert.match(requestBodies[0], /"repo_path":"D:\/repo"/);
  assert.match(requestBodies[1], /"action":"continue_run"/);
  assert.match(requestBodies[1], /"run_id":"run_protocol"/);
});

test("model test protocol helpers use explicit model health actions", async () => {
  const requestBodies = [];
  const fetchImpl = async (_url, init) => {
    requestBodies.push(init.body);
    return new Response(JSON.stringify({
      type: "response",
      ok: true,
      payload: {
        planner: { component: "planner", verification_state: "verified" },
        executor: { component: "executor", verification_state: "not_verified" },
      },
    }), { status: 200 });
  };

  await testPlannerModel("127.0.0.1:44777", { model: "gpt-5-latest" }, { fetchImpl });
  await testExecutorModel("127.0.0.1:44777", {}, { fetchImpl });

  assert.match(requestBodies[0], /"action":"test_planner_model"/);
  assert.match(requestBodies[0], /"model":"gpt-5-latest"/);
  assert.match(requestBodies[1], /"action":"test_executor_model"/);
});

test("injectControlMessage surfaces control protocol errors truthfully", async () => {
  await assert.rejects(
    () => injectControlMessage("127.0.0.1:44777", { message: "redirect the run" }, {
      fetchImpl: async () => new Response(JSON.stringify({
        type: "response",
        ok: false,
        error: { message: "no unfinished run is available for control-message injection" },
      }), { status: 400 }),
    }),
    /no unfinished run is available/,
  );
});

test("protocol client surfaces unreachable control server errors clearly", async () => {
  await assert.rejects(
    () => getStatusSnapshot("127.0.0.1:44777", "run_123", {
      fetchImpl: async () => {
        throw new Error("connection refused");
      },
    }),
    /Unable to reach control server/,
  );
});

test("setVerbosity uses the real runtime protocol action", async () => {
  let requestBody = "";
  const payload = await setVerbosity("127.0.0.1:44777", "verbose", {
    fetchImpl: async (_url, init) => {
      requestBody = init.body;
      return new Response(JSON.stringify({
        type: "response",
        ok: true,
        payload: { verbosity: "verbose" },
      }), { status: 200 });
    },
  });

  assert.match(requestBody, /"action":"set_verbosity"/);
  assert.match(requestBody, /"verbosity":"verbose"/);
  assert.equal(payload.verbosity, "verbose");
});

test("artifact protocol helpers use the real control actions", async () => {
  const requestBodies = [];
  const fetchImpl = async (_url, init) => {
    requestBodies.push(init.body);
    return new Response(JSON.stringify({
      type: "response",
      ok: true,
      payload: { ok: true },
    }), { status: 200 });
  };

  await listRecentArtifacts("127.0.0.1:44777", { run_id: "run_1", limit: 5 }, { fetchImpl });
  await getArtifact("127.0.0.1:44777", ".orchestrator/artifacts/context/run_1/context.json", { fetchImpl });

  assert.match(requestBodies[0], /"action":"list_recent_artifacts"/);
  assert.match(requestBodies[0], /"run_id":"run_1"/);
  assert.match(requestBodies[1], /"action":"get_artifact"/);
  assert.match(requestBodies[1], /"artifact_path":"\.orchestrator\/artifacts\/context\/run_1\/context\.json"/);
});

test("approval protocol helpers use the real control actions", async () => {
  const requestBodies = [];
  const fetchImpl = async (_url, init) => {
    requestBodies.push(init.body);
    return new Response(JSON.stringify({
      type: "response",
      ok: true,
      payload: { present: false, state: "granted" },
    }), { status: 200 });
  };

  await approveExecutor("127.0.0.1:44777", "run_1", { fetchImpl });
  await denyExecutor("127.0.0.1:44777", "run_1", { fetchImpl });

  assert.match(requestBodies[0], /"action":"approve_executor"/);
  assert.match(requestBodies[0], /"run_id":"run_1"/);
  assert.match(requestBodies[1], /"action":"deny_executor"/);
});

test("contract file protocol helpers use the real control actions", async () => {
  const requestBodies = [];
  const fetchImpl = async (_url, init) => {
    requestBodies.push(init.body);
    return new Response(JSON.stringify({
      type: "response",
      ok: true,
      payload: { ok: true },
    }), { status: 200 });
  };

  await listContractFiles("127.0.0.1:44777", "D:/repo", { fetchImpl });
  await openContractFile("127.0.0.1:44777", { repo_path: "D:/repo", path: ".orchestrator/brief.md" }, { fetchImpl });
  await saveContractFile("127.0.0.1:44777", {
    repo_path: "D:/repo",
    path: ".orchestrator/brief.md",
    content: "updated brief",
    expected_mtime: "2026-04-22T12:00:00Z",
  }, { fetchImpl });

  assert.match(requestBodies[0], /"action":"list_contract_files"/);
  assert.match(requestBodies[1], /"action":"open_contract_file"/);
  assert.match(requestBodies[1], /"path":"\.orchestrator\/brief\.md"/);
  assert.match(requestBodies[2], /"action":"save_contract_file"/);
  assert.match(requestBodies[2], /"expected_mtime":"2026-04-22T12:00:00Z"/);
});

test("autofill protocol helper uses the real control action", async () => {
  let requestBody = "";
  await runAIAutofill("127.0.0.1:44777", {
    repo_path: "D:/repo",
    targets: [".orchestrator/brief.md", ".orchestrator/roadmap.md"],
    answers: {
      project_summary: "Planner-led shell",
      desired_outcome: "Reduce setup friction",
    },
  }, {
    fetchImpl: async (_url, init) => {
      requestBody = init.body;
      return new Response(JSON.stringify({
        type: "response",
        ok: true,
        payload: { available: true, files: [] },
      }), { status: 200 });
    },
  });

  assert.match(requestBody, /"action":"run_ai_autofill"/);
  assert.match(requestBody, /"project_summary":"Planner-led shell"/);
});

test("repo tree protocol helpers use the real control actions", async () => {
  const requestBodies = [];
  const fetchImpl = async (_url, init) => {
    requestBodies.push(init.body);
    return new Response(JSON.stringify({
      type: "response",
      ok: true,
      payload: { count: 0, items: [] },
    }), { status: 200 });
  };

  await listRepoTree("127.0.0.1:44777", { repo_path: "D:/repo", path: "cmd", limit: 50 }, { fetchImpl });
  await openRepoFile("127.0.0.1:44777", { repo_path: "D:/repo", path: "README.md" }, { fetchImpl });

  assert.match(requestBodies[0], /"action":"list_repo_tree"/);
  assert.match(requestBodies[0], /"path":"cmd"/);
  assert.match(requestBodies[1], /"action":"open_repo_file"/);
  assert.match(requestBodies[1], /"path":"README\.md"/);
});

test("side chat protocol helpers use the real control actions", async () => {
  const requestBodies = [];
  const fetchImpl = async (_url, init) => {
    requestBodies.push(init.body);
    return new Response(JSON.stringify({
      type: "response",
      ok: true,
      payload: { available: false, stored: true },
    }), { status: 200 });
  };

  await sendSideChatMessage("127.0.0.1:44777", {
    repo_path: "D:/repo",
    message: "What remains before release?",
    context_policy: "repo_and_latest_run_summary",
  }, { fetchImpl });
  await listSideChatMessages("127.0.0.1:44777", { repo_path: "D:/repo", limit: 10 }, { fetchImpl });

  assert.match(requestBodies[0], /"action":"send_side_chat_message"/);
  assert.match(requestBodies[0], /"context_policy":"repo_and_latest_run_summary"/);
  assert.match(requestBodies[1], /"action":"list_side_chat_messages"/);
});

test("dogfood issue protocol helpers use the real control actions", async () => {
  const requestBodies = [];
  const fetchImpl = async (_url, init) => {
    requestBodies.push(init.body);
    return new Response(JSON.stringify({
      type: "response",
      ok: true,
      payload: { available: true, stored: true },
    }), { status: 200 });
  };

  await captureDogfoodIssue("127.0.0.1:44777", {
    repo_path: "D:/repo",
    run_id: "run_1",
    title: "Reconnect shows stale artifact",
    note: "The artifact viewer kept the old path after reconnect.",
    source: "operator_shell",
  }, { fetchImpl });
  await listDogfoodIssues("127.0.0.1:44777", { repo_path: "D:/repo", limit: 10 }, { fetchImpl });

  assert.match(requestBodies[0], /"action":"capture_dogfood_issue"/);
  assert.match(requestBodies[0], /"title":"Reconnect shows stale artifact"/);
  assert.match(requestBodies[1], /"action":"list_dogfood_issues"/);
});

test("worker action protocol helpers use the real control actions", async () => {
  const requestBodies = [];
  const fetchImpl = async (_url, init) => {
    requestBodies.push(init.body);
    return new Response(JSON.stringify({
      type: "response",
      ok: true,
      payload: { count: 0, items: [] },
    }), { status: 200 });
  };

  await listWorkers("127.0.0.1:44777", { run_id: "run_1", limit: 12 }, {
    fetchImpl,
  });
  await createWorker("127.0.0.1:44777", { run_id: "run_1", name: "code-survey", scope: "inspect UI shell" }, {
    fetchImpl,
  });
  await dispatchWorker("127.0.0.1:44777", { worker_id: "worker_1", prompt: "Inspect the shell." }, {
    fetchImpl,
  });
  await removeWorker("127.0.0.1:44777", { worker_id: "worker_1" }, {
    fetchImpl,
  });
  await integrateWorkers("127.0.0.1:44777", { worker_ids: ["worker_1", "worker_2"] }, {
    fetchImpl,
  });

  assert.match(requestBodies[0], /"action":"list_workers"/);
  assert.match(requestBodies[0], /"run_id":"run_1"/);
  assert.match(requestBodies[1], /"action":"create_worker"/);
  assert.match(requestBodies[1], /"name":"code-survey"/);
  assert.match(requestBodies[2], /"action":"dispatch_worker"/);
  assert.match(requestBodies[2], /"worker_id":"worker_1"/);
  assert.match(requestBodies[3], /"action":"remove_worker"/);
  assert.match(requestBodies[4], /"action":"integrate_workers"/);
  assert.match(requestBodies[4], /"worker_ids":\["worker_1","worker_2"\]/);
});

test("buildProgressPanelViewModel normalizes roadmap-aware operator progress", () => {
  const vm = buildProgressPanelViewModel({
    planner_status: {
      present: true,
      operator_message: "Implementing worker orchestration controls.",
      progress_percent: 61,
      progress_confidence: "medium",
      progress_basis: "Protocol actions and shell panes are in flight.",
      current_focus: "worker control forms",
      next_intended_step: "wire dispatch and integrate actions",
      why_this_step: "The shell needs real operator-triggered worker control.",
    },
    roadmap: {
      present: true,
      path: ".orchestrator/roadmap.md",
      alignment_text: "Current slice aligns with the orchestration-focused V2 shell work.",
      modified_at: "2026-04-23T01:02:03Z",
    },
  });

  assert.equal(vm.progressPercent, 61);
  assert.equal(vm.progressBarWidth, "61%");
  assert.equal(vm.progressConfidence, "medium");
  assert.equal(vm.currentFocus, "worker control forms");
  assert.equal(vm.roadmapPath, ".orchestrator/roadmap.md");
  assert.equal(vm.roadmapAlignmentText, "Current slice aligns with the orchestration-focused V2 shell work.");
  assert.equal(vm.roadmapModifiedAt, "2026-04-23T01:02:03Z");
});

test("buildRecommendedActionViewModel guides disconnected users to connect", () => {
  const vm = buildRecommendedActionViewModel(null, {
    connection: { connected: false, status: "disconnected", address: "http://127.0.0.1:44777" },
  });

  assert.equal(vm.state, "disconnected");
  assert.equal(vm.title, "Connect to the app engine.");
  assert.equal(vm.primaryAction.id, "connect");
  assert.equal(vm.primaryAction.enabled, true);
});

test("buildRecommendedActionViewModel asks for a goal before protocol start", () => {
  const vm = buildRecommendedActionViewModel({
    runtime: { repo_root: "D:/repo", repo_ready: true },
  }, {
    connection: { connected: true, status: "connected" },
  });

  assert.equal(vm.state, "connected_no_run");
  assert.equal(vm.title, "Enter a goal, then start a build.");
  assert.equal(vm.primaryAction.id, "start_run");
  assert.equal(vm.primaryAction.kind, "protocol");
  assert.equal(vm.primaryAction.enabled, false);
  assert.match(vm.detail, /Enter a goal/);
});

test("buildRecommendedActionViewModel uses protocol start when a no-run goal is entered", () => {
  const vm = buildRecommendedActionViewModel({
    runtime: { repo_root: "D:/repo", repo_ready: true },
  }, {
    connection: { connected: true, status: "connected" },
    goalEntered: true,
  });

  assert.equal(vm.state, "connected_no_run");
  assert.equal(vm.title, "Start a new build.");
  assert.equal(vm.primaryAction.id, "start_run");
  assert.equal(vm.primaryAction.kind, "protocol");
  assert.equal(vm.primaryAction.enabled, true);
  assert.match(vm.detail, /start_run protocol action/);
});

test("buildRecommendedActionViewModel guides resumable runs to continue command", () => {
  const vm = buildRecommendedActionViewModel({
    run: {
      id: "run_1",
      status: "stopped",
      stop_reason: "cycle_boundary",
      completed: false,
      resumable: true,
    },
  }, {
    connection: { connected: true, status: "connected" },
  });

  assert.equal(vm.state, "resumable");
  assert.equal(vm.title, "Continue the existing build.");
  assert.equal(vm.primaryAction.id, "continue_run");
  assert.equal(vm.primaryAction.kind, "protocol");
});

test("buildRecommendedActionViewModel prioritizes approval-required state", () => {
  const vm = buildRecommendedActionViewModel({
    run: {
      id: "run_approval",
      status: "stopped",
      completed: false,
      resumable: true,
    },
    approval: {
      present: true,
      state: "required",
      summary: "executor approval required for command: go test ./...",
    },
  }, {
    connection: { connected: true, status: "connected" },
  });

  assert.equal(vm.state, "approval_required");
  assert.equal(vm.title, "Review the action required.");
  assert.equal(vm.primaryAction.id, "review_approval");
});

test("buildRecommendedActionViewModel allows completed runs to open surfaced artifacts", () => {
  const vm = buildRecommendedActionViewModel({
    run: {
      id: "run_done",
      status: "completed",
      completed: true,
    },
    artifacts: {
      latest_path: ".orchestrator/artifacts/executor/run_done/result.json",
    },
  }, {
    connection: { connected: true, status: "connected" },
  });

  assert.equal(vm.state, "completed");
  assert.equal(vm.title, "Review results or start a new run.");
  assert.equal(vm.primaryAction.id, "open_latest_artifact");
});

test("model unavailable state becomes action-required recommended action", () => {
  const snapshot = {
    runtime: { executor_ready: true },
    run: {
      id: "run_model",
      status: "initialized",
      stop_reason: "executor_failed",
      completed: false,
      resumable: true,
      executor_last_error: "stream disconnected before completion: The model `gpt-5.5` does not exist or you do not have access to it.",
    },
    model_health: {
      blocking: true,
      executor: {
        verification_state: "invalid",
        requested_model: "gpt-5.5",
        model_unavailable: true,
        last_error: "stream disconnected before completion: The model `gpt-5.5` does not exist or you do not have access to it.",
        plain_english: "Codex could not start because the configured model gpt-5.5 is not available to this account.",
        recommended_action: "Change or test the configured Codex model.",
      },
    },
  };

  const vm = buildRecommendedActionViewModel(snapshot, {
    connection: { connected: true, status: "connected" },
  });
  const codex = buildCodexReadinessViewModel(snapshot);
  const what = buildWhatHappenedViewModel(snapshot, null, []);

  assert.equal(modelUnavailableFromText(snapshot.run.executor_last_error), true);
  assert.equal(vm.state, "model_invalid");
  assert.equal(vm.primaryAction.id, "open_settings");
  assert.equal(codex.modelInvalid, true);
  assert.equal(codex.needsAttention, true);
  assert.equal(what.stateLabel, "Model error");
  assert.match(what.stop.title, /gpt-5.5/);
});

test("buildTopStatusViewModel makes repo and run identity explicit", () => {
  const vm = buildTopStatusViewModel({
    runtime: {
      repo_root: "D:/repo",
      repo_ready: true,
      verbosity: "verbose",
    },
    run: {
      id: "run_2",
      status: "stopped",
      stop_reason: "planner_ask_human",
      completed: false,
    },
  }, {
    connection: { connected: true, status: "connected", address: "http://127.0.0.1:44777" },
    lastRefreshedAt: "2026-04-23T11:00:00Z",
  });

  assert.equal(vm.connectionState, "Ready");
  assert.equal(vm.repoRoot, "D:/repo");
  assert.equal(vm.runID, "run_2");
  assert.equal(vm.runState, "stopped");
  assert.equal(vm.blocker, "The planner needs information from you.");
  assert.equal(vm.verbosity, "verbose");
  assert.equal(vm.lastRefreshedAt, "2026-04-23T11:00:00Z");
});

test("connection and loop status view models use plain operator language", () => {
  const connection = buildConnectionStatusViewModel(null, {
    connection: { connected: true, status: "connected", address: "http://127.0.0.1:44777" },
    elapsedSeconds: 194,
  });
  assert.equal(connection.label, "Connection Status: Ready");
  assert.equal(connection.durationLabel, "Ready for 03:14");

  const loop = buildLoopStatusViewModel({
    run: {
      id: "run_live",
      status: "running",
      completed: false,
      latest_checkpoint: { sequence: 4, stage: "executor_turn" },
    },
  }, {
    latestEvent: { event: "executor_turn_started", at: "2026-04-23T11:01:00Z" },
  });
  assert.equal(loop.label, "Loop Status: Running");
  assert.equal(loop.stage, "executor_turn");
});

test("stop reason translation gives plain next actions without hiding technical code", () => {
  const approval = translateStopReason("executor_approval_required");
  assert.equal(approval.title, "Codex needs approval before continuing.");
  assert.match(approval.detail, /executor_approval_required/);
  assert.match(approval.nextAction, /Action Required/);

  const validation = translateStopReason("planner_validation_failed");
  assert.equal(validation.severity, "danger");
  const executorFailed = translateStopReason("executor_failed");
  assert.equal(executorFailed.severity, "danger");
  assert.match(executorFailed.nextAction, /Codex model/);
  assert.match(validation.title, /planner returned output/);
});

test("buildHomeDashboardViewModel exposes helpful empty state copy and refresh metadata", () => {
  const vm = buildHomeDashboardViewModel({
    runtime: {
      repo_root: "D:/repo",
      repo_ready: false,
      verbosity: "normal",
    },
  }, {
    connection: { connected: true, status: "connected" },
    artifacts: { items: [] },
    contractFiles: { files: [] },
    events: [],
    lastRefreshedAt: "4/23/2026, 11:00:00 AM",
    homeError: "status refresh failed",
  });

  assert.equal(vm.recommendation.state, "connected_no_run");
  assert.equal(vm.topStatus.repoRoot, "D:/repo");
  assert.equal(vm.refreshedLabel, "4/23/2026, 11:00:00 AM");
  assert.match(vm.emptyStates.noRun, /No run found/);
  assert.match(vm.emptyStates.noArtifacts, /Artifacts appear/);
  assert.match(vm.emptyStates.noPendingAction, /not currently holding/);
  assert.equal(vm.homeError, "status refresh failed");
  assert.equal(vm.contractStatus.message, "Contract status has not been loaded yet. Connect or refresh everything to inspect canonical files.");
  assert.equal(vm.codex.fullAccessReady, "No");
  assert.match(vm.codex.recommendedAction, /doctor/);
});

test("approval view model explains action-required state in plain English", () => {
  const vm = buildApprovalViewModel({
    approval: {
      present: true,
      state: "required",
      kind: "command_execution",
      summary: "executor approval required for command: go test ./...",
      command: "go test ./...",
      cwd: "D:/repo",
      available_actions: ["approve", "deny"],
    },
  });

  assert.equal(vm.needsAttention, true);
  assert.equal(vm.badgeCount, 1);
  assert.equal(vm.plainEnglish.requested, "Codex wants to run a command.");
  assert.match(vm.plainEnglish.approveEffect, /Approve tells Codex/);
});

test("codex readiness is truthful when full-access details are not verified", () => {
  const ready = buildCodexReadinessViewModel({ runtime: { executor_ready: true } });
  assert.equal(ready.executorReady, true);
  assert.equal(ready.fullAccessReady, "Not verified");
  assert.match(ready.model, /Not verified/);

  const missing = buildCodexReadinessViewModel({ runtime: { executor_ready: false } });
  assert.equal(missing.needsAttention, true);
  assert.equal(missing.fullAccessReady, "No");
});

test("verbosity view model documents auto-applied levels", () => {
  const vm = buildVerbosityViewModel("trace");
  assert.equal(vm.label, "Trace");
  assert.match(vm.description, /Raw safe event payloads/);
});

test("loadShellSession restores persisted local shell state and legacy address", () => {
  const storage = memoryStorage({
    "orchestrator-v2-shell.session": JSON.stringify({
      address: "http://127.0.0.1:55000",
      autoReconnect: false,
      verbosity: "trace",
      selectedWorkerID: "worker_1",
      selectedDogfoodIssueID: "dogfood_1",
      activityFilters: {
        searchText: "approval",
        currentRunOnly: true,
        autoScroll: false,
        categories: { approval: true, terminal: false },
      },
    }),
  });

  const session = loadShellSession(storage);
  assert.equal(session.address, "http://127.0.0.1:55000");
  assert.equal(session.autoReconnect, false);
  assert.equal(session.verbosity, "trace");
  assert.equal(session.selectedWorkerID, "worker_1");
  assert.equal(session.selectedDogfoodIssueID, "dogfood_1");
  assert.equal(session.activityFilters.searchText, "approval");
  assert.equal(session.activityFilters.currentRunOnly, true);
  assert.equal(session.activityFilters.autoScroll, false);
  assert.equal(session.activityFilters.terminal, undefined);
  assert.equal(session.activityFilters.categories.terminal, false);
});

test("saveShellSession writes normalized persisted shell state", () => {
  const storage = memoryStorage();
  const saved = saveShellSession(storage, {
    address: "127.0.0.1:44777",
    autoReconnect: true,
    lastConnected: true,
    selectedArtifactPath: ".orchestrator/artifacts/context/latest.json",
  });

  assert.equal(saved.address, "127.0.0.1:44777");
  assert.match(storage.getItem("orchestrator-v2-shell.session"), /selectedArtifactPath/);
  assert.equal(storage.getItem("orchestrator-v2-shell.address"), "127.0.0.1:44777");
});

test("reconnect helpers provide practical backoff and status messaging", () => {
  assert.equal(nextReconnectDelay(1), 1000);
  assert.equal(nextReconnectDelay(2), 2000);
  assert.equal(nextReconnectDelay(6), 15000);

  const details = buildConnectionDetails(
    { connected: false, status: "error", address: "http://127.0.0.1:44777", message: "event stream returned HTTP 503" },
    { enabled: true, pending: true, delayMs: 4000 },
  );
  assert.match(details, /Auto-reconnect will retry/);
  assert.match(details, /4s/);
});

test("formatProtocolError preserves actionable protocol details", () => {
  const formatted = formatProtocolError("status", {
    message: "Unable to reach control server at http://127.0.0.1:44777",
    code: "network_unreachable",
    status: 503,
  }, "Try starting orchestrator control serve.");

  assert.match(formatted.message, /status:/);
  assert.match(formatted.message, /network_unreachable/);
  assert.match(formatted.message, /HTTP 503/);
});

test("dogfood startup helper launches the control server and shell together", () => {
  const scriptPath = path.resolve(__dirname, "..", "..", "..", "scripts", "start-v2-dogfood.ps1");
  const launcherPath = path.resolve(__dirname, "..", "..", "..", "scripts", "Launch-Orchestrator-V2-Shell.vbs");
  const script = fs.readFileSync(scriptPath, "utf8");
  const launcher = fs.readFileSync(launcherPath, "utf8");

  assert.match(script, /param\(/);
  assert.match(script, /\[string\]\$ControlAddr = "127\.0\.0\.1:44777"/);
  assert.match(script, /control serve --addr \$ControlAddr/);
  assert.match(script, /npm run dev/);
  assert.match(script, /ORCHESTRATOR_V2_SHELL_ADDR/);
  assert.match(script, /http:\/\/\$ControlAddr/);
  assert.match(script, /\[switch\]\$SkipBuild/);
  assert.match(script, /\[switch\]\$SkipInstall/);
  assert.match(script, /\[switch\]\$DebugVisibleWindows/);
  assert.match(script, /-WindowStyle Hidden/);
  assert.match(script, /Wait-Process -Id \$shellProcess\.Id/);
  assert.match(script, /Stop-Process -Id \$controlProcess\.Id/);
  assert.match(script, /Invoke-RestMethod/);
  assert.match(launcher, /WindowStyle Hidden/);
  assert.match(launcher, /start-v2-dogfood\.ps1/);
});

test("buildActivityTimelineViewModel filters by category, run, and search text", () => {
  const vm = buildActivityTimelineViewModel([
    {
      event: "planner_turn_completed",
      sequence: 3,
      at: "2026-04-23T10:00:00Z",
      payload: { run_id: "run_1", summary: "planner done" },
    },
    {
      event: "worker_dispatch_completed",
      sequence: 4,
      at: "2026-04-23T10:01:00Z",
      payload: { run_id: "run_2", worker_id: "worker_1" },
    },
    {
      event: "terminal_session_started",
      sequence: "local-1",
      at: "2026-04-23T10:02:00Z",
      payload: { session_id: "terminal_1" },
      local: true,
      summary: "terminal_session_started PowerShell 1",
    },
  ], {
    currentRunOnly: true,
    currentRunID: "run_1",
    searchText: "planner",
    categories: {
      planner: true,
      executor: true,
      worker: false,
      approval: true,
      intervention: true,
      fault: true,
      terminal: true,
      status: true,
      other: true,
    },
  });

  assert.equal(vm.totalCount, 3);
  assert.equal(vm.filteredCount, 1);
  assert.equal(vm.items[0].eventName, "planner_turn_completed");
  assert.equal(vm.items[0].category, "planner");
});

test("buildActivityTimelineViewModel maps verbosity to visible output and raw payloads", () => {
  const events = [
    { event: "terminal_session_started", sequence: "local-1", at: "2026-04-23T10:00:00Z", payload: {}, local: true },
    { event: "executor_turn_failed", sequence: 2, at: "2026-04-23T10:01:00Z", payload: { error_message: "The model `gpt-5.5` does not exist or you do not have access to it." } },
  ];

  const quiet = buildActivityTimelineViewModel(events, { verbosity: "quiet" });
  const trace = buildActivityTimelineViewModel(events, { verbosity: "trace" });

  assert.equal(quiet.filteredCount, 1);
  assert.equal(quiet.items[0].eventName, "executor_turn_failed");
  assert.equal(quiet.items[0].severity, "danger");
  assert.equal(quiet.items[0].showPayload, false);
  assert.equal(trace.filteredCount, 2);
  assert.equal(trace.items[0].showPayload, true);
});

test("buildTerminalTabsViewModel normalizes active session state", () => {
  const vm = buildTerminalTabsViewModel({
    count: 2,
    active_session_id: "terminal_2",
    active_session: {
      session_id: "terminal_2",
      label: "PowerShell 2",
      status: "running",
      buffered_output: "console shell",
      message: "terminal session running",
    },
    sessions: [
      {
        session_id: "terminal_1",
        label: "PowerShell 1",
        status: "stopped",
        shell_label: "PowerShell",
        pid: null,
        exit_code: 0,
      },
      {
        session_id: "terminal_2",
        label: "PowerShell 2",
        status: "running",
        shell_label: "PowerShell",
        pid: 4242,
        exit_code: null,
      },
    ],
  });

  assert.equal(vm.count, 2);
  assert.equal(vm.activeSessionID, "terminal_2");
  assert.equal(vm.sessions[1].selected, true);
  assert.equal(vm.canStop, true);
  assert.equal(vm.output, "console shell");
});

test("buildWorkerPanelViewModel preserves selected worker detail", () => {
  const vm = buildWorkerPanelViewModel({
    count: 2,
    counts_by_status: { idle: 1, completed: 1 },
    items: [
      {
        worker_id: "worker_1",
        worker_name: "code-survey",
        status: "idle",
        scope: "inspect UI shell",
      },
      {
        worker_id: "worker_2",
        worker_name: "integration-pass",
        status: "completed",
        scope: "review worker outputs",
      },
    ],
  }, "worker_2");

  assert.equal(vm.selectedWorkerID, "worker_2");
  assert.equal(vm.selectedWorker.workerName, "integration-pass");
  assert.equal(vm.items[1].selected, true);
});

test("streamControlEvents parses NDJSON event payloads", async () => {
  const encoder = new TextEncoder();
  const events = [];
  const responseBody = new ReadableStream({
    start(controller) {
      controller.enqueue(encoder.encode('{"type":"event","event":"status_snapshot_emitted","sequence":1,"at":"2026-04-22T01:00:00Z","payload":{"run_id":"run_1"}}\n'));
      controller.enqueue(encoder.encode('{"type":"event","event":"control_message_queued","sequence":2,"at":"2026-04-22T01:00:01Z","payload":{"run_id":"run_1"}}\n'));
      controller.close();
    },
  });

  await streamControlEvents("127.0.0.1:44777", { runID: "run_1" }, (event) => {
    events.push(event);
  }, {
    fetchImpl: async () => new Response(responseBody, { status: 200 }),
  });

  assert.equal(events.length, 2);
  assert.equal(events[0].event, "status_snapshot_emitted");
  assert.equal(events[1].event, "control_message_queued");
});

test("buildStatusViewModel normalizes status snapshot for the shell", () => {
  const vm = buildStatusViewModel({
    runtime: { verbosity: "trace" },
    run: {
      id: "run_42",
      goal: "Ship the next milestone",
      stop_reason: "planner_ask_human",
      elapsed_label: "stopped after 00:14:32",
      elapsed_seconds: 872,
      executor_last_error: "None",
      completed: false,
    },
    planner_status: {
      present: true,
      operator_message: "Implementing the next bounded step.",
      current_focus: "settings and wiring",
      next_intended_step: "dispatch the settings page implementation",
      why_this_step: "UI skeleton is already in place",
      progress_percent: 48,
    },
    pending_action: {
      present: true,
      pending_action_summary: "dispatch settings implementation",
      held: true,
    },
  });

  assert.equal(vm.runID, "run_42");
  assert.equal(vm.elapsedLabel, "stopped after 00:14:32");
  assert.equal(vm.elapsedSeconds, 872);
  assert.equal(vm.operatorMessage, "Implementing the next bounded step.");
  assert.equal(vm.progressPercent, 48);
  assert.equal(vm.pendingActionSummary, "dispatch settings implementation");
  assert.equal(vm.pendingHeld, true);
});

test("buildPendingActionViewModel exposes pending action detail", () => {
  const vm = buildPendingActionViewModel({
    pending_action: {
      present: true,
      planner_outcome: "execute",
      pending_action_summary: "dispatch the executor step",
      held: true,
      hold_reason: "control_message_queued",
      pending_executor_prompt_summary: "Implement the settings shell",
      pending_executor_prompt: "Edit the settings page and wire save behavior.",
      pending_dispatch_target: { kind: "worker", worker_name: "ui-shell" },
      updated_at: "2026-04-22T15:00:00Z",
    },
  });

  assert.equal(vm.plannerOutcome, "execute");
  assert.equal(vm.summary, "dispatch the executor step");
  assert.equal(vm.held, true);
  assert.equal(vm.holdReason, "control_message_queued");
  assert.equal(vm.executorPromptSummary, "Implement the settings shell");
  assert.equal(vm.dispatchTarget, "worker / ui-shell");
  assert.equal(vm.updatedAt, "2026-04-22T15:00:00Z");
});

test("buildArtifactListViewModel normalizes artifact listings for the shell", () => {
  const vm = buildArtifactListViewModel({
    artifacts: {
      latest_path: ".orchestrator/artifacts/context/run_1/latest.json",
    },
  }, {
    latest_path: ".orchestrator/artifacts/context/run_1/latest.json",
    items: [
      {
        path: ".orchestrator/artifacts/context/run_1/latest.json",
        category: "context",
        source: "collected_context",
        latest: true,
        preview: "{\"focus\":\"inspect wiring\"}",
        at: "2026-04-22T15:10:00Z",
      },
    ],
  });

  assert.equal(vm.latestPath, ".orchestrator/artifacts/context/run_1/latest.json");
  assert.equal(vm.items.length, 1);
  assert.equal(vm.items[0].category, "context");
  assert.equal(vm.items[0].source, "collected_context");
});

test("buildApprovalViewModel normalizes approval state for the shell", () => {
  const vm = buildApprovalViewModel({
    approval: {
      present: true,
      state: "required",
      kind: "command_execution",
      summary: "executor approval required for command: go test ./...",
      run_id: "run_55",
      executor_turn_id: "turn_approval",
      executor_thread_id: "thread_approval",
      command: "go test ./...",
      available_actions: ["approve", "deny"],
      worker_approval_required: 1,
    },
  });

  assert.equal(vm.present, true);
  assert.equal(vm.state, "required");
  assert.equal(vm.kind, "command_execution");
  assert.equal(vm.runID, "run_55");
  assert.equal(vm.executorTurnID, "turn_approval");
  assert.deepEqual(vm.availableActions, ["approve", "deny"]);
  assert.equal(vm.workerApprovalRequired, 1);
});

test("buildRunSummaryViewModel derives a compact return summary from real protocol data", () => {
  const vm = buildRunSummaryViewModel({
    runtime: { verbosity: "verbose" },
    run: {
      id: "run_99",
      goal: "Improve the shell",
      stop_reason: "executor_approval_required",
      completed: false,
    },
    planner_status: {
      present: true,
      operator_message: "Preparing the next shell improvement.",
      progress_percent: 57,
    },
    pending_action: {
      present: true,
      pending_action_summary: "dispatch artifact viewer refinements",
    },
    artifacts: {
      latest_path: ".orchestrator/artifacts/context/run_99/collected_context.json",
    },
  }, null, [
    { event: "control_message_queued", payload: { run_id: "run_99" } },
    { event: "planner_turn_completed", payload: { run_id: "run_99" } },
  ]);

  assert.equal(vm.runState, "executor_approval_required");
  assert.equal(vm.operatorMessage, "Preparing the next shell improvement.");
  assert.equal(vm.progress, "57%");
  assert.equal(vm.pendingAction, "dispatch artifact viewer refinements");
  assert.equal(vm.approval, "No approval is currently required");
  assert.equal(vm.latestArtifact, ".orchestrator/artifacts/context/run_99/collected_context.json");
  assert.deepEqual(vm.recentEvents, [
    "Your control message was queued run=run_99.",
    "Planner finished choosing the next step run=run_99.",
  ]);
});

test("buildSideChatViewModel normalizes recorded side chat messages", () => {
  const vm = buildSideChatViewModel({
    available: true,
    count: 1,
    items: [
      {
        id: "sidechat_1",
        raw_text: "What remains before release?",
        source: "side_chat",
        status: "recorded",
        backend_state: "unavailable",
        response_message: "side chat backend is not implemented in this slice",
        created_at: "2026-04-22T16:00:00Z",
        run_id: "run_22",
        context_policy: "repo_and_latest_run_summary",
      },
    ],
  });

  assert.equal(vm.count, 1);
  assert.equal(vm.items[0].rawText, "What remains before release?");
  assert.equal(vm.items[0].backendState, "unavailable");
  assert.equal(vm.items[0].runID, "run_22");
});

test("buildDogfoodIssuesViewModel normalizes captured dogfood notes", () => {
  const vm = buildDogfoodIssuesViewModel({
    available: true,
    count: 1,
    items: [
      {
        id: "dogfood_1",
        repo_path: "D:/repo",
        run_id: "run_22",
        source: "operator_shell",
        title: "Reconnect shows stale artifact",
        note: "The artifact viewer kept the old path after reconnect.",
        created_at: "2026-04-23T08:00:00Z",
        updated_at: "2026-04-23T08:00:00Z",
      },
    ],
  }, "dogfood_1");

  assert.equal(vm.count, 1);
  assert.equal(vm.selectedIssueID, "dogfood_1");
  assert.equal(vm.selectedIssue.title, "Reconnect shows stale artifact");
  assert.equal(vm.selectedIssue.runID, "run_22");
});

test("buildWorkerPanelViewModel normalizes worker visibility for the shell", () => {
  const vm = buildWorkerPanelViewModel({
    count: 1,
    counts_by_status: { executor_active: 1 },
    items: [
      {
        worker_id: "worker_1",
        worker_name: "code-survey",
        status: "executor_active",
        scope: "inspect UI shell",
        worktree_path: "D:/repo.workers/code-survey",
        approval_required: true,
        approval_kind: "command_execution",
        executor_thread_id: "thread_worker",
        executor_turn_id: "turn_worker",
        interruptible: true,
        steerable: false,
        last_control_action: "approved",
        worker_task_summary: "inspect the shell layout",
        worker_result_summary: "survey completed",
        worker_error_summary: "",
        updated_at: "2026-04-22T16:10:00Z",
      },
    ],
  }, "worker_1");

  assert.equal(vm.count, 1);
  assert.equal(vm.countsByStatus.executor_active, 1);
  assert.equal(vm.items[0].workerName, "code-survey");
  assert.equal(vm.items[0].approvalRequired, true);
  assert.equal(vm.items[0].worktreePath, "D:/repo.workers/code-survey");
  assert.equal(vm.selectedWorkerID, "worker_1");
  assert.equal(vm.selectedWorker.workerID, "worker_1");
});

test("buildAutofillViewModel normalizes drafted contract files", () => {
  const vm = buildAutofillViewModel({
    available: true,
    message: "Drafted the requested files.",
    model: "gpt-5.2",
    generated_at: "2026-04-22T17:00:00Z",
    response_id: "resp_autofill",
    files: [
      {
        path: ".orchestrator/brief.md",
        summary: "Crisp brief",
        content: "# Brief",
        existing: true,
        existing_mtime: "2026-04-22T16:55:00Z",
      },
    ],
  });

  assert.equal(vm.model, "gpt-5.2");
  assert.equal(vm.generatedAt, "2026-04-22T17:00:00Z");
  assert.equal(vm.files[0].path, ".orchestrator/brief.md");
  assert.equal(vm.files[0].existing, true);
});

test("buildRepoTreeViewModel normalizes repo browser data", () => {
  const vm = buildRepoTreeViewModel({
    path: "cmd",
    parent_path: "",
    count: 2,
    items: [
      {
        name: "app.go",
        path: "cmd/app.go",
        kind: "file",
        read_only: true,
        editable_via_contract_editor: false,
        byte_size: 128,
        modified_at: "2026-04-22T17:10:00Z",
      },
      {
        name: "ui",
        path: "cmd/ui",
        kind: "directory",
        read_only: true,
        editable_via_contract_editor: false,
        modified_at: "2026-04-22T17:11:00Z",
      },
    ],
  }, {
    available: true,
    path: ".orchestrator/brief.md",
    content_type: "text/markdown",
    content: "# Brief",
    byte_size: 16,
    truncated: false,
    read_only: true,
    editable_via_contract_editor: true,
  });

  assert.equal(vm.path, "cmd");
  assert.equal(vm.count, 2);
  assert.equal(vm.items[0].path, "cmd/app.go");
  assert.equal(vm.openFile.path, ".orchestrator/brief.md");
  assert.equal(vm.openFile.editableViaContractEditor, true);
});

test("formatEventSummary keeps event stream rows readable", () => {
  const summary = formatEventSummary({
    event: "control_message_queued",
    payload: { run_id: "run_88" },
  });

  assert.equal(summary, "Your control message was queued run=run_88.");
});

test("classifyActivityCategory keeps intervention and terminal events distinct", () => {
  assert.equal(classifyActivityCategory({ event: "control_message_queued" }), "intervention");
  assert.equal(classifyActivityCategory({ event: "terminal_session_started" }), "terminal");
  assert.equal(classifyActivityCategory({ event: "worker_dispatch_completed" }), "worker");
});
