const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");

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
  checkForUpdates,
  getUpdateStatus,
  getUpdateChangelog,
  setVerbosity,
  setStopSafe,
  clearStopFlag,
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
  shutdownWindowStateByKey,
  shutdownAllWindowStates,
} = require("../electron/window-state-cleanup");
const {
  buildStatusViewModel,
  buildConnectionStatusViewModel,
  buildRepoBindingViewModel,
  buildLoopStatusViewModel,
  buildVerbosityViewModel,
  buildRecommendedActionViewModel,
  buildRunControlStateViewModel,
  buildTopStatusViewModel,
  buildHomeDashboardViewModel,
  translateStopReason,
  buildProgressPanelViewModel,
  buildActivityTimelineViewModel,
  buildTerminalTabsViewModel,
  buildPendingActionViewModel,
  buildAskHumanViewModel,
  buildArtifactListViewModel,
  buildApprovalViewModel,
  buildRunSummaryViewModel,
  buildWhatHappenedViewModel,
  buildLatestErrorViewModel,
  buildDebugBundleText,
  buildModelHealthBundleText,
  buildCodexReadinessViewModel,
  normalizeModelHealthSnapshot,
  modelUnavailableFromText,
  buildSideChatViewModel,
  buildDogfoodIssuesViewModel,
  buildWorkerPanelViewModel,
  buildAutofillViewModel,
  buildRepoTreeViewModel,
  classifyActivityCategory,
  formatEventSummary,
} = require("../src/renderer/view-model");
const {
  loadBackendMetadata,
  isOwnedBackend,
  metadataMatchesAddress,
  ownerMarker,
  fileModifiedTime,
  listenerMatchesOwnedMetadata,
  clearOwnedBackendPort,
  restartOwnedBackend,
} = require("../electron/backend-manager");

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

test("clear-stop recovery uses explicit clear_stop_flag before continue_run", async () => {
  const requestBodies = [];
  const fetchImpl = async (_url, init) => {
    requestBodies.push(init.body);
    return new Response(JSON.stringify({
      type: "response",
      ok: true,
      payload: { ok: true, run_id: "run_stop" },
    }), { status: 200 });
  };

  await clearStopFlag("127.0.0.1:44777", "run_stop", { fetchImpl });
  await continueRun("127.0.0.1:44777", {
    run_id: "run_stop",
    repo_path: "D:/repo",
  }, { fetchImpl });

  assert.match(requestBodies[0], /"action":"clear_stop_flag"/);
  assert.match(requestBodies[0], /"run_id":"run_stop"/);
  assert.match(requestBodies[1], /"action":"continue_run"/);
});

test("active-run recovery protocol helpers use explicit control actions", async () => {
  const requestBodies = [];
  const fetchImpl = async (_url, init) => {
    requestBodies.push(init.body);
    return new Response(JSON.stringify({
      type: "response",
      ok: true,
      payload: {
        present: true,
        recovered: true,
        active_guard_cleared: true,
        run_id: "run_stale",
      },
    }), { status: 200 });
  };

  await getActiveRunGuard("127.0.0.1:44777", { fetchImpl });
  await recoverStaleRun("127.0.0.1:44777", {
    run_id: "run_stale",
    reason: "operator_recovery",
    force: true,
  }, { fetchImpl });

  assert.match(requestBodies[0], /"action":"get_active_run_guard"/);
  assert.match(requestBodies[1], /"action":"recover_stale_run"/);
  assert.match(requestBodies[1], /"run_id":"run_stale"/);
  assert.match(requestBodies[1], /"force":true/);
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

test("runtime config and update helpers use explicit protocol actions", async () => {
  const requestBodies = [];
  const fetchImpl = async (_url, init) => {
    requestBodies.push(init.body);
    return new Response(JSON.stringify({
      type: "response",
      ok: true,
      payload: { ok: true, latest_version: "v1.1.0" },
    }), { status: 200 });
  };

  await getRuntimeConfig("127.0.0.1:44777", { fetchImpl });
  await setRuntimeConfig("127.0.0.1:44777", {
    timeouts: { executor_turn_timeout: "unlimited" },
    permission_profile: "autonomous",
  }, { fetchImpl });
  await checkForUpdates("127.0.0.1:44777", {}, { fetchImpl });
  await getUpdateStatus("127.0.0.1:44777", { fetchImpl });
  await getUpdateChangelog("127.0.0.1:44777", {}, { fetchImpl });

  assert.match(requestBodies[0], /"action":"get_runtime_config"/);
  assert.match(requestBodies[1], /"action":"set_runtime_config"/);
  assert.match(requestBodies[1], /"executor_turn_timeout":"unlimited"/);
  assert.match(requestBodies[1], /"permission_profile":"autonomous"/);
  assert.match(requestBodies[2], /"action":"check_for_updates"/);
  assert.match(requestBodies[3], /"action":"get_update_status"/);
  assert.match(requestBodies[4], /"action":"get_update_changelog"/);
});

test("renderer exposes every timeout field and side-chat quick actions", () => {
  const root = path.resolve(__dirname, "..");
  const html = fs.readFileSync(path.join(root, "src", "renderer", "index.html"), "utf8");
  const app = fs.readFileSync(path.join(root, "src", "renderer", "app.js"), "utf8");

  for (const id of [
    "timeout-planner-request",
    "timeout-executor-idle",
    "timeout-executor-turn",
    "timeout-subagent",
    "timeout-shell-command",
    "timeout-install",
    "timeout-human-wait",
    "side-chat-what-now",
    "side-chat-what-changed",
    "side-chat-explain-blocker",
    "side-chat-ask-planner-reconsider",
    "side-chat-safe-stop",
    "side-chat-copy-conversation",
    "side-chat-copy-support",
  ]) {
    assert.match(html, new RegExp(`id="${id}"`));
  }
  assert.match(app, /timeoutExecutorIdle/);
  assert.match(app, /timeoutSubagent/);
  assert.match(app, /sideChatActionRequest/);
});

test("normalizeModelHealthSnapshot lets newer successful tests clear stale Codex model errors", () => {
  const snapshot = {
    run: {
      stopped_at: "2026-04-24T02:00:00Z",
      executor_last_error: "The model `gpt-5.5` does not exist or you do not have access to it.",
    },
    model_health: {
      planner: { component: "planner", configured_model: "gpt-5-latest", verification_state: "not_verified" },
      executor: {
        component: "executor",
        configured_model: "gpt-5.5",
        requested_model: "gpt-5.5",
        verification_state: "invalid",
        model_unavailable: true,
        last_error: "The model `gpt-5.5` does not exist or you do not have access to it.",
      },
      needs_attention: true,
      blocking: true,
    },
  };
  const normalized = normalizeModelHealthSnapshot(snapshot, {
    planner: {
      planner: {
        component: "planner",
        configured_model: "gpt-5-latest",
        requested_model: "gpt-5.4",
        resolved_model: "gpt-5.4",
        verified_model: "gpt-5.4-2026-03-05",
        verification_state: "verified",
        test_performed: true,
        last_tested_at: "2026-04-24T02:05:00Z",
      },
    },
    executor: {
      executor: {
        component: "executor",
        configured_model: "gpt-5.5",
        requested_model: "gpt-5.5",
        verified_model: "gpt-5.5",
        verification_state: "verified",
        access_mode: "danger-full-access sandbox, approval never",
        effort: "xhigh",
        codex_model_verified: true,
        codex_permission_mode_verified: true,
        codex_executable_path: "C:/Users/me/AppData/Roaming/npm/codex.cmd",
        codex_version: "codex-cli 0.124.0",
        test_performed: true,
        last_tested_at: "2026-04-24T02:06:00Z",
      },
    },
  });

  assert.equal(normalized.model_health.blocking, false);
  assert.equal(normalized.model_health.needs_attention, false);
  assert.equal(normalized.model_health.executor.verification_state, "verified");
  assert.equal(normalized.model_health.executor.model_unavailable, false);
  assert.equal(buildCodexReadinessViewModel(normalized).modelInvalid, false);
  assert.equal(buildLatestErrorViewModel(normalized, []).present, false);
});

test("normalizeModelHealthSnapshot keeps newer failed test blocking after prior success", () => {
  const normalized = normalizeModelHealthSnapshot({
    run: { stopped_at: "2026-04-24T02:00:00Z" },
    model_health: {
      planner: {
        component: "planner",
        configured_model: "gpt-5.4",
        verification_state: "verified",
        verified_model: "gpt-5.4",
      },
      executor: {
        component: "executor",
        configured_model: "gpt-5.5",
        verification_state: "verified",
        verified_model: "gpt-5.5",
        access_mode: "danger-full-access sandbox, approval never",
        effort: "xhigh",
        codex_model_verified: true,
        codex_permission_mode_verified: true,
        last_tested_at: "2026-04-24T02:01:00Z",
      },
    },
  }, {
    executor: {
      executor: {
        component: "executor",
        configured_model: "gpt-5.5",
        requested_model: "gpt-5.5",
        verification_state: "invalid",
        model_unavailable: true,
        last_error: "The model `gpt-5.5` does not exist or you do not have access to it.",
        test_performed: true,
        last_tested_at: "2026-04-24T02:07:00Z",
      },
    },
  });

  assert.equal(normalized.model_health.blocking, true);
  assert.equal(normalized.model_health.executor.verification_state, "invalid");
});

test("buildModelHealthBundleText includes backend identity and redacts secrets", () => {
  const bundle = buildModelHealthBundleText({
    runtime: { repo_root: "D:/repo" },
    backend: {
      pid: 1234,
      started_at: "2026-04-24T02:00:00Z",
      binary_path: "D:/Projects/agentic_loop/dist/orchestrator.exe",
      binary_version: "v1.0.1-dev",
      stale: false,
    },
    model_health: {
      planner: {
        configured_model: "gpt-5-latest",
        verified_model: "gpt-5.4",
        verification_state: "verified",
      },
      executor: {
        configured_model: "gpt-5.5",
        requested_model: "gpt-5.5",
        verified_model: "gpt-5.5",
        verification_state: "verified",
        access_mode: "danger-full-access sandbox, approval never",
        effort: "xhigh",
        codex_model_verified: true,
        codex_permission_mode_verified: true,
        codex_executable_path: "C:/Users/me/AppData/Roaming/npm/codex.cmd",
        codex_version: "codex-cli 0.124.0",
        codex_config_source: "C:/Users/me/.codex/config.toml",
        last_error: "api_key=sk-secretsecretsecret",
      },
      needs_attention: false,
      blocking: false,
      message: "Planner and Codex requirements verified.",
    },
  }, {
    now: "2026-04-24T02:10:00Z",
    address: "http://127.0.0.1:44777",
  });

  assert.match(bundle, /Backend PID: 1234/);
  assert.match(bundle, /Planner verified: yes/);
  assert.match(bundle, /Codex verified: yes/);
  assert.doesNotMatch(bundle, /sk-secretsecretsecret/);
  assert.match(bundle, /api_key: \[REDACTED\]/);
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
      payload: { available: true, stored: true },
    }), { status: 200 });
  };

  await sendSideChatMessage("127.0.0.1:44777", {
    repo_path: "D:/repo",
    message: "What remains before release?",
    context_policy: "repo_and_latest_run_summary",
  }, { fetchImpl });
  await listSideChatMessages("127.0.0.1:44777", { repo_path: "D:/repo", limit: 10 }, { fetchImpl });
  await sideChatContextSnapshot("127.0.0.1:44777", { repo_path: "D:/repo", run_id: "run_1" }, { fetchImpl });
  await sideChatActionRequest("127.0.0.1:44777", {
    repo_path: "D:/repo",
    run_id: "run_1",
    action: "ask_planner_reconsider",
    message: "Please reconsider emulator setup.",
    approved: true,
  }, { fetchImpl });

  assert.match(requestBodies[0], /"action":"send_side_chat_message"/);
  assert.match(requestBodies[0], /"context_policy":"repo_and_latest_run_summary"/);
  assert.match(requestBodies[1], /"action":"list_side_chat_messages"/);
  assert.match(requestBodies[2], /"action":"side_chat_context_snapshot"/);
  assert.match(requestBodies[2], /"run_id":"run_1"/);
  assert.match(requestBodies[3], /"action":"side_chat_action_request"/);
  assert.match(requestBodies[3], /"ask_planner_reconsider"/);
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
  assert.equal(vm.sections.length, 5);
  assert.equal(vm.sections.find((section) => section.id === "roadmap_alignment").open, false);
  assert.equal(vm.sections.find((section) => section.id === "progress_basis").label, "Progress Basis");
});

test("long roadmap and planner progress text is grouped into collapsible status sections", () => {
  const longText = "Roadmap alignment ".repeat(40);
  const vm = buildProgressPanelViewModel({
    planner_status: {
      present: true,
      progress_basis: "The implementation is blocked by a long diagnostic explanation. ".repeat(8),
      current_focus: "Status readability",
      next_intended_step: "Make the status panel readable",
      why_this_step: "Dogfooding showed narrow cards were hiding useful context.",
    },
    roadmap: {
      present: true,
      alignment_text: longText,
    },
  });

  const roadmap = vm.sections.find((section) => section.id === "roadmap_alignment");
  const basis = vm.sections.find((section) => section.id === "progress_basis");
  assert.equal(roadmap.isLong, true);
  assert.equal(roadmap.open, false);
  assert.match(roadmap.preview, /\.\.\.$/);
  assert.equal(basis.isLong, true);
  assert.equal(basis.open, false);
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

test("stale active-run guard recommends mechanical recovery and blocks run controls", () => {
  const snapshot = {
    runtime: { repo_root: "D:/repo", repo_ready: true },
    active_run_guard: {
      present: true,
      stale: true,
      run_id: "run_stale",
      backend_pid: 1234,
      session_id: "session_old",
      message: "This run was active under a previous backend process and may be stale.",
    },
    run: {
      id: "run_stale",
      status: "initialized",
      completed: false,
      resumable: false,
    },
  };

  const recommended = buildRecommendedActionViewModel(snapshot, {
    connection: { connected: true, status: "connected" },
    goalEntered: true,
  });
  const approval = buildApprovalViewModel(snapshot);
  const loop = buildLoopStatusViewModel(snapshot);
  const controls = buildRunControlStateViewModel(snapshot, {
    connection: { connected: true },
    goalEntered: true,
  });

  assert.equal(recommended.state, "recovery_needed");
  assert.equal(recommended.primaryAction.id, "recover_backend");
  assert.equal(approval.needsAttention, true);
  assert.equal(approval.badgeCount, 1);
  assert.equal(approval.staleGuard.runID, "run_stale");
  assert.equal(loop.state, "recovery_needed");
  assert.equal(controls.startEnabled, false);
  assert.equal(controls.continueEnabled, false);
  assert.match(controls.note, /Recover Backend/);
});

test("cleared stale active-run guard re-enables continue for execute-ready runs", () => {
  const snapshot = {
    runtime: { repo_root: "D:/repo", repo_ready: true },
    active_run_guard: {
      present: false,
      stale: false,
      message: "no active run guard is currently recorded",
    },
    run: {
      id: "run_recovered",
      status: "initialized",
      completed: false,
      resumable: true,
      next_operator_action: "continue_existing_run",
      latest_planner_outcome: "execute",
      execute_ready: true,
      waiting_at_safe_point: true,
      latest_checkpoint: {
        stage: "planner",
        label: "planner_turn_completed",
        safe_pause: true,
      },
      executor_turn_status: "",
      executor_thread_id: "",
      executor_turn_id: "",
    },
  };

  const recommended = buildRecommendedActionViewModel(snapshot, {
    connection: { connected: true, status: "connected" },
    goalEntered: true,
  });
  const loop = buildLoopStatusViewModel(snapshot);
  const controls = buildRunControlStateViewModel(snapshot, {
    connection: { connected: true },
    goalEntered: true,
  });
  const bundle = buildDebugBundleText(snapshot, null, [], { now: "2026-04-24T12:00:00Z" });

  assert.equal(recommended.state, "ready_to_continue");
  assert.equal(recommended.primaryAction.id, "continue_run");
  assert.equal(loop.state, "ready_to_continue");
  assert.equal(controls.continueEnabled, true);
  assert.equal(controls.continueLabel, "Continue Build / Dispatch Executor");
  assert.match(bundle, /Active-run guard present: no/);
  assert.match(bundle, /Continue enabled: true/);
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
      kind: "command_execution",
      summary: "executor approval required for command: go test ./...",
    },
  }, {
    connection: { connected: true, status: "connected" },
  });

  assert.equal(vm.state, "approval_required");
  assert.equal(vm.title, "Review the action required.");
  assert.equal(vm.primaryAction.id, "review_approval");
});

test("ask_human status becomes Action Required and points to answer flow", () => {
  const snapshot = {
    run: {
      id: "run_question",
      status: "stopped",
      completed: false,
      resumable: true,
      stop_reason: "planner_ask_human",
      next_operator_action: "answer_human_question",
      latest_planner_outcome: "ask_human",
    },
    ask_human: {
      present: true,
      run_id: "run_question",
      question: "Did you verify that Codex gpt-5.5 access is fixed?",
      blocker: "The planner paused because executor model access was previously failing.",
      action_summary: "wait for human confirmation",
      planner_outcome: "ask_human",
      source: "human.question.presented",
    },
  };

  const askHuman = buildAskHumanViewModel(snapshot);
  const approval = buildApprovalViewModel(snapshot);
  const recommended = buildRecommendedActionViewModel(snapshot, {
    connection: { connected: true, status: "connected" },
  });
  const loop = buildLoopStatusViewModel(snapshot);

  assert.equal(askHuman.present, true);
  assert.match(askHuman.question, /gpt-5\.5 access/);
  assert.equal(approval.needsAttention, true);
  assert.equal(approval.badgeCount, 1);
  assert.equal(approval.askHuman.present, true);
  assert.equal(approval.canApprove, false);
  assert.equal(approval.canDeny, false);
  assert.equal(recommended.state, "ask_human");
  assert.equal(recommended.primaryAction.id, "answer_ask_human");
  assert.equal(recommended.primaryAction.target, "attention");
  assert.equal(loop.state, "needs_you");
});

test("pending ask_human action is enough to show planner answer workflow", () => {
  const snapshot = {
    run: {
      id: "run_pending_question",
      status: "stopped",
      completed: false,
      resumable: true,
    },
    pending_action: {
      available: true,
      present: true,
      held: true,
      turn_type: "ask_human",
      planner_outcome: "ask_human",
      pending_action_summary: "Please confirm whether the model/access issue is fixed.",
      pending_reason: "planner_selected_ask_human",
    },
  };

  const askHuman = buildAskHumanViewModel(snapshot);
  const approval = buildApprovalViewModel(snapshot);
  const recommended = buildRecommendedActionViewModel(snapshot, {
    connection: { connected: true, status: "connected" },
  });

  assert.equal(askHuman.present, true);
  assert.match(askHuman.question, /model\/access issue/);
  assert.equal(approval.needsAttention, true);
  assert.equal(recommended.primaryAction.id, "answer_ask_human");
});

test("run control explains disabled continue while ask_human answer is pending", () => {
  const vm = buildRunControlStateViewModel({
    run: {
      id: "run_waiting",
      status: "stopped",
      completed: false,
      resumable: true,
      stop_reason: "planner_ask_human",
      next_operator_action: "answer_human_question",
    },
    ask_human: {
      present: true,
      question: "Is the model issue fixed?",
    },
  }, {
    connection: { connected: true, status: "connected" },
    goalEntered: true,
  });

  assert.equal(vm.askHuman.present, true);
  assert.equal(vm.startEnabled, false);
  assert.equal(vm.continueEnabled, false);
  assert.match(vm.startDisabledReason, /unfinished run/i);
  assert.match(vm.continueDisabledReason, /waiting for your answer/i);
  assert.match(vm.note, /Action Required/i);
});

test("safe stop state becomes Action Required with clear-stop continue guidance", () => {
  const snapshot = {
    stop_flag: {
      present: true,
      path: "D:/repo/.orchestrator/state/auto.stop",
      applies_at: "next_safe_point",
      reason: "operator_requested_safe_stop_from_shell",
    },
    run: {
      id: "run_safe_stop",
      status: "stopped",
      completed: false,
      resumable: true,
      stop_reason: "operator_stop_requested",
    },
  };

  const approval = buildApprovalViewModel(snapshot);
  const recommended = buildRecommendedActionViewModel(snapshot, {
    connection: { connected: true, status: "connected" },
    goalEntered: true,
  });
  const controls = buildRunControlStateViewModel(snapshot, {
    connection: { connected: true },
    goalEntered: true,
  });
  const loop = buildLoopStatusViewModel(snapshot);

  assert.equal(approval.needsAttention, true);
  assert.equal(approval.badgeCount, 1);
  assert.equal(approval.safeStop.present, true);
  assert.match(approval.summary, /Safe stop was requested/);
  assert.equal(recommended.state, "safe_stop_requested");
  assert.equal(recommended.primaryAction.id, "clear_stop_continue");
  assert.equal(controls.continueEnabled, false);
  assert.match(controls.continueDisabledReason, /safe stop was requested/i);
  assert.equal(loop.state, "safe_stop_requested");
});

test("non-actionable approval metadata does not create Action Required state", () => {
  const snapshot = {
    run: {
      id: "run_stale",
      status: "stopped",
      completed: false,
      resumable: true,
    },
    approval: {
      present: true,
      state: "none",
      kind: "Unavailable",
      run_id: "run_stale",
      executor_thread_id: "thread_old",
      executor_turn_id: "turn_old",
      available_actions: ["approve", "deny"],
      worker_approval_required: 0,
    },
    workers: {
      approval_required: 0,
    },
  };

  const approval = buildApprovalViewModel(snapshot);
  const recommended = buildRecommendedActionViewModel(snapshot, {
    connection: { connected: true, status: "connected" },
  });
  const loop = buildLoopStatusViewModel(snapshot);

  assert.equal(approval.present, false);
  assert.equal(approval.reportedPresent, true);
  assert.equal(approval.needsAttention, false);
  assert.equal(approval.badgeCount, 0);
  assert.equal(approval.canApprove, false);
  assert.equal(approval.canDeny, false);
  assert.deepEqual(approval.availableActions, []);
  assert.equal(recommended.state, "resumable");
  assert.notEqual(loop.state, "needs_you");
});

test("Unavailable approval kind and zero worker approvals are not actionable", () => {
  const vm = buildApprovalViewModel({
    approval: {
      present: true,
      state: "required",
      kind: "Unavailable",
      worker_approval_required: 0,
      available_actions: ["approve", "deny"],
    },
    workers: {
      approval_required: 0,
    },
  });

  assert.equal(vm.present, false);
  assert.equal(vm.needsAttention, false);
  assert.equal(vm.badgeCount, 0);
  assert.equal(vm.canApprove, false);
  assert.equal(vm.canDeny, false);
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
  assert.equal(what.primaryActions.some((action) => action.id === "test_model_health"), true);
});

test("debug bundle includes safe run diagnosis fields and excludes secrets", () => {
  const snapshot = {
    runtime: {
      repo_root: "D:/repo",
      version: "1.2.3-test",
      verbosity: "verbose",
    },
    run: {
      id: "run_debug",
      goal: "Fix the failing build without exposing api_key=sk-secret123456",
      status: "stopped",
      stop_reason: "executor_failed",
      completed: false,
      resumable: true,
      started_at: "2026-04-23T10:00:00Z",
      stopped_at: "2026-04-23T10:12:00Z",
      elapsed_label: "Stopped after 00:12:00",
      latest_planner_outcome: "execute",
      executor_status: "failed",
      executor_thread_id: "thread_1",
      executor_turn_id: "turn_1",
      executor_last_error: "The model `gpt-5.5` does not exist or you do not have access to it. Authorization: Bearer abcdef123456",
    },
    planner_status: {
      present: true,
      operator_message: "Codex failed before code changes.",
      progress_percent: 12,
      progress_confidence: "low",
      progress_basis: "No code changes were made.",
      current_focus: "Model configuration",
      next_intended_step: "Test model health",
      why_this_step: "The executor could not start.",
    },
    model_health: {
      planner: { configured_model: "gpt-5.4", verification_state: "verified" },
      executor: {
        requested_model: "gpt-5.5",
        verification_state: "invalid",
        effort: "xhigh",
        access_mode: "danger-full-access sandbox, approval never",
        codex_executable_path: "C:/Users/me/AppData/Roaming/npm/codex.cmd",
        codex_version: "codex-cli 0.124.0",
        model_unavailable: true,
        last_error: "The model `gpt-5.5` does not exist or you do not have access to it.",
      },
      blocking: true,
    },
    pending_action: {
      present: true,
      held: true,
      pending_action_summary: "dispatch executor prompt",
    },
    stop_flag: {
      present: true,
      reason: "operator_requested_safe_stop_from_shell",
      path: "D:/repo/.orchestrator/state/auto.stop",
      applies_at: "next_safe_point",
    },
    artifacts: {
      latest_path: ".orchestrator/artifacts/executor/run_debug/error.json",
    },
  };

  const bundle = buildDebugBundleText(snapshot, {
    latest_path: ".orchestrator/artifacts/executor/run_debug/error.json",
    items: [
      { path: ".orchestrator/artifacts/executor/run_debug/error.json", category: "executor", source: "runtime", at: "2026-04-23T10:12:00Z" },
    ],
  }, [
    { event: "executor_turn_failed", at: "2026-04-23T10:12:00Z", payload: { run_id: "run_debug", error_message: "The model `gpt-5.5` does not exist or you do not have access to it." } },
    { event: "stop_flag_detected", at: "2026-04-23T10:11:00Z", payload: { run_id: "run_debug" } },
  ], {
    now: "2026-04-23T10:13:00Z",
    sideChat: {
      items: [
        { created_at: "2026-04-23T10:10:00Z", raw_text: "note only" },
      ],
    },
  });

  assert.match(bundle, /Orchestrator V2 Run Debug Bundle/);
  assert.match(bundle, /Run id: run_debug/);
  assert.match(bundle, /Planner model: configured=gpt-5.4/);
  assert.match(bundle, /Codex model\/access: model=gpt-5.5/);
  assert.match(bundle, /Stop reason code: executor_failed/);
  assert.match(bundle, /Plain-English stop reason/);
  assert.match(bundle, /Executor thread id: thread_1/);
  assert.match(bundle, /Progress basis: No code changes were made/);
  assert.match(bundle, /Stop flag present: yes/);
  assert.match(bundle, /Latest stop_flag_detected event: 2026-04-23T10:11:00Z/);
  assert.match(bundle, /Side Chat affects active run: no/);
  assert.match(bundle, /Last side-chat action timestamp: 2026-04-23T10:10:00Z/);
  assert.match(bundle, /Secrets\/API keys\/auth tokens are intentionally excluded/);
  assert.doesNotMatch(bundle, /sk-secret123456/);
  assert.doesNotMatch(bundle, /Bearer abcdef123456/);
});

test("latest error view model finds model failures from status or live events", () => {
  const fromStatus = buildLatestErrorViewModel({
    run: {
      executor_last_error: "The model `gpt-5.5` does not exist or you do not have access to it.",
    },
  }, []);
  const fromEvent = buildLatestErrorViewModel({}, [
    { event: "fault_recorded", payload: { message: "transport failed" } },
  ]);

  assert.equal(fromStatus.present, true);
  assert.equal(fromStatus.modelRelated, true);
  assert.match(fromStatus.recommendedAction, /Test model health/);
  assert.equal(fromEvent.present, true);
  assert.match(fromEvent.summary, /transport failed/);
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
  assert.equal(vm.blocker, "The planner needs your answer before it can continue.");
  assert.equal(vm.verbosity, "verbose");
  assert.equal(vm.lastRefreshedAt, "2026-04-23T11:00:00Z");
});

test("repo binding mismatch blocks wrong-repo run state and recommends backend restart", () => {
  const snapshot = {
    runtime: {
      repo_root: "D:/Projects/agentic_loop",
      repo_ready: true,
      verbosity: "normal",
    },
    backend: {
      repo_root: "D:/Projects/agentic_loop",
    },
    run: {
      id: "run_wrong_repo",
      goal: "collect context bounded test",
      status: "cancelled",
      completed: false,
      resumable: false,
    },
    approval: {
      state: "none",
      kind: "Unavailable",
      worker_approval_required: 0,
    },
  };
  const options = {
    connection: { connected: true, status: "connected" },
    expectedRepoPath: "D:/Projects/brick-breaker-android",
    goalEntered: true,
  };

  const binding = buildRepoBindingViewModel(snapshot, options);
  const top = buildTopStatusViewModel(snapshot, options);
  const home = buildHomeDashboardViewModel(snapshot, {
    ...options,
    artifacts: { items: [] },
    contractFiles: { files: [] },
    events: [],
  });
  const recommendation = buildRecommendedActionViewModel(snapshot, options);
  const controls = buildRunControlStateViewModel(snapshot, options);
  const approval = buildApprovalViewModel(snapshot);

  assert.equal(binding.mismatch, true);
  assert.equal(binding.matches, false);
  assert.match(binding.message, /Wrong Repo Backend/);
  assert.equal(top.repoMismatch, true);
  assert.match(top.blocker, /expected D:\/Projects\/brick-breaker-android/);
  assert.equal(top.runID, "Hidden until repo matches");
  assert.equal(home.status.runID, "Hidden until repo matches");
  assert.match(home.status.goal, /Wrong repo backend/);
  assert.equal(recommendation.state, "repo_mismatch");
  assert.equal(recommendation.primaryAction.id, "recover_backend");
  assert.equal(recommendation.primaryAction.label, "Restart Backend for Target Repo");
  assert.equal(controls.startEnabled, false);
  assert.equal(controls.continueEnabled, false);
  assert.match(controls.startDisabledReason, /Wrong Repo Backend/);
  assert.equal(approval.needsAttention, false);
  assert.equal(approval.badgeCount, 0);
});

test("correct repo with cancelled non-resumable run allows starting fresh but not continue", () => {
  const snapshot = {
    runtime: {
      repo_root: "D:/Projects/brick-breaker-android",
      repo_ready: true,
    },
    run: {
      id: "run_cancelled",
      status: "cancelled",
      completed: false,
      resumable: false,
      stop_reason: "operator_stop",
    },
  };
  const options = {
    connection: { connected: true, status: "connected" },
    expectedRepoPath: "D:/Projects/brick-breaker-android",
    goalEntered: true,
  };

  const binding = buildRepoBindingViewModel(snapshot, options);
  const recommendation = buildRecommendedActionViewModel(snapshot, options);
  const controls = buildRunControlStateViewModel(snapshot, options);

  assert.equal(binding.mismatch, false);
  assert.equal(recommendation.state, "fresh_run_available");
  assert.equal(recommendation.primaryAction.id, "start_run");
  assert.equal(controls.startEnabled, true);
  assert.equal(controls.continueEnabled, false);
  assert.match(controls.continueDisabledReason, /not marked resumable/);
});

test("correct repo with resumable run enables continue and blocks duplicate start", () => {
  const snapshot = {
    runtime: {
      repo_root: "D:/Projects/brick-breaker-android",
    },
    run: {
      id: "run_resumable",
      status: "initialized",
      completed: false,
      resumable: true,
      next_operator_action: "continue_existing_run",
    },
  };
  const options = {
    connection: { connected: true, status: "connected" },
    expectedRepoPath: "D:/Projects/brick-breaker-android",
    goalEntered: true,
  };

  const recommendation = buildRecommendedActionViewModel(snapshot, options);
  const controls = buildRunControlStateViewModel(snapshot, options);

  assert.equal(recommendation.state, "resumable");
  assert.equal(recommendation.primaryAction.id, "continue_run");
  assert.equal(controls.continueEnabled, true);
  assert.equal(controls.startEnabled, false);
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
      actively_processing: true,
      completed: false,
      latest_checkpoint: { sequence: 4, stage: "executor_turn" },
    },
  }, {
    latestEvent: { event: "executor_turn_started", at: "2026-04-23T11:01:00Z" },
  });
  assert.equal(loop.label, "Loop Status: Running");
  assert.equal(loop.stage, "executor_turn");
});

test("execute outcome at planner safe pause is ready to continue, not running", () => {
  const snapshot = {
    active_run_guard: {
      present: true,
      current_backend: true,
      stale: false,
      currently_processing: false,
      waiting_at_safe_point: true,
      run_id: "run_execute",
    },
    run: {
      id: "run_execute",
      status: "initialized",
      completed: false,
      resumable: true,
      latest_planner_outcome: "execute",
      next_operator_action: "continue_existing_run",
      latest_checkpoint: {
        sequence: 3,
        stage: "planner",
        label: "planner_turn_completed",
        safe_pause: true,
      },
      executor_turn_status: "",
      executor_thread_id: "",
      executor_turn_id: "",
    },
  };

  const loop = buildLoopStatusViewModel(snapshot);
  const recommended = buildRecommendedActionViewModel(snapshot, {
    connection: { connected: true, status: "connected" },
  });
  const controls = buildRunControlStateViewModel(snapshot, {
    connection: { connected: true },
    goalEntered: true,
  });

  assert.equal(loop.state, "ready_to_continue");
  assert.equal(loop.label, "Loop Status: Ready to Continue");
  assert.match(loop.detail, /Executor has not started/);
  assert.equal(recommended.state, "ready_to_continue");
  assert.equal(recommended.primaryAction.id, "continue_run");
  assert.equal(recommended.primaryAction.label, "Continue Build / Dispatch Executor");
  assert.equal(controls.continueEnabled, true);
  assert.equal(controls.continueLabel, "Continue Build / Dispatch Executor");
  assert.equal(controls.startEnabled, false);
  assert.match(controls.note, /Continue Build \/ Dispatch Executor/);
});

test("active-run guard presence alone does not mean the backend is working", () => {
  const snapshot = {
    active_run_guard: {
      present: true,
      current_backend: true,
      stale: false,
      currently_processing: false,
      waiting_at_safe_point: true,
      run_id: "run_guard",
    },
    run: {
      id: "run_guard",
      status: "active",
      completed: false,
      resumable: true,
      latest_planner_outcome: "collect_context",
      next_operator_action: "continue_existing_run",
      latest_checkpoint: {
        sequence: 4,
        stage: "planner",
        label: "planner_turn_completed",
        safe_pause: true,
      },
    },
  };

  const loop = buildLoopStatusViewModel(snapshot);

  assert.equal(loop.state, "waiting_at_safe_point");
  assert.notEqual(loop.label, "Loop Status: Running");
});

test("currently-processing active-run guard shows Running", () => {
  const loop = buildLoopStatusViewModel({
    active_run_guard: {
      present: true,
      current_backend: true,
      stale: false,
      currently_processing: true,
      run_id: "run_live",
    },
    run: {
      id: "run_live",
      status: "active",
      completed: false,
      resumable: false,
      latest_checkpoint: { sequence: 5, stage: "executor_turn", safe_pause: false },
    },
  });

  assert.equal(loop.state, "running");
  assert.equal(loop.label, "Loop Status: Running");
});

test("debug bundle renders normalized loop state and checkpoint JSON", () => {
  const bundle = buildDebugBundleText({
    active_run_guard: {
      present: true,
      current_backend: true,
      stale: false,
      currently_processing: false,
      waiting_at_safe_point: true,
      last_progress_at: "2026-04-24T12:00:00Z",
    },
    run: {
      id: "run_execute",
      goal: "Build the next slice",
      status: "initialized",
      completed: false,
      resumable: true,
      latest_planner_outcome: "execute",
      next_operator_action: "continue_existing_run",
      latest_checkpoint: {
        sequence: 3,
        stage: "planner",
        label: "planner_turn_completed",
        safe_pause: true,
        planner_turn: 2,
        executor_turn: 0,
      },
    },
  }, null, [], { now: "2026-04-24T12:01:00Z" });

  assert.match(bundle, /Normalized loop state: ready_to_continue/);
  assert.match(bundle, /Actively processing: false/);
  assert.match(bundle, /Waiting at safe point: true/);
  assert.match(bundle, /Execute ready: true/);
  assert.match(bundle, /Continue enabled: true/);
  assert.match(bundle, /"stage":"planner"/);
  assert.doesNotMatch(bundle, /\[object Object\]/);
});

test("debug bundle includes expected and actual repo binding details", () => {
  const bundle = buildDebugBundleText({
    runtime: {
      repo_root: "D:/Projects/agentic_loop",
      version: "dev",
    },
    run: {
      id: "run_wrong_repo",
      status: "cancelled",
      completed: false,
      resumable: false,
    },
  }, null, [], {
    now: "2026-04-24T12:20:00Z",
    expectedRepoPath: "D:/Projects/brick-breaker-android",
  });

  assert.match(bundle, /Expected repo path: D:\/Projects\/brick-breaker-android/);
  assert.match(bundle, /Actual backend repo root: D:\/Projects\/agentic_loop/);
  assert.match(bundle, /Repo match: no/);
  assert.match(bundle, /GUI is connected to a backend serving the wrong repo/);
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
  assert.equal(vm.codex.fullAccessReady, "Not verified");
  assert.match(vm.codex.recommendedAction, /Test Codex Config|Codex/);
  assert.equal(vm.liveOutput.primaryAction.id, "open_live_output");
  assert.match(vm.liveOutput.detail, /Verbose and Trace/);
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
  assert.equal(missing.fullAccessReady, "Not verified");
});

test("codex readiness shows verified gpt-5.5 full access details", () => {
  const ready = buildCodexReadinessViewModel({
    runtime: { executor_ready: true },
    model_health: {
      executor: {
        requested_model: "gpt-5.5",
        verification_state: "verified",
        access_mode: "danger-full-access sandbox, approval never",
        effort: "xhigh",
        codex_executable_path: "C:/Users/me/AppData/Roaming/npm/codex.cmd",
        codex_version: "codex-cli 0.124.0",
        codex_config_source: "C:/Users/me/.codex/config.toml",
        codex_model_verified: true,
        codex_permission_mode_verified: true,
      },
    },
  });

  assert.equal(ready.title, "Codex Full Access: Verified");
  assert.equal(ready.fullAccessReady, "Yes");
  assert.equal(ready.needsAttention, false);
  assert.match(ready.codexVersion, /0\.124\.0/);
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
  assert.match(script, /ORCHESTRATOR_V2_BACKEND_META/);
  assert.match(script, /ORCHESTRATOR_V2_EXPECTED_REPO/);
  assert.match(script, /Wait-DogfoodBackendRepoMatch/);
  assert.match(script, /expected_repo=\$resolvedRepoPath/);
  assert.match(script, /dogfood-backend\.json/);
  assert.match(script, /orchestrator-v2-dogfood/);
  assert.match(script, /owner_session_id/);
  assert.match(script, /binary_mtime_at_launch/);
  assert.match(script, /Stop-StaleOwnedBackendIfPresent/);
  assert.match(script, /Warn-IfUnknownProcessOwnsPort/);
  assert.match(script, /Wait-PortClear/);
  assert.match(script, /Clear-OwnedDogfoodPort/);
  assert.match(script, /Format-DogfoodPortDiagnostic/);
  assert.match(script, /taskkill \/PID \$ProcessId \/T \/F/);
  assert.match(script, /It was not killed automatically/);
  assert.match(script, /http:\/\/\$ControlAddr/);
  assert.match(script, /\[switch\]\$SkipBuild/);
  assert.match(script, /\[switch\]\$SkipInstall/);
  assert.match(script, /\[switch\]\$DebugVisibleWindows/);
  assert.match(script, /-WindowStyle Hidden/);
  assert.match(script, /Start-Process -FilePath \$binaryPath/);
  assert.match(script, /-WorkingDirectory \$resolvedRepoPath/);
  assert.match(script, /Wait-Process -Id \$shellProcess\.Id/);
  assert.match(script, /Invoke-DogfoodTaskKill -ProcessId \$controlProcess\.Id/);
  assert.match(script, /Invoke-RestMethod/);
  assert.match(launcher, /WindowStyle Hidden/);
  assert.match(launcher, /start-v2-dogfood\.ps1/);
});

test("backend ownership helpers avoid killing unknown processes", () => {
  assert.equal(isOwnedBackend({ owner: ownerMarker, pid: 1234 }), true);
  assert.equal(isOwnedBackend({ owner: "someone-else", pid: 1234 }), false);
  assert.equal(metadataMatchesAddress({ control_addr: "127.0.0.1:44777" }, "http://127.0.0.1:44777"), true);
  assert.equal(metadataMatchesAddress({ control_addr: "127.0.0.1:44777" }, "http://127.0.0.1:44778"), false);
  assert.equal(listenerMatchesOwnedMetadata({
    pid: 111,
    binary_path: "D:/Projects/agentic_loop/dist/orchestrator.exe",
    control_addr: "127.0.0.1:44777",
  }, {
    pid: 222,
    parent_pid: 0,
    path: "D:\\Projects\\agentic_loop\\dist\\orchestrator.exe",
    command_line: '"D:\\Projects\\agentic_loop\\dist\\orchestrator.exe" control serve --addr 127.0.0.1:44777',
  }), true);
  assert.equal(listenerMatchesOwnedMetadata({
    pid: 111,
    binary_path: "D:/Projects/agentic_loop/dist/orchestrator.exe",
    control_addr: "127.0.0.1:44777",
  }, {
    pid: 333,
    parent_pid: 0,
    path: "C:\\Windows\\System32\\WindowsPowerShell\\v1.0\\powershell.exe",
    command_line: "powershell -NoExit",
  }), false);
  assert.equal(fileModifiedTime(path.join(__dirname, "does-not-exist.exe")), "");
});

test("backend metadata loader tolerates PowerShell UTF-8 BOM files", () => {
  const tmpDir = fs.mkdtempSync(path.join(require("node:os").tmpdir(), "orchestrator-backend-bom-"));
  const metaPath = path.join(tmpDir, "dogfood-backend.json");
  fs.writeFileSync(metaPath, `\uFEFF${JSON.stringify({ owner: ownerMarker, pid: 1234 })}`, "utf8");
  const metadata = loadBackendMetadata(metaPath);

  assert.equal(metadata.owner, ownerMarker);
  assert.equal(metadata.pid, 1234);
});

test("backend recovery clears owned port listeners with taskkill fallback", async () => {
  let queryCount = 0;
  const taskkillPIDs = [];
  const execFileImpl = (command, args, _options, callback) => {
    if (command === "powershell.exe") {
      queryCount += 1;
      const stdout = queryCount === 1 ? JSON.stringify({
        pid: 2222,
        path: "D:\\Projects\\agentic_loop\\dist\\orchestrator.exe",
        command_line: '"D:\\Projects\\agentic_loop\\dist\\orchestrator.exe" control serve --addr 127.0.0.1:44777',
        parent_pid: 1111,
      }) : "";
      callback(null, stdout, "");
      return;
    }
    if (command === "taskkill") {
      taskkillPIDs.push(args[1]);
      callback(null, "SUCCESS", "");
      return;
    }
    callback(new Error(`unexpected command ${command}`), "", "");
  };

  const result = await clearOwnedBackendPort({
    pid: 1111,
    binary_path: "D:/Projects/agentic_loop/dist/orchestrator.exe",
    control_addr: "127.0.0.1:44777",
  }, {
    execFileImpl,
    timeoutMs: 500,
    pollMs: 1,
  });

  assert.equal(result.cleared, true);
  assert.deepEqual(taskkillPIDs, ["2222"]);
});

test("backend recovery refuses to kill unknown port owners and reports diagnostics", async () => {
  const taskkillPIDs = [];
  const execFileImpl = (command, args, _options, callback) => {
    if (command === "powershell.exe") {
      callback(null, JSON.stringify({
        pid: 3333,
        path: "C:\\Tools\\other.exe",
        command_line: "other.exe --listen 44777",
        parent_pid: 0,
      }), "");
      return;
    }
    if (command === "taskkill") {
      taskkillPIDs.push(args[1]);
      callback(null, "SUCCESS", "");
      return;
    }
    callback(new Error(`unexpected command ${command}`), "", "");
  };

  const result = await clearOwnedBackendPort({
    pid: 1111,
    binary_path: "D:/Projects/agentic_loop/dist/orchestrator.exe",
    control_addr: "127.0.0.1:44777",
  }, {
    execFileImpl,
    timeoutMs: 50,
    pollMs: 1,
  });

  assert.equal(result.cleared, false);
  assert.equal(taskkillPIDs.length, 0);
  assert.match(result.message, /current port holders/);
  assert.match(result.message, /matches_owned_metadata: false/);
});

test("restartOwnedBackend does not spawn when the port remains held by an unknown process", async () => {
  const tmpDir = fs.mkdtempSync(path.join(require("node:os").tmpdir(), "orchestrator-backend-"));
  const metaPath = path.join(tmpDir, "dogfood-backend.json");
  fs.writeFileSync(metaPath, JSON.stringify({
    owner: ownerMarker,
    pid: 1111,
    repo_path: "D:/Projects/brick-breaker-android",
    control_addr: "127.0.0.1:44777",
    binary_path: "D:/Projects/agentic_loop/dist/orchestrator.exe",
  }), "utf8");

  let spawnCalled = false;
  const execFileImpl = (command, _args, _options, callback) => {
    if (command === "taskkill") {
      callback(null, "SUCCESS", "");
      return;
    }
    if (command === "powershell.exe") {
      callback(null, JSON.stringify({
        pid: 4444,
        path: "C:\\Tools\\other.exe",
        command_line: "other.exe --listen 44777",
        parent_pid: 0,
      }), "");
      return;
    }
    callback(new Error(`unexpected command ${command}`), "", "");
  };

  const result = await restartOwnedBackend({
    metaPath,
    address: "http://127.0.0.1:44777",
    execFileImpl,
    spawnImpl: () => {
      spawnCalled = true;
      throw new Error("spawn should not be called while port is blocked");
    },
    portClearTimeoutMs: 50,
    portClearPollMs: 1,
  });

  assert.equal(result.available, true);
  assert.equal(result.restarted, false);
  assert.equal(result.blocked, true);
  assert.equal(spawnCalled, false);
  assert.match(result.message, /did not clear/);
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
  assert.equal(vm.canApprove, true);
  assert.equal(vm.canDeny, true);
  assert.equal(vm.workerApprovalRequired, 1);
});

test("worker approval count alone creates a badge without enabling primary executor buttons", () => {
  const vm = buildApprovalViewModel({
    approval: {
      present: false,
      state: "none",
      kind: "Unavailable",
    },
    workers: {
      approval_required: 2,
    },
  });

  assert.equal(vm.present, false);
  assert.equal(vm.needsAttention, true);
  assert.equal(vm.badgeCount, 2);
  assert.equal(vm.canApprove, false);
  assert.equal(vm.canDeny, false);
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
  assert.equal(vm.approval, "No approval needed.");
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
        backend_state: "context_agent",
        response_message: "Latest run: run_22. Side Chat did not alter the active run.",
        created_at: "2026-04-22T16:00:00Z",
        run_id: "run_22",
        context_policy: "repo_and_latest_run_summary",
      },
    ],
  });

  assert.equal(vm.count, 1);
  assert.equal(vm.nonInterfering, true);
  assert.match(vm.modeDescription, /explicit audited action/);
  assert.equal(vm.buttonLabel, "Ask Side Chat");
  assert.equal(vm.items[0].rawText, "What remains before release?");
  assert.equal(vm.items[0].backendState, "context_agent");
  assert.equal(vm.items[0].runID, "run_22");
});

test("renderer recovery path uses the defined side-chat refresh helper", () => {
  const source = fs.readFileSync(path.join(__dirname, "../src/renderer/app.js"), "utf8");

  assert.doesNotMatch(source, /refreshSideChatMessages/);
  assert.match(source, /refreshSideChat\(\{ quiet: true \}\)/);
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

  assert.equal(formatEventSummary({
    event: "planner_turn_completed",
    payload: { run_id: "run_88", planner_outcome: "execute" },
  }), "Planner selected an execute task. Waiting to dispatch Codex run=run_88.");
  assert.equal(formatEventSummary({
    event: "executor_dispatch_requested",
    payload: { run_id: "run_88" },
  }), "Dispatching Codex executor run=run_88.");
});

test("classifyActivityCategory keeps intervention and terminal events distinct", () => {
  assert.equal(classifyActivityCategory({ event: "control_message_queued" }), "intervention");
  assert.equal(classifyActivityCategory({ event: "terminal_session_started" }), "terminal");
  assert.equal(classifyActivityCategory({ event: "worker_dispatch_completed" }), "worker");
});

test("window state cleanup uses captured keys and is idempotent", () => {
  const calls = [];
  const stateByKey = new Map();
  stateByKey.set(42, {
    abortController: {
      abort() {
        calls.push("abort");
      },
    },
    terminal: {
      shutdown() {
        calls.push("shutdown");
      },
    },
  });

  assert.equal(shutdownWindowStateByKey(stateByKey, 42), true);
  assert.equal(shutdownWindowStateByKey(stateByKey, 42), false);
  assert.deepEqual(calls, ["abort", "shutdown"]);
  assert.equal(stateByKey.size, 0);

  stateByKey.set(7, {
    abortController: {
      abort() {
        calls.push("abort-7");
      },
    },
    terminal: {
      shutdown() {
        calls.push("shutdown-7");
      },
    },
  });
  assert.equal(shutdownAllWindowStates(stateByKey), 1);
  assert.equal(shutdownAllWindowStates(stateByKey), 0);
  assert.equal(stateByKey.size, 0);
});
