(function registerViewModel(root, factory) {
  const api = factory();
  if (typeof module === "object" && module.exports) {
    module.exports = api;
  }
  if (root) {
    root.OrchestratorViewModel = api;
  }
})(typeof window !== "undefined" ? window : globalThis, function viewModelFactory() {
  function safeString(value, fallback = "Unavailable") {
    const text = String(value || "").trim();
    return text === "" ? fallback : text;
  }

  function truncateText(value, limit = 220) {
    const text = safeString(value, "");
    if (text.length <= limit) {
      return text;
    }
    return `${text.slice(0, Math.max(0, limit - 1)).trimEnd()}...`;
  }

  function redactDiagnosticText(value) {
    return String(value || "")
      .replace(/sk-[A-Za-z0-9_-]{8,}/g, "[REDACTED_API_KEY]")
      .replace(/\b(api[_-]?key|auth[_-]?token|access[_-]?token|refresh[_-]?token|password|secret)\b\s*[:=]\s*[^\s\n]+/gi, "$1: [REDACTED]")
      .replace(/\b(Bearer)\s+[A-Za-z0-9._~+/-]+=*/gi, "$1 [REDACTED]");
  }

  function lower(value) {
    return safeString(value, "").toLowerCase();
  }

  function normalizeRepoPath(value) {
    const text = safeString(value, "");
    if (text === "") {
      return "";
    }
    return text.replace(/\//g, "\\").replace(/\\+$/, "").toLowerCase();
  }

  function runtimeSnapshot(snapshot) {
    return snapshot && snapshot.runtime ? snapshot.runtime : {};
  }

  function repoContractReadinessViewModel(snapshot) {
    const runtime = runtimeSnapshot(snapshot);
    const hasRepoReady = Object.prototype.hasOwnProperty.call(runtime, "repo_ready");
    const missing = Array.isArray(runtime.repo_contract_missing)
      ? runtime.repo_contract_missing.map((item) => safeString(item, "")).filter(Boolean)
      : [];
    const ready = hasRepoReady ? Boolean(runtime.repo_ready) : true;
    const missingText = missing.length > 0 ? ` Missing: ${missing.join(", ")}.` : "";
    return {
      known: hasRepoReady,
      ready,
      missing,
      message: ready
        ? "Repo contract markers look ready from the latest status snapshot."
        : `Target repo contract is not ready.${missingText} Run \`orchestrator init\` from the target repo, then refresh the dashboard.`,
    };
  }

  function runSnapshot(snapshot) {
    return snapshot && snapshot.run ? snapshot.run : null;
  }

  function approvalSnapshot(snapshot) {
    return snapshot && snapshot.approval ? snapshot.approval : {};
  }

  function plannerStatusSnapshot(snapshot) {
    return snapshot && snapshot.planner_status ? snapshot.planner_status : {};
  }

  function pendingActionSnapshot(snapshot) {
    return snapshot && snapshot.pending_action ? snapshot.pending_action : {};
  }

  function askHumanSnapshot(snapshot) {
    return snapshot && snapshot.ask_human ? snapshot.ask_human : {};
  }

  function activeRunGuardSnapshot(snapshot) {
    return snapshot && snapshot.active_run_guard ? snapshot.active_run_guard : {};
  }

  function stopFlagSnapshot(snapshot) {
    return snapshot && snapshot.stop_flag ? snapshot.stop_flag : {};
  }

  function repoBindingViewModel(snapshot, options = {}) {
    const runtime = runtimeSnapshot(snapshot);
    const backend = snapshot && snapshot.backend ? snapshot.backend : {};
    const expected = safeString(
      options.expectedRepoPath ||
        (options.connection && options.connection.expectedRepoPath) ||
        (options.connection && options.connection.expected_repo_path),
      "",
    );
    const actual = safeString(runtime.repo_root || backend.repo_root, "");
    const hasExpected = expected !== "";
    const hasActual = actual !== "";
    const matches = !hasExpected || !hasActual || normalizeRepoPath(expected) === normalizeRepoPath(actual);
    const mismatch = hasExpected && hasActual && !matches;
    return {
      expected,
      actual,
      hasExpected,
      hasActual,
      matches,
      mismatch,
      message: mismatch
        ? `Wrong Repo Backend: expected ${expected}, but the connected backend is serving ${actual}. Restart Backend for Target Repo before starting or continuing work.`
        : (hasExpected
          ? `Backend repo matches the expected target: ${expected}.`
          : "No expected repo binding was provided by the launcher."),
    };
  }

  function hasRepoMismatch(snapshot, options = {}) {
    return repoBindingViewModel(snapshot, options).mismatch;
  }

  function hasStaleActiveRunGuard(snapshot) {
    const guard = activeRunGuardSnapshot(snapshot);
    return Boolean(guard.present && guard.stale);
  }

  function safeStopRequested(snapshot) {
    const run = runSnapshot(snapshot);
    const stopReason = lower(run && run.stop_reason);
    const flag = stopFlagSnapshot(snapshot);
    return Boolean(flag.present)
      || stopReason === "operator_stop_requested";
  }

  function buildSafeStopViewModel(snapshot) {
    const run = runSnapshot(snapshot);
    const flag = stopFlagSnapshot(snapshot);
    const present = safeStopRequested(snapshot);
    const reason = safeString(flag.reason || (run && run.stop_reason), present ? "operator_stop_requested" : "Unavailable");
    return {
      present,
      flagPresent: Boolean(flag.present),
      path: safeString(flag.path, "Unavailable"),
      appliesAt: safeString(flag.applies_at || flag.appliesAt, "next_safe_point"),
      reason,
      runID: safeString(run && run.id, "Unavailable"),
      message: present
        ? "Safe stop was requested. Clear the stop flag before continuing this run."
        : "No safe stop flag is currently active.",
    };
  }

  function runStateLabel(run) {
    if (!run) {
      return "No active run";
    }
    if (run.completed) {
      return "completed";
    }
    return safeString(run.status || run.stop_reason || run.next_operator_action, "unfinished");
  }

  function formatDuration(seconds) {
    const totalSeconds = Math.max(0, Math.floor(Number(seconds) || 0));
    const hours = Math.floor(totalSeconds / 3600);
    const minutes = Math.floor((totalSeconds % 3600) / 60);
    const secs = totalSeconds % 60;
    if (hours > 0) {
      return `${hours}:${String(minutes).padStart(2, "0")}:${String(secs).padStart(2, "0")}`;
    }
    return `${String(minutes).padStart(2, "0")}:${String(secs).padStart(2, "0")}`;
  }

  const stopReasonCopy = {
    planner_complete: {
      title: "The planner declared the run complete.",
      nextAction: "Review the results and latest artifacts, or start a new build when you are ready.",
      severity: "success",
    },
    planner_ask_human: {
      title: "The planner needs information from you.",
      nextAction: "Answer in Control Chat so the raw message reaches the planner at the next safe point.",
      severity: "attention",
    },
    executor_approval_required: {
      title: "Codex needs approval before continuing.",
      nextAction: "Open Action Required, review the request, then approve or deny it.",
      severity: "attention",
    },
    missing_required_config: {
      title: "Required configuration is missing.",
      nextAction: "Open Settings or run doctor from the headless CLI to fix missing configuration.",
      severity: "danger",
    },
    planner_validation_failed: {
      title: "The planner returned output that did not match the required contract.",
      nextAction: "Review the latest planner artifact and continue after the contract issue is fixed.",
      severity: "danger",
    },
    executor_failed: {
      title: "Codex/executor failed before completing the requested work.",
      nextAction: "Open Live Output and Settings. If the failure is a model error, change or test the configured Codex model before continuing.",
      severity: "danger",
    },
    transport_or_process_error: {
      title: "A network, API, or process error stopped the run.",
      nextAction: "Check the activity timeline and logs, then reconnect or continue when the service is healthy.",
      severity: "danger",
    },
    max_cycles_reached: {
      title: "The run reached its configured cycle limit.",
      nextAction: "Continue the existing run if more progress is expected.",
      severity: "warning",
    },
    operator_stop: {
      title: "You requested a safe stop.",
      nextAction: "Continue the run when you are ready, or review the latest outputs first.",
      severity: "neutral",
    },
    operator_stop_requested: {
      title: "You requested a safe stop.",
      nextAction: "Clear the safe stop, then continue the run when you are ready.",
      severity: "neutral",
    },
    cycle_boundary: {
      title: "The run stopped at a safe cycle boundary.",
      nextAction: "Continue the existing run if you want unattended progress to keep going.",
      severity: "neutral",
    },
  };

  function translateStopReason(code) {
    const normalized = safeString(code, "").toLowerCase();
    if (normalized === "") {
      return {
        code: "none",
        title: "No stop reason is recorded.",
        detail: "The latest status snapshot did not include a stop code.",
        nextAction: "Update the dashboard or watch the live activity timeline.",
        severity: "neutral",
      };
    }

    const direct = stopReasonCopy[normalized];
    if (direct) {
      return {
        code: normalized,
        title: direct.title,
        detail: `Technical stop code: ${normalized}`,
        nextAction: direct.nextAction,
        severity: direct.severity,
      };
    }

    if (normalized.includes("approval")) {
      return translateStopReason("executor_approval_required");
    }
    if (normalized.includes("ask_human") || normalized.includes("human")) {
      return translateStopReason("planner_ask_human");
    }
    if (normalized.includes("validation")) {
      return translateStopReason("planner_validation_failed");
    }
    if (normalized.includes("error") || normalized.includes("transport") || normalized.includes("process")) {
      return translateStopReason("transport_or_process_error");
    }
    if (normalized.includes("complete")) {
      return translateStopReason("planner_complete");
    }

    return {
      code: normalized,
      title: "The run stopped.",
      detail: `Technical stop code: ${normalized}`,
      nextAction: "Review the latest activity and continue only if the run is resumable.",
      severity: "neutral",
    };
  }

  function latestArtifactPath(snapshot, artifactListing) {
    const artifacts = snapshot && snapshot.artifacts ? snapshot.artifacts : {};
    const listing = artifactListing || {};
    const items = Array.isArray(listing.items) ? listing.items : [];
    const latest = items.find((item) => item.latest) || items[0] || {};
    return safeString(listing.latest_path || artifacts.latest_path || latest.path, "");
  }

  function positiveCount(value) {
    const count = Number(value);
    return Number.isFinite(count) && count > 0 ? Math.trunc(count) : 0;
  }

  function actionableApprovalState(state) {
    const normalized = lower(state);
    return normalized.includes("required")
      || normalized.includes("pending")
      || normalized.includes("requested")
      || normalized.includes("waiting");
  }

  function unavailableApprovalKind(kind) {
    const normalized = lower(kind);
    return normalized === "" || normalized === "none" || normalized === "unavailable" || normalized === "null";
  }

  function workerApprovalCount(snapshot) {
    const approval = approvalSnapshot(snapshot);
    const workers = snapshot && snapshot.workers ? snapshot.workers : {};
    return positiveCount(approval.worker_approval_required || workers.approval_required);
  }

  function hasPrimaryApprovalRequired(snapshot) {
    const approval = approvalSnapshot(snapshot);
    return actionableApprovalState(approval.state) && !unavailableApprovalKind(approval.kind);
  }

  function hasApprovalRequired(snapshot) {
    return hasPrimaryApprovalRequired(snapshot) || workerApprovalCount(snapshot) > 0;
  }

  function hasAskHumanPending(snapshot) {
    const run = runSnapshot(snapshot);
    const pending = pendingActionSnapshot(snapshot);
    const askHuman = askHumanSnapshot(snapshot);
    const stopReason = lower(run && run.stop_reason);
    const nextAction = lower(run && run.next_operator_action);
    const plannerOutcome = lower((run && run.latest_planner_outcome) || pending.planner_outcome);
    const turnType = lower(pending.turn_type || askHuman.turn_type);
    const summary = lower(pending.pending_action_summary || pending.action_summary || askHuman.action_summary || askHuman.question);
    if (askHuman.present === true) {
      return true;
    }
    return stopReason.includes("ask_human")
      || nextAction.includes("ask_human")
      || nextAction.includes("answer")
      || plannerOutcome.includes("ask_human")
      || turnType.includes("ask_human")
      || summary.includes("ask_human")
      || summary.includes("human answer")
      || summary.includes("human input");
  }

  function buildAskHumanViewModel(snapshot) {
    const askHuman = askHumanSnapshot(snapshot);
    const pending = pendingActionSnapshot(snapshot);
    const plannerStatus = plannerStatusSnapshot(snapshot);
    const run = runSnapshot(snapshot);
    const present = hasAskHumanPending(snapshot);
    const question = safeString(
      askHuman.question ||
        askHuman.blocker ||
        pending.pending_action_summary ||
        pending.pending_reason ||
        plannerStatus.operator_message ||
        plannerStatus.current_focus,
      present ? "The planner needs your answer before it can continue." : "No planner question is waiting.",
    );
    const blocker = safeString(
      askHuman.blocker ||
        askHuman.action_summary ||
        plannerStatus.operator_message ||
        plannerStatus.current_focus ||
        pending.pending_action_summary,
      present ? question : "No ask_human blocker is active.",
    );

    return {
      present,
      title: present ? "Planner needs your answer" : "No planner answer needed",
      question,
      blocker,
      actionSummary: safeString(askHuman.action_summary || pending.pending_action_summary, question),
      runID: safeString(askHuman.run_id || (run && run.id), "Unavailable"),
      source: safeString(askHuman.source || pending.pending_reason || "status", "status"),
      plannerOutcome: safeString(askHuman.planner_outcome || pending.planner_outcome || (run && run.latest_planner_outcome), present ? "ask_human" : "Unavailable"),
      responseID: safeString(askHuman.response_id || pending.planner_response_id, "Unavailable"),
      updatedAt: safeString(askHuman.updated_at || pending.updated_at, "Unavailable"),
      message: present
        ? "Type a raw answer below. The shell will queue it through inject_control_message, then Continue Build can resume the run through continue_run."
        : "No planner question is currently waiting for a human answer.",
    };
  }

  function latestCheckpoint(run) {
    return run && run.latest_checkpoint && typeof run.latest_checkpoint === "object"
      ? run.latest_checkpoint
      : {};
  }

  function executorTurnActive(run) {
    const status = lower(
      (run && (run.executor_turn_status || run.executor_status || run.latest_executor_status)),
    );
    return status === "active"
      || status === "running"
      || status === "in_progress"
      || status === "started"
      || status === "executor_active";
  }

  function executeReadyToDispatch(snapshot) {
    const run = runSnapshot(snapshot);
    if (!run || run.completed) {
      return false;
    }
    if (run.execute_ready === true) {
      return true;
    }
    const checkpoint = latestCheckpoint(run);
    const plannerOutcome = lower(run.latest_planner_outcome || run.planner_outcome || pendingActionSnapshot(snapshot).planner_outcome);
    const nextAction = lower(run.next_operator_action);
    return checkpoint.safe_pause === true
      && lower(checkpoint.stage) === "planner"
      && plannerOutcome === "execute"
      && nextAction === "continue_existing_run"
      && !executorTurnActive(run);
  }

  function waitingAtSafePoint(snapshot) {
    const run = runSnapshot(snapshot);
    if (!run || run.completed || hasAskHumanPending(snapshot) || hasApprovalRequired(snapshot) || executorModelInvalid(snapshot)) {
      return false;
    }
    if (run.waiting_at_safe_point === true) {
      return true;
    }
    const guard = activeRunGuardSnapshot(snapshot);
    if (guard.waiting_at_safe_point === true) {
      return true;
    }
    const checkpoint = latestCheckpoint(run);
    const nextAction = lower(run.next_operator_action);
    return checkpoint.safe_pause === true
      && nextAction === "continue_existing_run"
      && !executorTurnActive(run);
  }

  function activelyProcessing(snapshot) {
    const run = runSnapshot(snapshot);
    const guard = activeRunGuardSnapshot(snapshot);
    if (run && run.actively_processing === true) {
      return true;
    }
    if (guard.currently_processing === true) {
      return true;
    }
    if (!run || run.completed || waitingAtSafePoint(snapshot) || executeReadyToDispatch(snapshot)) {
      return false;
    }
    const stopReason = lower(run.stop_reason);
    return executorTurnActive(run)
      || stopReason.includes("executor_in_progress")
      || stopReason.includes("planner_in_progress");
  }

  function hasActiveRunInProgress(snapshot) {
    return activelyProcessing(snapshot);
  }

  function modelHealthSnapshot(snapshot) {
    return snapshot && snapshot.model_health ? snapshot.model_health : {};
  }

  function cloneJSON(value) {
    if (!value || typeof value !== "object") {
      return value;
    }
    return JSON.parse(JSON.stringify(value));
  }

  function parseTimeMillis(value) {
    const date = new Date(value || "");
    return Number.isNaN(date.getTime()) ? 0 : date.getTime();
  }

  function componentFromTest(testResult, componentName) {
    if (!testResult || typeof testResult !== "object") {
      return null;
    }
    const direct = testResult[componentName];
    if (direct && typeof direct === "object") {
      return direct;
    }
    return testResult.component === componentName ? testResult : null;
  }

  function componentsCompatible(fresh, current) {
    if (!fresh || typeof fresh !== "object") {
      return false;
    }
    if (!current || typeof current !== "object") {
      return true;
    }
    const freshComponent = safeString(fresh.component, "");
    const currentComponent = safeString(current.component, "");
    if (freshComponent && currentComponent && freshComponent !== currentComponent) {
      return false;
    }
    const freshConfigured = safeString(fresh.configured_model, "");
    const currentConfigured = safeString(current.configured_model, "");
    return freshConfigured === "" || currentConfigured === "" || freshConfigured === currentConfigured;
  }

  function shouldUseFreshComponent(fresh, current, run) {
    if (!componentsCompatible(fresh, current)) {
      return false;
    }
    const freshAt = parseTimeMillis(fresh.last_tested_at);
    const currentAt = parseTimeMillis(current && current.last_tested_at);
    if (freshAt === 0 && !fresh.test_performed) {
      return false;
    }
    if (runFailureNewerThanComponent(run, fresh)) {
      return false;
    }
    return freshAt >= currentAt || lower(current && current.verification_state) !== "verified";
  }

  function runFailureNewerThanComponent(run, component) {
    if (!run || !component) {
      return false;
    }
    const errorText = safeString(run.executor_last_error || run.last_error, "");
    if (errorText === "") {
      return false;
    }
    const componentAt = parseTimeMillis(component.last_tested_at);
    if (componentAt === 0) {
      return false;
    }
    const runAt = parseTimeMillis(run.stopped_at || run.updated_at);
    return runAt > componentAt;
  }

  function modelAtLeastGPT54(model) {
    const text = lower(model);
    if (text === "gpt-5-latest" || text === "latest" || text === "gpt5-latest") {
      return true;
    }
    const match = text.match(/^gpt-5(?:\.([0-9]+))?(?:-|$)/);
    if (!match) {
      return true;
    }
    const minor = match[1] === undefined ? 0 : Number(match[1]);
    return Number.isFinite(minor) && minor >= 4;
  }

  function plannerComponentVerified(component) {
    if (!component || lower(component.verification_state) !== "verified") {
      return false;
    }
    const model = safeString(component.verified_model || component.resolved_model || component.requested_model || component.configured_model, "");
    return model !== "" && modelAtLeastGPT54(model);
  }

  function executorComponentVerified(component) {
    if (!component || lower(component.verification_state) !== "verified") {
      return false;
    }
    const model = safeString(component.verified_model || component.resolved_model || component.requested_model || component.configured_model || component.codex_model_configured, "");
    const access = lower(component.access_mode);
    const effort = lower(component.effort);
    return model === "gpt-5.5"
      && Boolean(component.codex_model_verified)
      && Boolean(component.codex_permission_mode_verified)
      && access.includes("danger-full-access")
      && access.includes("approval never")
      && (effort === "" || effort === "xhigh");
  }

  function normalizeModelHealthSnapshot(snapshot, modelTests = {}) {
    if (!snapshot || typeof snapshot !== "object") {
      return snapshot;
    }
    const next = cloneJSON(snapshot);
    const health = next.model_health && typeof next.model_health === "object" ? next.model_health : {};
    const run = next.run || {};
    const plannerTest = componentFromTest(modelTests.planner, "planner");
    const executorTest = componentFromTest(modelTests.executor, "executor");

    health.planner = health.planner && typeof health.planner === "object" ? health.planner : {};
    health.executor = health.executor && typeof health.executor === "object" ? health.executor : {};

    if (shouldUseFreshComponent(plannerTest, health.planner, null)) {
      health.planner = { ...health.planner, ...cloneJSON(plannerTest) };
    }
    if (shouldUseFreshComponent(executorTest, health.executor, run)) {
      health.executor = { ...health.executor, ...cloneJSON(executorTest) };
    }
    if (executorComponentVerified(health.executor) && !runFailureNewerThanComponent(run, health.executor)) {
      health.executor.model_unavailable = false;
      health.executor.last_error = "";
      health.executor.codex_last_probe_error = "";
    }

    const plannerVerified = plannerComponentVerified(health.planner);
    const executorVerified = executorComponentVerified(health.executor) && !runFailureNewerThanComponent(run, health.executor);
    const plannerInvalid = lower(health.planner.verification_state) === "invalid";
    const executorInvalid = Boolean(health.executor.model_unavailable)
      || lower(health.executor.verification_state) === "invalid"
      || lower(health.executor.verification_state) === "unavailable"
      || (modelUnavailableFromText(health.executor.last_error) && !executorVerified);

    health.needs_attention = !(plannerVerified && executorVerified);
    health.blocking = plannerInvalid || executorInvalid;
    if (plannerVerified && executorVerified) {
      health.needs_attention = false;
      health.blocking = false;
      health.message = "Planner and Codex requirements verified.";
    } else if (health.blocking) {
      health.message = health.message || "Configured planner/Codex model requirements are not satisfied. Fix and test before continuing.";
    } else {
      health.message = "Model health has not been fully checked yet. Run or wait for the automatic planner and Codex checks.";
    }

    next.model_health = health;
    return next;
  }

  function modelUnavailableFromText(text) {
    const normalized = lower(text);
    return normalized.includes("model")
      && (
        normalized.includes("does not exist")
        || normalized.includes("do not have access")
        || normalized.includes("don't have access")
        || normalized.includes("lack access")
        || normalized.includes("lacks access")
      );
  }

  function executorModelInvalid(snapshot) {
    const modelHealth = modelHealthSnapshot(snapshot);
    const executor = modelHealth.executor || {};
    const run = runSnapshot(snapshot) || {};
    if (executorComponentVerified(executor) && !runFailureNewerThanComponent(run, executor)) {
      return false;
    }
    return Boolean(modelHealth.blocking)
      || Boolean(executor.model_unavailable)
      || lower(executor.verification_state) === "invalid"
      || modelUnavailableFromText(executor.last_error)
      || modelUnavailableFromText(run.executor_last_error);
  }

  function buildLoopStatusViewModel(snapshot, options = {}) {
    const run = runSnapshot(snapshot);
    const latestEvent = options.latestEvent || null;
    const launching = Boolean(options.launching);
    const lastUpdate = safeString(
      options.lastUpdateLabel || (latestEvent && (latestEvent.at || latestEvent.timestampLabel)),
      "No updates yet",
    );

    if (launching) {
      return {
        state: "launching",
        label: "Loop Status: Launching",
        className: "loop-running",
        detail: "A start or continue request was accepted and the run loop is being launched.",
        stage: "launching run",
        turn: "Unavailable",
        lastUpdate,
        latestActivity: safeString(latestEvent && (latestEvent.summary || latestEvent.event), "Launching run..."),
      };
    }

    if (hasStaleActiveRunGuard(snapshot)) {
      const guard = activeRunGuardSnapshot(snapshot);
      return {
        state: "recovery_needed",
        label: "Loop Status: Needs You",
        className: "loop-error",
        detail: safeString(guard.message, "A stale active-run guard from a previous backend is blocking new work."),
        stage: "stale active run",
        turn: safeString(guard.run_id || guard.runID, "Unavailable"),
        lastUpdate,
        latestActivity: "Recover Backend / Unlock Repo can mechanically clear the stale active-run guard without deleting history or artifacts.",
      };
    }

    const repoBinding = repoBindingViewModel(snapshot, options);
    if (repoBinding.mismatch) {
      return {
        state: "repo_mismatch",
        label: "Loop Status: Wrong Repo",
        className: "loop-error",
        detail: "The shell is connected to a backend serving a different repo than the dogfood target.",
        stage: "repo binding mismatch",
        turn: "Unavailable",
        lastUpdate,
        latestActivity: "Restart Backend for Target Repo before starting or continuing a run.",
      };
    }

    if (safeStopRequested(snapshot)) {
      const safeStop = buildSafeStopViewModel(snapshot);
      return {
        state: "safe_stop_requested",
        label: "Loop Status: Stopped",
        className: "loop-stopped",
        detail: "Safe stop was requested. Use Clear Stop and Continue when you are ready to resume.",
        stage: "safe stop requested",
        turn: safeStop.runID,
        lastUpdate,
        latestActivity: safeString(latestEvent && (latestEvent.summary || latestEvent.event), safeStop.message),
      };
    }

    if (!run) {
      return {
        state: "no_run",
        label: "Loop Status: No Run Yet",
        className: "loop-idle",
        detail: "No run was found for this repo yet.",
        stage: "waiting for first run",
        turn: "Unavailable",
        lastUpdate,
        latestActivity: safeString(latestEvent && (latestEvent.summary || latestEvent.event), "No run activity yet."),
      };
    }

    const checkpoint = latestCheckpoint(run);
    const stop = translateStopReason(run.stop_reason);
    const stage = safeString(checkpoint.stage || checkpoint.label || run.latest_planner_outcome, "Unavailable");
    const turn = checkpoint.sequence ? `checkpoint ${checkpoint.sequence}` : safeString(run.latest_planner_outcome, "Unavailable");

    if (hasAskHumanPending(snapshot) || executorModelInvalid(snapshot) || hasApprovalRequired(snapshot)) {
      return {
        state: "needs_you",
        label: hasAskHumanPending(snapshot) ? "Loop Status: Needs You" : (executorModelInvalid(snapshot) ? "Loop Status: Error" : "Loop Status: Needs You"),
        className: hasAskHumanPending(snapshot) ? "loop-attention" : (executorModelInvalid(snapshot) ? "loop-error" : "loop-attention"),
        detail: hasAskHumanPending(snapshot)
          ? "The planner needs a human answer before it can continue."
          : executorModelInvalid(snapshot)
          ? "Codex could not start because the configured model is unavailable or invalid."
          : "The run is waiting for approval or worker attention.",
        stage,
        turn,
        lastUpdate,
        latestActivity: safeString(latestEvent && (latestEvent.summary || latestEvent.event), hasAskHumanPending(snapshot) ? "Planner is waiting for a human answer." : (executorModelInvalid(snapshot) ? "Configured Codex model is unavailable." : stop.title)),
      };
    }

    if (run.completed) {
      return {
        state: "completed",
        label: "Loop Status: Completed",
        className: "loop-completed",
        detail: stop.title,
        stage,
        turn,
        lastUpdate,
        latestActivity: safeString(latestEvent && (latestEvent.summary || latestEvent.event), "Run completed."),
      };
    }

    if (hasActiveRunInProgress(snapshot)) {
      return {
        state: "running",
        label: "Loop Status: Running",
        className: "loop-running",
        detail: "The backend is actively advancing a planner or executor turn. Watch progress or request Safe Stop if you want a clean pause.",
        stage,
        turn,
        lastUpdate,
        latestActivity: safeString(latestEvent && (latestEvent.summary || latestEvent.event), "Run activity is in progress."),
      };
    }

    if (executeReadyToDispatch(snapshot)) {
      return {
        state: "ready_to_continue",
        label: "Loop Status: Ready to Continue",
        className: "loop-attention",
        detail: "Planner selected the next code task. Executor has not started yet. Click Continue Build to dispatch it.",
        stage,
        turn,
        lastUpdate,
        latestActivity: safeString(latestEvent && (latestEvent.summary || latestEvent.event), "Planner selected an execute task. Waiting to dispatch Codex."),
      };
    }

    if (waitingAtSafePoint(snapshot)) {
      return {
        state: "waiting_at_safe_point",
        label: "Loop Status: Waiting at Safe Point",
        className: "loop-attention",
        detail: "The run is paused at a safe checkpoint. Click Continue Build to continue.",
        stage,
        turn,
        lastUpdate,
        latestActivity: safeString(latestEvent && (latestEvent.summary || latestEvent.event), "Waiting at safe point. Continue Build will advance the next step."),
      };
    }

    const errorLike = stop.severity === "danger";
    return {
      state: errorLike ? "error" : "stopped",
      label: errorLike ? "Loop Status: Error" : "Loop Status: Stopped",
      className: errorLike ? "loop-error" : "loop-stopped",
      detail: stop.title,
      stage,
      turn,
      lastUpdate,
      latestActivity: safeString(latestEvent && (latestEvent.summary || latestEvent.event), stop.title),
    };
  }

  function buildConnectionStatusViewModel(snapshot, options = {}) {
    const connection = options.connection || {};
    const connected = Boolean(connection.connected);
    const status = lower(connection.status);
    const seconds = Number(options.elapsedSeconds) || 0;
    const address = safeString(connection.address || options.address, "http://127.0.0.1:44777");

    if (connected) {
      return {
        state: "ready",
        label: "Connection Status: Ready",
        className: "connection-ready",
        durationLabel: `Ready for ${formatDuration(seconds)}`,
        detail: "The shell is attached to the local engine control protocol.",
        address,
      };
    }
    if (status === "connecting") {
      return {
        state: "connecting",
        label: "Connection Status: Connecting...",
        className: "connection-connecting",
        durationLabel: `Connecting... ${formatDuration(seconds)}`,
        detail: "Trying to reach the local engine control protocol.",
        address,
      };
    }
    if (status === "reconnecting" || options.reconnecting) {
      return {
        state: "reconnecting",
        label: "Connection Status: Reconnecting...",
        className: "connection-connecting",
        durationLabel: `Reconnecting... ${formatDuration(seconds)}`,
        detail: "The shell lost contact and is trying to reattach.",
        address,
      };
    }
    return {
      state: "not_connected",
      label: "Connection Status: Not Connected",
      className: "connection-not-connected",
      durationLabel: "Not connected",
      detail: "Start the control server or click Connect.",
      address,
    };
  }

  function buildRecommendedActionViewModel(snapshot, options = {}) {
    const connection = options.connection || {};
    const connected = Boolean(connection.connected);
    const connectionStatus = lower(connection.status);
    const reconnecting = !connected && (connectionStatus === "connecting" || Boolean(options.reconnecting));
    const run = runSnapshot(snapshot);
    const artifactPath = latestArtifactPath(snapshot, options.artifacts);
    const goalEntered = Boolean(options.goalEntered);

    if (!connected) {
      return {
        state: reconnecting ? "reconnecting" : "disconnected",
        title: reconnecting ? "Wait for the app to reconnect." : "Connect to the app engine.",
        detail: reconnecting
          ? "The shell is trying to reconnect. You can also click Connect / Reconnect after the server is back."
          : "Start the local engine, confirm the address, then connect. No repo or run state is available until the shell is connected.",
        primaryAction: {
          id: "connect",
          label: reconnecting ? "Reconnect Now" : "Connect",
          enabled: true,
          kind: "protocol",
        },
      };
    }

    const repoBinding = repoBindingViewModel(snapshot, options);
    if (repoBinding.mismatch) {
      return {
        state: "repo_mismatch",
        title: "Restart the backend for the target repo.",
        detail: repoBinding.message,
        primaryAction: {
          id: "recover_backend",
          label: "Restart Backend for Target Repo",
          enabled: true,
          kind: "shell",
        },
      };
    }

    if (hasStaleActiveRunGuard(snapshot)) {
      const guard = activeRunGuardSnapshot(snapshot);
      return {
        state: "recovery_needed",
        title: "Recover the backend and unlock this repo.",
        detail: safeString(
            guard.message,
            "An old run was active under a previous backend process and is no longer progressing. Recover Backend / Unlock Repo will mechanically clear the stale active-run guard and restart the owned backend only if needed.",
        ),
        primaryAction: {
          id: "recover_backend",
          label: "Recover Backend / Unlock Repo",
          enabled: true,
          kind: "shell",
        },
      };
    }

    const repoContract = repoContractReadinessViewModel(snapshot);
    if (!repoContract.ready) {
      return {
        state: "repo_contract_not_ready",
        title: "Initialize the target repo contract.",
        detail: repoContract.message,
        primaryAction: {
          id: "refresh_status",
          label: "Refresh After Init",
          enabled: true,
          kind: "protocol",
        },
      };
    }

    if (safeStopRequested(snapshot)) {
      const safeStop = buildSafeStopViewModel(snapshot);
      return {
        state: "safe_stop_requested",
        title: "Clear the safe stop when you are ready.",
        detail: `${safeStop.message} This only clears the mechanical stop flag; the planner still decides what happens after continue_run resumes.`,
        primaryAction: {
          id: "clear_stop_continue",
          label: "Clear Stop and Continue",
          enabled: true,
          kind: "protocol",
        },
      };
    }

    if (hasAskHumanPending(snapshot)) {
      const askHuman = buildAskHumanViewModel(snapshot);
      return {
        state: "ask_human",
        title: "Needs your answer.",
        detail: `${askHuman.question} Use Action Required to send a raw answer and continue the run from the GUI.`,
        primaryAction: {
          id: "answer_ask_human",
          label: "Answer and Continue",
          enabled: true,
          kind: "scroll",
          target: "attention",
        },
      };
    }

    if (executorModelInvalid(snapshot)) {
      return {
        state: "model_invalid",
        title: "Change or test the configured Codex model.",
        detail: "Codex reported that the configured model is unavailable to this account. No silent fallback will be used.",
        primaryAction: {
          id: "open_settings",
          label: "Open Model Settings",
          enabled: true,
          kind: "tab",
          target: "settings",
        },
      };
    }

    if (!run) {
      return {
        state: "connected_no_run",
        title: goalEntered ? "Start a new build." : "Enter a goal, then start a build.",
        detail: goalEntered
          ? "The control server is reachable and no run was found for this repo. Start Run will create a durable run through the explicit start_run protocol action."
          : "The app engine is reachable, but no run was found for this repo. Enter a goal in Run Control, then click Start Build.",
        primaryAction: {
          id: "start_run",
          label: "Start Build",
          enabled: goalEntered,
          kind: "protocol",
        },
      };
    }

    if (hasApprovalRequired(snapshot)) {
      return {
        state: "approval_required",
        title: "Review the action required.",
        detail: "Codex or a worker is waiting for a human decision. Open Action Required to review what is being requested.",
        primaryAction: {
          id: "review_approval",
          label: "Review Action Required",
          enabled: true,
          kind: "scroll",
          target: "approval",
        },
      };
    }

    if (executeReadyToDispatch(snapshot)) {
      return {
        state: "ready_to_continue",
        title: "Dispatch the Codex executor.",
        detail: "Planner selected the next implementation task, but Codex has not started yet. Continue Build dispatches the executor through continue_run.",
        primaryAction: {
          id: "continue_run",
          label: "Continue Build / Dispatch Executor",
          enabled: true,
          kind: "protocol",
        },
      };
    }

    if (waitingAtSafePoint(snapshot)) {
      return {
        state: "waiting_at_safe_point",
        title: "Continue from the safe point.",
        detail: "The loop is paused at a durable safe checkpoint. Continue Build advances the next planner-owned action.",
        primaryAction: {
          id: "continue_run",
          label: "Continue Build",
          enabled: true,
          kind: "protocol",
        },
      };
    }

    if (hasActiveRunInProgress(snapshot)) {
      return {
        state: "active",
        title: "Watch progress. No action needed.",
        detail: "The loop appears active. Watch the activity timeline; use Safe Stop only if you want a clean stop at a safe boundary.",
        primaryAction: {
          id: "refresh_status",
          label: "Update Dashboard",
          enabled: true,
          kind: "protocol",
        },
      };
    }

    if (run.completed) {
      return {
        state: "completed",
        title: "Review results or start a new run.",
        detail: artifactPath
          ? "The latest run is complete. Open the latest artifact to inspect what happened, or enter a new goal and start another protocol-backed run."
          : "The latest run is complete. No artifact is currently surfaced, so refresh artifacts or enter a new goal and start another protocol-backed run.",
        primaryAction: {
          id: artifactPath ? "open_latest_artifact" : "start_run",
          label: artifactPath ? "Open Latest Output" : "Start Build",
          enabled: true,
          kind: artifactPath ? "artifact" : "protocol",
        },
      };
    }

    if (run.resumable !== false) {
      return {
        state: "resumable",
        title: "Continue the existing build.",
        detail: "An unfinished run is available. Continue Build resumes it through the explicit continue_run protocol action.",
        primaryAction: {
          id: "continue_run",
          label: "Continue Build",
          enabled: true,
          kind: "protocol",
        },
      };
    }

    if (goalEntered) {
      return {
        state: "fresh_run_available",
        title: "Start a fresh build.",
        detail: "The latest run is cancelled, completed, or not resumable for this repo. Start Build creates a new durable run with the goal you entered.",
        primaryAction: {
          id: "start_run",
          label: "Start Fresh Build",
          enabled: true,
          kind: "protocol",
        },
      };
    }

    return {
      state: "refresh_needed",
      title: "Enter a goal to start fresh, or update the dashboard.",
      detail: "The latest run is not resumable. Enter a new goal to start a fresh build, or refresh the snapshot if you expected a different state.",
      primaryAction: {
        id: "refresh_status",
        label: "Update Dashboard",
        enabled: true,
        kind: "protocol",
      },
    };
  }

  function buildRunControlStateViewModel(snapshot, options = {}) {
    const connected = Boolean(options.connected || (options.connection && options.connection.connected));
    const run = runSnapshot(snapshot);
    const goalEntered = Boolean(options.goalEntered);
    const launchInFlight = Boolean(options.launchInFlight);
    const modelHealthChecking = Boolean(options.modelHealthChecking);
    const askHuman = buildAskHumanViewModel(snapshot);
    const staleGuard = hasStaleActiveRunGuard(snapshot);
    const repoBinding = repoBindingViewModel(snapshot, options);
    const repoContract = repoContractReadinessViewModel(snapshot);
    const executeReady = executeReadyToDispatch(snapshot);
    const safePoint = waitingAtSafePoint(snapshot);
    const processing = hasActiveRunInProgress(snapshot);
    const stopRequested = safeStopRequested(snapshot);
    const startDisabledReasons = [];
    const continueDisabledReasons = [];

    if (!connected) {
      startDisabledReasons.push("Disabled because the app is not connected to the engine.");
      continueDisabledReasons.push("Disabled because the app is not connected to the engine.");
    }
    if (launchInFlight) {
      startDisabledReasons.push("Disabled because a run action is already launching.");
      continueDisabledReasons.push("Disabled because a run action is already launching.");
    }
    if (modelHealthChecking) {
      startDisabledReasons.push("Disabled because model health is checking.");
      continueDisabledReasons.push("Disabled because model health is checking.");
    }
    if (repoBinding.mismatch) {
      startDisabledReasons.push(repoBinding.message);
      continueDisabledReasons.push(repoBinding.message);
    }
    if (staleGuard) {
      startDisabledReasons.push("Disabled because a stale active run is blocking this repo. Use Recover Backend / Unlock Repo first.");
      continueDisabledReasons.push("Disabled because a stale active run is blocking this repo. Use Recover Backend / Unlock Repo first.");
    }
    if (!repoContract.ready) {
      startDisabledReasons.push(`Disabled because ${repoContract.message}`);
      continueDisabledReasons.push(`Disabled because ${repoContract.message}`);
    }
    if (!goalEntered) {
      startDisabledReasons.push("Disabled because Start Build needs a goal.");
    }
    const unfinishedResumableRun = Boolean(run && !run.completed && run.resumable !== false);
    if (unfinishedResumableRun) {
      startDisabledReasons.push("Disabled because an unfinished run already exists. Continue it, answer the planner, or safe-stop it before starting another build.");
    }
    if (askHuman.present) {
      continueDisabledReasons.push("The planner is waiting for your answer; use Action Required, then continue with the queued answer.");
    }
    if (stopRequested) {
      continueDisabledReasons.push("Disabled because a safe stop was requested; use Clear Stop and Continue.");
    }
    if (processing) {
      continueDisabledReasons.push("Disabled because the backend is already actively processing this run.");
    }
    if (!run) {
      continueDisabledReasons.push("Disabled because no existing run was found.");
    } else if (run.completed) {
      continueDisabledReasons.push("Disabled because the latest run is already complete.");
    } else if (run.resumable === false) {
      continueDisabledReasons.push("Disabled because the latest run is not marked resumable.");
    }

    const startEnabled = connected && !launchInFlight && !modelHealthChecking
      && !repoBinding.mismatch
      && !staleGuard
      && repoContract.ready
      && goalEntered
      && !unfinishedResumableRun;
    const continueEnabled = connected && !launchInFlight && !modelHealthChecking
      && !repoBinding.mismatch
      && !staleGuard
      && repoContract.ready
      && !stopRequested
      && !processing
      && Boolean(run && !run.completed && run.resumable !== false && !askHuman.present);
    const primaryNote = stopRequested
      ? "Safe stop was requested. Use Clear Stop and Continue to clear the stop flag, then resume through continue_run."
      : askHuman.present
      ? "Planner is waiting for your answer. Open Action Required to send the answer and continue from the GUI."
      : executeReady
        ? "Planner selected an implementation task. Continue Build / Dispatch Executor starts the Codex executor turn."
      : safePoint
        ? "Waiting at a safe checkpoint. Continue Build advances the next planner-owned step."
      : processing
        ? "The backend is actively processing this run. Watch Live Output, or use Safe Stop if you need a clean pause."
      : staleGuard
        ? "Recovery needed. Recover Backend / Unlock Repo clears stale active-run state without deleting history or artifacts, and restarts the owned backend only if needed."
      : repoBinding.mismatch
        ? "Wrong repo backend. Restart Backend for Target Repo before showing or acting on run state."
      : !repoContract.ready
        ? repoContract.message
      : (startDisabledReasons[0] || continueDisabledReasons[0] || "Start and Continue use explicit engine protocol actions.");

    return {
      askHuman,
      startEnabled,
      continueEnabled,
      startLabel: "Start Build",
      continueLabel: executeReady ? "Continue Build / Dispatch Executor" : "Continue Build",
      startDisabledReason: startEnabled ? "" : safeString(startDisabledReasons[0], "Start Build is unavailable for the current state."),
      continueDisabledReason: continueEnabled ? "" : safeString(continueDisabledReasons[0], "Continue Build is unavailable for the current state."),
      note: primaryNote,
    };
  }

  function buildTopStatusViewModel(snapshot, options = {}) {
    const connection = options.connection || {};
    const runtime = runtimeSnapshot(snapshot);
    const run = runSnapshot(snapshot);
    const approval = approvalSnapshot(snapshot);
    const pending = pendingActionSnapshot(snapshot);
    const askHuman = buildAskHumanViewModel(snapshot);
    const connected = Boolean(connection.connected);
    const reconnecting = !connected && (lower(connection.status) === "connecting" || Boolean(options.reconnecting));
    const connectionStatus = buildConnectionStatusViewModel(snapshot, {
      connection,
      address: options.address,
      reconnecting,
      elapsedSeconds: options.connectionElapsedSeconds,
    });
    const loopStatus = buildLoopStatusViewModel(snapshot, {
      launching: Boolean(options.launching),
      latestEvent: options.latestEvent,
      lastUpdateLabel: options.lastUpdateLabel,
      expectedRepoPath: options.expectedRepoPath,
      connection: options.connection,
    });
    const stop = translateStopReason(run && run.stop_reason);
    const modelHealth = modelHealthSnapshot(snapshot);
    const repoBinding = repoBindingViewModel(snapshot, options);
    const blocker = repoBinding.mismatch
      ? repoBinding.message
      : executorModelInvalid(snapshot)
      ? safeString(modelHealth.message, "Configured Codex model is unavailable.")
      : hasApprovalRequired(snapshot)
      ? safeString(approval.summary || approval.message || approval.state, "Approval required")
      : askHuman.present
      ? askHuman.question
      : (pending && pending.held
        ? `Pending action held: ${safeString(pending.hold_reason, "held")}`
        : (run && run.stop_reason ? stop.title : "None"));

    return {
      connectionState: connected ? "Ready" : (reconnecting ? "Reconnecting..." : "Not Connected"),
      connectionLabel: connectionStatus.label,
      connectionDetail: connectionStatus.detail,
      connectionDurationLabel: connectionStatus.durationLabel,
      connectionClass: connectionStatus.className,
      address: safeString(connection.address || options.address, "http://127.0.0.1:44777"),
      expectedRepoRoot: repoBinding.expected,
      actualRepoRoot: repoBinding.actual,
      repoRoot: safeString(runtime.repo_root, "No repo loaded from control server yet"),
      repoMatch: repoBinding.matches,
      repoMismatch: repoBinding.mismatch,
      repoBindingMessage: repoBinding.message,
      repoReady: repoContractReadinessViewModel(snapshot).ready,
      repoContractMissing: repoContractReadinessViewModel(snapshot).missing,
      runID: repoBinding.mismatch ? "Hidden until repo matches" : (run ? safeString(run.id) : "No active run"),
      runState: repoBinding.mismatch ? "repo mismatch" : runStateLabel(run),
      loopState: loopStatus.state,
      loopLabel: loopStatus.label,
      loopClass: loopStatus.className,
      loopDetail: loopStatus.detail,
      loopStage: loopStatus.stage,
      loopTurn: loopStatus.turn,
      loopLastUpdate: loopStatus.lastUpdate,
      loopLatestActivity: loopStatus.latestActivity,
      blocker,
      verbosity: safeString(runtime.verbosity || options.verbosity, "normal"),
      lastRefreshedAt: safeString(options.lastRefreshedAt, "Not refreshed yet"),
    };
  }

  function buildContractStatusViewModel(contractFiles) {
    const files = contractFiles && Array.isArray(contractFiles.files) ? contractFiles.files : [];
    const canonical = [
      ".orchestrator/brief.md",
      ".orchestrator/roadmap.md",
      ".orchestrator/decisions.md",
      ".orchestrator/human-notes.md",
      "AGENTS.md",
    ];
    const normalized = canonical.map((path) => {
      const match = files.find((file) => file.path === path);
      return {
        path,
        exists: Boolean(match && match.exists),
        modifiedAt: safeString(match && match.modified_at, "Unavailable"),
      };
    });
    return {
      loaded: files.length > 0,
      count: files.length,
      files: normalized,
      missingCount: normalized.filter((file) => !file.exists).length,
      message: files.length === 0
        ? "Contract status has not been loaded yet. Connect or refresh everything to inspect canonical files."
        : "",
    };
  }

  function buildHomeDashboardViewModel(snapshot, options = {}) {
    const topStatus = buildTopStatusViewModel(snapshot, options);
    const recommendation = buildRecommendedActionViewModel(snapshot, options);
    const repoBinding = repoBindingViewModel(snapshot, options);
    const repoContract = repoContractReadinessViewModel(snapshot);
    const rawStatus = buildStatusViewModel(snapshot);
    const status = repoBinding.mismatch
      ? {
        ...rawStatus,
        runID: "Hidden until repo matches",
        goal: "Wrong repo backend. Restart Backend for Target Repo before reading or acting on run state.",
        stopReason: "repo_mismatch",
        nextOperatorAction: "restart_backend_for_target_repo",
        latestPlannerOutcome: "Unavailable",
        executorTurnStatus: "Unavailable",
        pendingHeld: false,
      }
      : rawStatus;
    const progress = buildProgressPanelViewModel(snapshot);
    const pending = buildPendingActionViewModel(snapshot);
    const approval = buildApprovalViewModel(snapshot);
    const whatHappened = buildWhatHappenedViewModel(snapshot, options.artifacts, options.events || []);
    const codex = buildCodexReadinessViewModel(snapshot);
    const artifacts = buildArtifactListViewModel(snapshot, options.artifacts);
    const contractStatus = buildContractStatusViewModel(options.contractFiles);
    const recentEvents = buildActivityTimelineViewModel(options.events || [], {
      categories: {
        planner: true,
        executor: true,
        worker: true,
        approval: true,
        intervention: true,
        fault: true,
        terminal: true,
        status: true,
        other: true,
      },
    }).items.slice(0, 4);
    const latestError = buildLatestErrorViewModel(snapshot, options.events || []);

    return {
      topStatus,
      recommendation,
      status,
      progress,
      pending,
      approval,
      whatHappened,
      codex,
      artifacts,
      contractStatus,
      homeError: safeString(options.homeError, ""),
      preparedCommand: safeString(options.preparedCommand, ""),
      refreshedLabel: topStatus.lastRefreshedAt,
      repo: {
        root: topStatus.repoRoot,
        expected: repoBinding.expected,
        actual: repoBinding.actual,
        matches: repoBinding.matches,
        mismatch: repoBinding.mismatch,
        ready: topStatus.repoReady,
        message: repoBinding.mismatch
          ? repoBinding.message
          : repoContract.message,
      },
      latestPlannerMessage: status.operatorMessage,
      latestArtifactPath: artifacts.latestPath,
      recentActivity: recentEvents,
      liveOutput: {
        title: "See what the AI/CLI is doing.",
        detail: latestError.present
          ? `Latest error: ${latestError.summary}`
          : "Open Live Output for planner, executor/Codex, worker, approval, model, and shell activity. Verbose and Trace show more technical detail there.",
        primaryAction: { id: "open_live_output", label: "Open Live Output" },
        latestError,
      },
      debugBundleAvailable: Boolean(snapshot),
      emptyStates: {
        noRun: "No run found for this repo yet. Start a new run or use the terminal to run `orchestrator run --goal ...`.",
        noArtifacts: "No artifacts yet. Artifacts appear after planner/executor turns complete.",
        noPendingAction: "No pending action. The engine is not currently holding a next action.",
        noRepoTree: "Repo tree is empty because the shell is disconnected or no repo root is loaded.",
      },
    };
  }

  function buildStatusViewModel(snapshot) {
    const runtime = snapshot && snapshot.runtime ? snapshot.runtime : {};
    const run = snapshot && snapshot.run ? snapshot.run : null;
    const checkpoint = latestCheckpoint(run);
    const plannerStatus = snapshot && snapshot.planner_status ? snapshot.planner_status : {};
    const pending = snapshot && snapshot.pending_action ? snapshot.pending_action : {};
    const modelHealth = modelHealthSnapshot(snapshot);
    const plannerModel = modelHealth.planner || {};
    const executorModel = modelHealth.executor || {};

    return {
      hasRun: Boolean(run),
      runID: run ? safeString(run.id) : "No active run",
      goal: run ? safeString(run.goal) : "No run found for this repo yet. Start a new run or use the terminal to run orchestrator run.",
      stopReason: run ? safeString(run.stop_reason, "None") : "None",
      startedAt: run ? safeString(run.started_at, "Unavailable") : "Unavailable",
      stoppedAt: run ? safeString(run.stopped_at, "Unavailable") : "Unavailable",
      elapsedLabel: run ? safeString(run.elapsed_label, "Elapsed time unavailable") : "Elapsed time unavailable",
      elapsedSeconds: run && Number.isFinite(run.elapsed_seconds) ? Number(run.elapsed_seconds) : 0,
      executorLastError: run ? safeString(run.executor_last_error, "None") : "None",
      executorFailureStage: run ? safeString(run.executor_failure_stage, "Unavailable") : "Unavailable",
      executorTurnStatus: run ? safeString(run.executor_turn_status || run.executor_status || run.latest_executor_status, "Unavailable") : "Unavailable",
      checkpointStage: safeString(checkpoint.stage, "Unavailable"),
      checkpointLabel: safeString(checkpoint.label, "Unavailable"),
      checkpointSafePause: checkpoint.safe_pause === true,
      latestPlannerOutcome: run ? safeString(run.latest_planner_outcome || run.planner_outcome, "Unavailable") : "Unavailable",
      nextOperatorAction: run ? safeString(run.next_operator_action, "Unavailable") : "Unavailable",
      activityState: run ? safeString(run.activity_state, "Unavailable") : "Unavailable",
      activityMessage: run ? safeString(run.activity_message, "Unavailable") : "Unavailable",
      completed: Boolean(run && run.completed),
      operatorMessage: plannerStatus.present ? safeString(plannerStatus.operator_message, "No operator message yet") : "No operator message yet",
      progressPercent: Number.isInteger(plannerStatus.progress_percent) ? plannerStatus.progress_percent : null,
      currentFocus: plannerStatus.present ? safeString(plannerStatus.current_focus, "Unavailable") : "Unavailable",
      nextIntendedStep: plannerStatus.present ? safeString(plannerStatus.next_intended_step, "Unavailable") : "Unavailable",
      whyThisStep: plannerStatus.present ? safeString(plannerStatus.why_this_step, "Unavailable") : "Unavailable",
      verbosity: safeString(runtime.verbosity, "normal"),
      plannerModelConfigured: safeString(plannerModel.configured_model, "Unavailable"),
      plannerModelVerification: safeString(plannerModel.verification_state, "not_verified"),
      executorModelRequested: safeString(executorModel.requested_model || executorModel.configured_model, "Not reported yet"),
      executorModelVerification: safeString(executorModel.verification_state, "not_verified"),
      executorModelError: safeString(executorModel.last_error, "None"),
      modelHealthMessage: safeString(modelHealth.message, "Model health has not been checked yet."),
      modelHealthBlocking: Boolean(modelHealth.blocking),
      pendingActionSummary: pending && pending.present
        ? safeString(pending.pending_action_summary || pending.pending_reason, "Pending action recorded")
        : "No pending action. The engine is not currently holding a next action.",
      pendingHeld: Boolean(pending && pending.held),
    };
  }

  function buildProgressPanelViewModel(snapshot) {
    const plannerStatus = snapshot && snapshot.planner_status ? snapshot.planner_status : {};
    const roadmap = snapshot && snapshot.roadmap ? snapshot.roadmap : {};
    const progressPercent = Number.isInteger(plannerStatus.progress_percent)
      && plannerStatus.progress_percent >= 0
      && plannerStatus.progress_percent <= 100
      ? plannerStatus.progress_percent
      : null;

    const progressBasis = plannerStatus.present ? safeString(plannerStatus.progress_basis, "Unavailable") : "Unavailable";
    const currentFocus = plannerStatus.present ? safeString(plannerStatus.current_focus, "Unavailable") : "Unavailable";
    const nextIntendedStep = plannerStatus.present ? safeString(plannerStatus.next_intended_step, "Unavailable") : "Unavailable";
    const whyThisStep = plannerStatus.present ? safeString(plannerStatus.why_this_step, "Unavailable") : "Unavailable";
    const roadmapAlignmentText = safeString(roadmap.alignment_text || roadmap.preview, roadmap.message || "No roadmap context available yet.");
    const sections = [
      buildProgressSection("progress_basis", "Progress Basis", progressBasis, { open: progressBasis.length <= 320 }),
      buildProgressSection("current_focus", "Current Focus", currentFocus, { open: currentFocus.length <= 260 }),
      buildProgressSection("next_intended_step", "Next Intended Step", nextIntendedStep, { open: nextIntendedStep.length <= 260 }),
      buildProgressSection("why_this_step", "Why This Step", whyThisStep, { open: whyThisStep.length <= 260 }),
      buildProgressSection("roadmap_alignment", "Roadmap Alignment / Context", roadmapAlignmentText, { open: false }),
    ];

    return {
      operatorMessage: plannerStatus.present ? safeString(plannerStatus.operator_message, "No operator message yet") : "No operator message yet",
      progressPercent,
      progressBarWidth: progressPercent === null ? "0%" : `${progressPercent}%`,
      progressConfidence: plannerStatus.present ? safeString(plannerStatus.progress_confidence, "Unavailable") : "Unavailable",
      progressBasis,
      progressBasisPreview: truncateText(progressBasis, 180),
      currentFocus,
      currentFocusPreview: truncateText(currentFocus, 160),
      nextIntendedStep,
      nextIntendedStepPreview: truncateText(nextIntendedStep, 160),
      whyThisStep,
      whyThisStepPreview: truncateText(whyThisStep, 160),
      roadmapPresent: Boolean(roadmap.present),
      roadmapPath: safeString(roadmap.path, ".orchestrator/roadmap.md"),
      roadmapAlignmentText,
      roadmapAlignmentPreview: truncateText(roadmapAlignmentText, 180),
      roadmapModifiedAt: safeString(roadmap.modified_at, "Unavailable"),
      roadmapMessage: safeString(roadmap.message, ""),
      sections,
    };
  }

  function buildProgressSection(id, label, value, options = {}) {
    const text = safeString(value, "Unavailable");
    return {
      id,
      label,
      value: text,
      preview: truncateText(text, 220),
      isLong: text.length > 220,
      open: Boolean(options.open),
      available: text !== "Unavailable",
    };
  }

  function formatEventSummary(event) {
    const payload = event && event.payload ? event.payload : {};
    const runID = payload.run_id ? ` run=${payload.run_id}` : "";
    const explicitSummary = safeString(event && event.summary, "");
    if (explicitSummary) {
      return explicitSummary;
    }

    const name = safeString(event && event.event, "unknown_event");
    switch (name) {
      case "run_started":
        return `Build loop started${runID}.`;
      case "run_completed":
        return `Build loop completed${runID}.`;
      case "runtime.initialized":
      case "runtime_initialized":
        return "Orchestrator initialized successfully.";
      case "planner_prompt_sent":
        return `Prompt sent to planner${runID}.`;
      case "planner_response_received":
        return `Planner response received${runID}.`;
      case "planner_requested_repo_context":
        return `Planner requested repo context${runID}.`;
      case "context_collected":
        return `Context collected${runID}.`;
      case "planner_turn_started":
        return `Awaiting planner response${runID}.`;
      case "planner_turn_completed":
        if (safeString(payload.planner_outcome, "") === "execute") {
          return `Planner selected an execute task. Waiting to dispatch Codex${runID}.`;
        }
        return `Planner response received${runID}.`;
      case "executor_dispatch_requested":
        return `Prompt sent to Codex${runID}.`;
      case "executor_turn_started":
        return `Codex turn started${runID}.`;
      case "executor_turn_completed":
        return `Codex response received${runID}.`;
      case "executor_waiting":
        return `Waiting on Codex${runID}.`;
      case "executor_turn_failed":
        if (modelUnavailableFromText(payload.error_message)) {
          return `Codex could not start because the configured model is unavailable${runID}.`;
        }
        return `Codex executor turn failed${payload.error_message ? `: ${payload.error_message}` : runID}.`;
      case "executor_approval_required":
      case "approval_required":
        return `Run stopped because approval is required${runID}.`;
      case "fault_recorded":
        if (modelUnavailableFromText(payload.error || payload.message)) {
          return `Configured model is unavailable${runID}.`;
        }
        return `A fault was recorded${runID}.`;
      case "model_health_tested":
        return `${safeString(payload.component, "Model")} health was tested.`;
      case "model_health_failed":
        return `${safeString(payload.component, "Model")} health check failed${payload.error ? `: ${payload.error}` : ""}.`;
      case "verbosity_changed":
        return `Verbosity changed to ${safeString(payload.verbosity, "the selected level")}.`;
      case "safe_point_reached":
        return `A safe pause point was reached${runID}.`;
      case "safe_pause_requested":
        return `Safe pause requested${runID}.`;
      case "paused_at_safe_point":
        return `Paused at safe point${runID}.`;
      case "control_message_queued":
        return `Human reply received${runID}.`;
      case "control_message_consumed":
        return `Planner received your control message${runID}.`;
      case "human_question_presented":
      case "human.question.presented":
      case "ask_human":
        return `Waiting on human input${runID}.`;
      case "file_changes_detected":
        return `Files changed${runID}.`;
      case "tests_started":
        return `Tests started${runID}.`;
      case "tests_completed":
        return `Tests completed${runID}.`;
      case "snapshot_captured":
        return `Snapshot captured${payload.artifact_path ? `: ${payload.artifact_path}` : runID}.`;
      case "setup_action_completed":
        return `Setup action completed${payload.action ? `: ${payload.action}` : ""}.`;
      case "worker_created":
        return `Worker was created${runID}.`;
      case "worker_dispatch_completed":
        return `Worker completed a dispatched turn${runID}.`;
      case "artifact_created":
        return `Artifact created${payload.artifact_path ? `: ${payload.artifact_path}` : runID}.`;
      case "terminal_session_started":
        return "Operator terminal tab started.";
      case "terminal_session_closed":
        return "Operator terminal tab closed.";
      default:
        return `${name}${runID}`;
    }
  }

  function classifyActivityCategory(event) {
    const name = safeString(event && event.event, "unknown_event",).toLowerCase();
    if (name.startsWith("terminal_")) {
      return "terminal";
    }
    if (name.includes("snapshot") || name.includes("setup")) {
      return "setup";
    }
    if (name.includes("human") || name.includes("control_message")) {
      return "human";
    }
    if (name.includes("file") || name.includes("artifact")) {
      return "files";
    }
    if (name.includes("test")) {
      return "tests";
    }
    if (name.includes("model")) {
      return "fault";
    }
    if (name.includes("approval")) {
      return "approval";
    }
    if (name.startsWith("worker_")) {
      return "worker";
    }
    if (
      name.startsWith("control_message_")
      || name.startsWith("safe_point_intervention_")
      || name.startsWith("planner_intervention_")
      || name.startsWith("pending_action_")
    ) {
      return "intervention";
    }
    if (name.startsWith("planner_")) {
      return "planner";
    }
    if (name.startsWith("executor_")) {
      return "executor";
    }
    if (name.includes("fault") || name.includes("error")) {
      return "fault";
    }
    if (
      name.startsWith("run_")
      || name.startsWith("runtime")
      || name.startsWith("status_")
      || name.startsWith("safe_point_")
      || name.startsWith("verbosity_")
    ) {
      return "system";
    }
    return "other";
  }

  function categoryLabel(category) {
    switch (category) {
      case "planner":
        return "Planner";
      case "executor":
        return "Executor";
      case "worker":
        return "Worker";
      case "human":
        return "Human";
      case "files":
        return "Files";
      case "tests":
        return "Tests";
      case "setup":
        return "Setup";
      case "approval":
        return "Approval";
      case "intervention":
        return "Intervention";
      case "fault":
        return "Fault";
      case "terminal":
        return "Terminal";
      case "system":
      case "status":
        return "System";
      default:
        return "Other";
    }
  }

  function severityForCategory(category) {
    switch (category) {
      case "fault":
        return "danger";
      case "approval":
      case "intervention":
        return "warning";
      case "planner":
      case "executor":
      case "worker":
      case "files":
      case "tests":
      case "setup":
        return "info";
      case "terminal":
      case "system":
      case "status":
      default:
        return "neutral";
    }
  }

  function formatEventTimestamp(value) {
    const date = new Date(value || "");
    if (Number.isNaN(date.getTime())) {
      return safeString(value, "Unavailable");
    }
    return date.toLocaleString();
  }

  function buildActivityTimelineViewModel(events, options = {}) {
    const items = Array.isArray(events) ? events : [];
    const categories = options.categories || {};
    const searchText = String(options.searchText || "").trim().toLowerCase();
    const currentRunOnly = Boolean(options.currentRunOnly);
    const currentRunID = String(options.currentRunID || "").trim();
    const verbosity = buildVerbosityViewModel(options.verbosity || "normal").value;

    const normalized = items.map((event) => {
      const payload = event && event.payload ? event.payload : {};
      const category = classifyActivityCategory(event);
      const summary = safeString(event.summary || formatEventSummary(event), "unknown_event");
      const payloadText = JSON.stringify(payload || {}, null, 2);
      const runID = safeString(payload.run_id, "");
      const eventName = safeString(event.event, "unknown_event");
      const modelError = modelUnavailableFromText(payload.error_message || payload.error || payload.message);
      const severity = modelError || eventName.includes("failed") ? "danger" : severityForCategory(category);
      return {
        id: safeString(event.sequence, `${event.event || "event"}-${event.at || "unknown"}`),
        eventName,
        summary,
        category,
        categoryLabel: categoryLabel(category),
        severity,
        at: safeString(event.at, "Unavailable"),
        timestampLabel: formatEventTimestamp(event.at),
        sequence: safeString(event.sequence, "local"),
        runID,
        payloadText,
        sourceLabel: event.local ? "local shell" : "engine",
        showPayload: verbosity === "trace",
      };
    });

    const filtered = normalized.filter((item) => {
      if (!eventVisibleForVerbosity(item, verbosity)) {
        return false;
      }
      if (categories[item.category] === false) {
        return false;
      }

      if (currentRunOnly && currentRunID && item.runID !== "" && item.runID !== currentRunID) {
        return false;
      }

      if (searchText === "") {
        return true;
      }

      const haystack = [
        item.summary,
        item.eventName,
        item.categoryLabel,
        item.runID,
        item.payloadText,
      ].join("\n").toLowerCase();
      return haystack.includes(searchText);
    });

    const latestError = normalized.find((item) => item.severity === "danger") || null;
    return {
      totalCount: normalized.length,
      filteredCount: filtered.length,
      currentRunOnly,
      currentRunID: currentRunID || "Unavailable",
      searchText,
      verbosity,
      items: filtered,
      latestError,
      emptyMessage: normalized.length === 0
        ? "No events received yet. Connect and use the controls to generate real protocol traffic."
        : "No events match the current filters.",
    };
  }

  function eventVisibleForVerbosity(item, verbosity) {
    if (verbosity === "trace" || verbosity === "verbose") {
      return true;
    }
    if (verbosity === "quiet") {
      return item.severity === "danger"
        || item.category === "approval"
        || item.eventName === "run_started"
        || item.eventName === "run_completed"
        || item.eventName === "executor_turn_failed"
        || item.eventName === "fault_recorded";
    }
    return item.category !== "terminal" || item.severity === "danger";
  }

  function buildTerminalTabsViewModel(snapshot) {
    const terminal = snapshot || {};
    const sessions = Array.isArray(terminal.sessions) ? terminal.sessions : [];
    const activeSessionID = safeString(terminal.active_session_id, "");
    const activeSession = terminal.active_session || sessions.find((session) => session.session_id === activeSessionID) || null;

    return {
      count: Number.isInteger(terminal.count) ? terminal.count : sessions.length,
      activeSessionID,
      activeSession,
      activeSummary: activeSession
        ? `${safeString(activeSession.label)} | ${safeString(activeSession.status)} | ${safeString(activeSession.message, "no message")}`
        : safeString(terminal.message, "No terminal sessions yet."),
      output: activeSession && activeSession.buffered_output
        ? activeSession.buffered_output
        : "Create a terminal tab to open a local operator shell session.",
      canStart: !activeSession || (activeSession.status !== "running" && activeSession.status !== "starting"),
      canStop: Boolean(activeSession && (activeSession.status === "running" || activeSession.status === "starting")),
      canClose: Boolean(activeSession),
      canSend: Boolean(activeSession && activeSession.status === "running"),
      sessions: sessions.map((session) => ({
        sessionID: safeString(session.session_id),
        label: safeString(session.label, safeString(session.shell_label, "Shell")),
        status: safeString(session.status, "stopped"),
        shellLabel: safeString(session.shell_label, "Shell"),
        pid: session.pid,
        exitCode: session.exit_code,
        selected: safeString(session.session_id) === activeSessionID,
      })),
    };
  }

  function buildPendingActionViewModel(snapshot) {
    const pending = snapshot && snapshot.pending_action ? snapshot.pending_action : {};
    const dispatch = pending && pending.pending_dispatch_target ? pending.pending_dispatch_target : null;
    return {
      present: Boolean(pending.present),
      message: safeString(pending.message, "No pending action. The engine is not currently holding a next action."),
      plannerOutcome: safeString(pending.planner_outcome, "Unavailable"),
      summary: pending.present ? safeString(pending.pending_action_summary || pending.pending_reason, "Pending action recorded") : "No pending action. The engine is not currently holding a next action.",
      held: Boolean(pending.held),
      holdReason: safeString(pending.hold_reason, "None"),
      executorPromptSummary: safeString(pending.pending_executor_prompt_summary, "Unavailable"),
      executorPrompt: safeString(pending.pending_executor_prompt, ""),
      dispatchTarget: dispatch
        ? safeString([dispatch.kind, dispatch.worker_name || dispatch.worker_id].filter(Boolean).join(" / "), "Unavailable")
        : "Unavailable",
      updatedAt: safeString(pending.updated_at, "Unavailable"),
    };
  }

  function buildArtifactListViewModel(snapshot, artifactListing) {
    const snapshotArtifacts = snapshot && snapshot.artifacts ? snapshot.artifacts : {};
    const listing = artifactListing && Array.isArray(artifactListing.items) ? artifactListing : snapshotArtifacts;
    const items = Array.isArray(listing.items) ? listing.items : [];

    return {
      latestPath: safeString((listing.latest_path || snapshotArtifacts.latest_path), "Unavailable"),
      message: items.length === 0 ? safeString(listing.message || snapshotArtifacts.message, "No artifacts yet. Artifacts appear after planner/executor turns complete.") : "",
      items: items.map((item) => ({
        path: safeString(item.path),
        category: safeString(item.category, "artifact"),
        source: safeString(item.source, "unknown"),
        latest: Boolean(item.latest),
        preview: safeString(item.preview, "No preview"),
        at: safeString(item.at, "Unavailable"),
      })),
    };
  }

  function buildApprovalViewModel(snapshot) {
    const approval = snapshot && snapshot.approval ? snapshot.approval : {};
    const workerApprovalRequired = workerApprovalCount(snapshot);
    const askHuman = buildAskHumanViewModel(snapshot);
    const staleGuard = hasStaleActiveRunGuard(snapshot);
    const safeStop = buildSafeStopViewModel(snapshot);
    const guard = activeRunGuardSnapshot(snapshot);
    const defaultSummary = workerApprovalRequired > 0
      ? `${workerApprovalRequired} worker approval(s) pending. Worker-specific shell controls are still deferred in this slice.`
      : askHuman.present
        ? "Planner needs your answer."
        : staleGuard
          ? "A stale active run guard is blocking this repo."
          : safeStop.present
            ? "Safe stop was requested."
      : "No approval needed.";
    const primaryApprovalRequired = hasPrimaryApprovalRequired(snapshot);
    const needsAttention = primaryApprovalRequired || workerApprovalRequired > 0 || askHuman.present || staleGuard || safeStop.present;
    const command = safeString(approval.command, "");
    const kind = safeString(approval.kind, "Unavailable");
    const approvalType = kind === "command_execution"
      ? "Codex wants to run a command."
      : kind === "file_change"
        ? "Codex wants to apply file changes."
        : kind === "permissions"
          ? "Codex is asking for broader permissions."
          : "Codex is asking for approval.";

    return {
      present: primaryApprovalRequired,
      reportedPresent: Boolean(approval.present),
      needsAttention,
      badgeCount: (primaryApprovalRequired ? 1 : 0) + workerApprovalRequired + (askHuman.present ? 1 : 0) + (staleGuard ? 1 : 0) + (safeStop.present ? 1 : 0),
      askHuman,
      safeStop,
      staleGuard: staleGuard
        ? {
          present: true,
          runID: safeString(guard.run_id || guard.runID, "Unavailable"),
          backendPID: safeString(guard.backend_pid || guard.backendPID, "Unavailable"),
          sessionID: safeString(guard.session_id || guard.sessionID, "Unavailable"),
          reason: safeString(guard.stale_reason || guard.staleReason, "active run guard belongs to a previous backend process/session"),
          message: safeString(guard.message, "Recover Backend / Unlock Repo can mechanically clear the stale active-run guard without changing the run."),
        }
        : { present: false },
      state: safeString(approval.state, "none"),
      kind,
      summary: askHuman.present ? askHuman.question : (safeStop.present ? safeStop.message : safeString(approval.summary || approval.message, defaultSummary)),
      runID: safeString(approval.run_id, "Unavailable"),
      executorTurnID: safeString(approval.executor_turn_id, "Unavailable"),
      executorThreadID: safeString(approval.executor_thread_id, "Unavailable"),
      reason: safeString(approval.reason, "Unavailable"),
      command: safeString(approval.command, "Unavailable"),
      cwd: safeString(approval.cwd, "Unavailable"),
      grantRoot: safeString(approval.grant_root, "Unavailable"),
      lastControlAction: safeString(approval.last_control_action, "Unavailable"),
      workerApprovalRequired,
      availableActions: primaryApprovalRequired && Array.isArray(approval.available_actions) ? approval.available_actions : [],
      canApprove: primaryApprovalRequired && Array.isArray(approval.available_actions) && approval.available_actions.includes("approve"),
      canDeny: primaryApprovalRequired && Array.isArray(approval.available_actions) && approval.available_actions.includes("deny"),
      message: safeString(approval.message, defaultSummary),
      plainEnglish: askHuman.present
        ? {
          title: "Planner needs your answer",
          requested: askHuman.question,
          why: askHuman.blocker,
          approveEffect: "Send Answer and Continue queues your raw message for the planner, then starts continue_run for this run.",
          denyEffect: "There is no approval denial here. If you do not want to continue, leave the answer empty or use Safe Stop.",
          scope: `Run ${askHuman.runID}`,
        }
        : staleGuard
        ? {
          title: "Recovery Needed",
          requested: "Recover Backend / Unlock Repo",
          why: safeString(guard.message, "An old run was active under a previous backend process and is no longer progressing."),
          approveEffect: "Recovery clears the stale active-run guard while preserving run status, checkpoint, history, and artifacts. It restarts only the dogfood-owned backend when needed.",
          denyEffect: "If you do nothing, Start/Continue may remain blocked because the old active-run guard is still present.",
          scope: `Run ${safeString(guard.run_id || guard.runID, "Unavailable")} from backend PID ${safeString(guard.backend_pid || guard.backendPID, "Unavailable")}`,
        }
        : safeStop.present
        ? {
          title: "Safe Stop Requested",
          requested: "Clear Stop and Continue",
          why: safeStop.message,
          approveEffect: "Clear Stop and Continue removes the mechanical stop flag, then calls continue_run if the run is resumable.",
          denyEffect: "If you do nothing, the safe stop remains active and the run will not continue from the shell.",
          scope: `Run ${safeStop.runID}`,
        }
        : needsAttention
        ? {
          title: "Action Required",
          requested: primaryApprovalRequired ? approvalType : `${workerApprovalRequired} worker approval(s) need attention.`,
          why: primaryApprovalRequired
            ? safeString(approval.reason, "Codex paused because the engine reported an approval-required state.")
            : "A worker is waiting for a worker-specific approval. The shell shows this truthfully, but worker approval controls remain deferred.",
          approveEffect: primaryApprovalRequired
            ? "Approve tells Codex it may continue with this specific request."
            : "Use the headless worker approval command for worker-specific approval in this slice.",
          denyEffect: primaryApprovalRequired
            ? "Deny records the denial and lets the planner/runtime handle the next safe step."
            : "Use the headless worker denial command for worker-specific denial in this slice.",
          scope: command || safeString(approval.cwd || approval.grant_root, "Scope details are not available in the latest snapshot."),
        }
        : {
          title: "No Action Required",
          requested: "No approval needed.",
          why: "The latest status snapshot may include old run/thread IDs, but it does not show an actionable approval state.",
          approveEffect: "",
          denyEffect: "",
          scope: "",
        },
    };
  }

  function buildRunSummaryViewModel(snapshot, artifactListing, events) {
    const status = buildStatusViewModel(snapshot);
    const pending = buildPendingActionViewModel(snapshot);
    const artifacts = buildArtifactListViewModel(snapshot, artifactListing);
    const approval = buildApprovalViewModel(snapshot);
    const workers = snapshot && snapshot.workers ? snapshot.workers : {};
    const recentEvents = Array.isArray(events) ? events.slice(0, 5).map(formatEventSummary) : [];

    const whatHappened = buildWhatHappenedViewModel(snapshot, artifactListing, events);
    const latestError = buildLatestErrorViewModel(snapshot, events);

    return {
      runState: whatHappened.stateLabel,
      stopExplanation: whatHappened.stop.title,
      nextAction: whatHappened.stop.nextAction,
      stopSeverity: whatHappened.stop.severity,
      operatorMessage: status.operatorMessage,
      elapsed: status.elapsedLabel,
      progress: status.progressPercent === null ? "Unavailable" : `${status.progressPercent}%`,
      pendingAction: pending.summary,
      approval: approval.summary,
      workerSummary: Number.isInteger(workers.total)
        ? `total=${workers.total} | active=${Number(workers.active || 0)} | approval_required=${Number(workers.approval_required || 0)}`
        : "Unavailable",
      latestArtifact: artifacts.latestPath,
      latestError: latestError.present ? latestError.summary : "No latest error is available.",
      recentEvents: recentEvents.length > 0 ? recentEvents : ["No recent events received yet"],
      primaryActions: whatHappened.primaryActions,
    };
  }

  function buildWhatHappenedViewModel(snapshot, artifactListing, events) {
    const run = runSnapshot(snapshot);
    const plannerStatus = plannerStatusSnapshot(snapshot);
    const artifacts = buildArtifactListViewModel(snapshot, artifactListing);
    const recentEvents = Array.isArray(events) ? events.slice(0, 3).map(formatEventSummary) : [];
    const stop = translateStopReason(run && run.stop_reason);

    if (!run) {
      return {
        visible: true,
        stateLabel: "No run yet",
        completed: false,
        needsHuman: false,
        stop: {
          ...stop,
          title: "No run has been started for this repo yet.",
          nextAction: "Enter a goal and click Start Build.",
        },
        plannerMessage: "No planner message yet.",
        artifactSummary: artifacts.latestPath === "Unavailable" ? "No artifacts yet." : artifacts.latestPath,
        recentEvents: recentEvents.length > 0 ? recentEvents : ["No live activity yet."],
        primaryActions: [
          { id: "start_run", label: "Start Build" },
          { id: "open_live_output", label: "Open Live Output" },
        ],
      };
    }

    const needsHuman = hasApprovalRequired(snapshot) || hasAskHumanPending(snapshot);
    const modelInvalid = executorModelInvalid(snapshot);
    const codex = buildCodexReadinessViewModel(snapshot);
    const modelStop = modelInvalid
      ? {
        code: "executor_model_unavailable",
        title: codex.plainEnglish,
        detail: codex.lastError,
        nextAction: codex.recommendedAction,
        severity: "danger",
      }
      : stop;
    const primaryActions = [
      { id: "copy_debug_bundle", label: "Copy Debug Bundle" },
      { id: "open_live_output", label: "Open Live Output" },
    ];
    if (modelInvalid) {
      primaryActions.splice(1, 0, { id: "test_model_health", label: "Test Model Health" });
    }
    if (!run.completed && run.resumable !== false && !needsHuman) {
      primaryActions.push({ id: "continue_run", label: "Continue Build" });
    }
    if (artifacts.latestPath !== "Unavailable") {
      primaryActions.push({ id: "open_latest_artifact", label: "Open Latest Artifact" });
    }
    return {
      visible: Boolean(run.completed || run.stop_reason || needsHuman || modelInvalid),
      stateLabel: modelInvalid ? "Model error" : (run.completed ? "Completed" : (needsHuman ? "Needs you" : safeString(run.status || run.stop_reason, "Stopped"))),
      completed: Boolean(run.completed),
      needsHuman: needsHuman || modelInvalid,
      stop: modelStop,
      plannerMessage: plannerStatus.present
        ? safeString(plannerStatus.operator_message, "No planner message yet.")
        : "No planner message yet.",
      artifactSummary: artifacts.latestPath === "Unavailable" ? "No latest artifact is surfaced yet." : artifacts.latestPath,
      recentEvents: recentEvents.length > 0 ? recentEvents : ["No recent events received yet."],
      primaryActions,
    };
  }

  function buildLatestErrorViewModel(snapshot, events = []) {
    const run = runSnapshot(snapshot) || {};
    const health = modelHealthSnapshot(snapshot);
    const executor = health.executor || {};
    const executorVerifiedAfterRun = executorComponentVerified(executor) && !runFailureNewerThanComponent(run, executor);
    const candidates = [
      executor.last_error,
      executorVerifiedAfterRun ? "" : run.executor_last_error,
      run.last_error,
      health.last_error,
      snapshot && snapshot.error,
    ].filter((value) => safeString(value, "") !== "");
    const eventError = Array.isArray(events)
      ? events.find((event) => {
        const name = lower(event && event.event);
        const payload = event && event.payload ? event.payload : {};
        return name.includes("failed")
          || name.includes("fault")
          || name.includes("error")
          || modelUnavailableFromText(payload.error_message || payload.error || payload.message);
      })
      : null;
    if (eventError) {
      const payload = eventError.payload || {};
      candidates.push(payload.error_message || payload.error || payload.message || formatEventSummary(eventError));
    }
    const message = safeString(candidates.find((value) => safeString(value, "") !== ""), "");
    if (!message) {
      return {
        present: false,
        summary: "No latest error is available.",
        message: "",
        modelRelated: false,
      };
    }
    const modelRelated = modelUnavailableFromText(message) || executorModelInvalid(snapshot);
    return {
      present: true,
      summary: truncateText(message, 240),
      message: redactDiagnosticText(message),
      modelRelated,
      recommendedAction: modelRelated
        ? "Test model health, then change the configured Codex model if the probe fails."
        : "Open Live Output and copy the debug bundle for support.",
    };
  }

  function formatCheckpointForBundle(run) {
    const checkpoint = latestCheckpoint(run);
    if (!checkpoint || Object.keys(checkpoint).length === 0) {
      return "Unavailable";
    }
    return JSON.stringify({
      sequence: checkpoint.sequence || 0,
      stage: safeString(checkpoint.stage, ""),
      label: safeString(checkpoint.label, ""),
      safe_pause: checkpoint.safe_pause === true,
      planner_turn: checkpoint.planner_turn || 0,
      executor_turn: checkpoint.executor_turn || 0,
      created_at: safeString(checkpoint.created_at, ""),
    });
  }

  function buildDebugBundleText(snapshot, artifactListing, events = [], options = {}) {
    const status = buildStatusViewModel(snapshot);
    const topStatus = buildTopStatusViewModel(snapshot, options);
    const loopStatus = buildLoopStatusViewModel(snapshot, {
      latestEvent: Array.isArray(events) ? events[0] : null,
      lastUpdateLabel: options.lastUpdateLabel,
      expectedRepoPath: options.expectedRepoPath,
    });
    const runControl = buildRunControlStateViewModel(snapshot, {
      connected: true,
      goalEntered: true,
      launchInFlight: false,
      modelHealthChecking: false,
      expectedRepoPath: options.expectedRepoPath,
    });
    const progress = buildProgressPanelViewModel(snapshot);
    const pending = buildPendingActionViewModel(snapshot);
    const approval = buildApprovalViewModel(snapshot);
    const artifacts = buildArtifactListViewModel(snapshot, artifactListing);
    const codex = buildCodexReadinessViewModel(snapshot);
    const activity = buildActivityTimelineViewModel(events, { verbosity: "verbose" });
    const run = runSnapshot(snapshot) || {};
    const runtime = runtimeSnapshot(snapshot);
    const backend = snapshot && snapshot.backend ? snapshot.backend : {};
    const activeGuard = activeRunGuardSnapshot(snapshot);
    const stopFlag = buildSafeStopViewModel(snapshot);
    const repoBinding = repoBindingViewModel(snapshot, options);
    const health = modelHealthSnapshot(snapshot);
    const planner = health.planner || {};
    const executor = health.executor || {};
    const stop = translateStopReason(run.stop_reason);
    const latestError = buildLatestErrorViewModel(snapshot, events);
    const timeouts = snapshot && snapshot.timeouts ? snapshot.timeouts : {};
    const permissions = snapshot && snapshot.permissions ? snapshot.permissions : {};
    const buildTime = snapshot && snapshot.build_time ? snapshot.build_time : {};
    const updateStatus = snapshot && snapshot.update_status ? snapshot.update_status : {};
    const timeoutValue = (key) => {
      const entry = timeouts[key] || {};
      return safeString(entry.value, "Unavailable");
    };
    const artifactPaths = artifacts.items.length > 0
      ? artifacts.items.map((item) => `- ${item.path} (${item.category}, ${item.source}, ${item.at})`).join("\n")
      : `- Latest artifact: ${artifacts.latestPath}`;
    const recentEvents = activity.items.slice(0, 10).map((event) => `- ${event.timestampLabel} [${event.categoryLabel}] ${event.summary}`).join("\n")
      || "- No recent events available.";
    const rawEvents = Array.isArray(events) ? events : [];
    const latestStopFlagEvent = rawEvents.find((event) => lower(event && (event.event || event.type)) === "stop_flag_detected");
    const sideChatListing = options.sideChat || {};
    const sideChatItems = Array.isArray(sideChatListing.items) ? sideChatListing.items : [];
    const lastSideChatAction = sideChatItems.length > 0
      ? safeString(sideChatItems[0].created_at || sideChatItems[0].createdAt, "Unavailable")
      : "Unavailable";
    const gitStatus = snapshot && snapshot.git_status
      ? safeString(snapshot.git_status.summary || snapshot.git_status.status || JSON.stringify(snapshot.git_status), "Unavailable")
      : "Unavailable through current protocol.";

    const bundle = [
      "# Orchestrator V2 Run Debug Bundle",
      "",
      `Generated at: ${safeString(options.now || new Date().toISOString())}`,
      "Secrets/API keys/auth tokens are intentionally excluded. Full artifact contents and hidden chain-of-thought are not included.",
      "",
      "## Environment",
      `- Repo root: ${topStatus.repoRoot}`,
      `- Expected repo path: ${repoBinding.expected || "Unavailable"}`,
      `- Actual backend repo root: ${repoBinding.actual || "Unavailable"}`,
      `- Repo match: ${repoBinding.matches ? "yes" : "no"}`,
      `- Repo contract ready: ${repoContractReadinessViewModel(snapshot).ready ? "yes" : "no"}`,
      `- Repo contract missing: ${repoContractReadinessViewModel(snapshot).missing.join(", ") || "none"}`,
      repoBinding.mismatch ? "- Warning: GUI is connected to a backend serving the wrong repo. Do not Start/Continue until Backend for Target Repo is restarted." : "- Warning: none",
      `- Binary version: ${safeString(runtime.version || runtime.binary_version || runtime.build_version || (snapshot && snapshot.version), "Unavailable")}`,
      `- Backend PID/session: ${safeString(backend.pid, "Unavailable")} / ${safeString(backend.owner_session_id || backend.owner_sessionID, "Unavailable")}`,
      `- Backend control address: ${safeString(backend.control_address || options.address, "Unavailable")}`,
      `- Backend stale: ${backend.stale ? "yes" : "no"}`,
      `- Backend owner metadata: ${safeString(backend.owner_metadata || backend.owner, "Unavailable")}`,
      `- Planner model: configured=${safeString(planner.configured_model || status.plannerModelConfigured)} requested=${safeString(planner.requested_model, "Unavailable")} verified=${safeString(planner.verified_model || planner.verification_state, status.plannerModelVerification)}`,
      `- Codex model/access: model=${codex.model} effort=${codex.effort} full_access=${codex.fullAccessReady} verification=${codex.verificationState}`,
      `- Codex binary: ${codex.codexPath}`,
      `- Codex version: ${codex.codexVersion}`,
      `- Permission profile: ${safeString(permissions.profile, "Unavailable")}`,
      `- Total Build Time: ${safeString(buildTime.total_build_time_label, "Unavailable")}`,
      `- Current Step: ${safeString(buildTime.current_step_label, "Unavailable")}`,
      `- Current Step Time: ${safeString(buildTime.current_step_time_label, "Unavailable")}`,
      `- Executor turn timeout: ${timeoutValue("executor_turn_timeout")}`,
      `- Human wait timeout: ${timeoutValue("human_wait_timeout")}`,
      `- Install timeout: ${timeoutValue("install_timeout")}`,
      `- Update status: current=${safeString(updateStatus.current_version, "Unavailable")} latest=${safeString(updateStatus.latest_version, "Not checked")} available=${updateStatus.update_available ? "yes" : "no"}`,
      "",
      "## Run",
      `- Run id: ${status.runID}`,
      `- Goal: ${status.goal}`,
      `- Run status: ${safeString(run.status, "Unavailable")}`,
      `- Normalized loop state: ${loopStatus.state} (${loopStatus.label})`,
      `- Actively processing: ${hasActiveRunInProgress(snapshot) ? "true" : "false"}`,
      `- Waiting at safe point: ${waitingAtSafePoint(snapshot) ? "true" : "false"}`,
      `- Execute ready: ${executeReadyToDispatch(snapshot) ? "true" : "false"}`,
      `- Continue enabled: ${runControl.continueEnabled ? "true" : "false"}`,
      `- Continue disabled reason: ${runControl.continueDisabledReason || "None"}`,
      `- Completed: ${status.completed ? "true" : "false"}`,
      `- Resumable: ${run.resumable === false ? "false" : "true/unknown"}`,
      `- Started at: ${status.startedAt}`,
      `- Stopped/updated at: ${status.stoppedAt}`,
      `- Run Elapsed: ${status.elapsedLabel}`,
      "",
      "## Stop / Error",
      `- Stop reason code: ${stop.code}`,
      `- Plain-English stop reason: ${stop.title}`,
      `- Recommended next action: ${latestError.present ? latestError.recommendedAction : stop.nextAction}`,
      `- Stop flag present: ${stopFlag.flagPresent ? "yes" : "no"}`,
      `- Stop flag reason/source: ${stopFlag.present ? stopFlag.reason : "None"}`,
      `- Latest stop_flag_detected event: ${latestStopFlagEvent ? safeString(latestStopFlagEvent.at || latestStopFlagEvent.created_at || latestStopFlagEvent.timestamp, "available") : "Unavailable"}`,
      `- Stop source: ${stopFlag.present ? "backend stop flag file" : (stop.code === "operator_stop_requested" || stop.code === "operator_stop" ? "operator safe stop reason" : "unknown/unavailable")}`,
      `- Checkpoint: ${formatCheckpointForBundle(run)}`,
      `- Planner outcome: ${safeString(run.latest_planner_outcome || run.planner_outcome, "Unavailable")}`,
      `- Executor status: ${safeString(run.executor_turn_status || run.executor_status || run.executor_state || run.latest_executor_status, "Unavailable")}`,
      `- Executor thread id: ${safeString(run.executor_thread_id || approval.executorThreadID, "Unavailable")}`,
      `- Executor turn id: ${safeString(run.executor_turn_id || approval.executorTurnID, "Unavailable")}`,
      `- Last error: ${latestError.present ? latestError.message : "Unavailable"}`,
      `- Executor failure stage: ${status.executorFailureStage}`,
      "",
      "## Planner Operator Status",
      `- Operator message: ${status.operatorMessage}`,
      `- Progress: ${status.progressPercent === null ? "Unavailable" : `${status.progressPercent}%`} (${progress.progressConfidence} confidence)`,
      `- Progress basis: ${progress.progressBasis}`,
      `- Current focus: ${progress.currentFocus}`,
      `- Next intended step: ${progress.nextIntendedStep}`,
      `- Why this step: ${progress.whyThisStep}`,
      `- Roadmap alignment/context: ${progress.roadmapAlignmentText}`,
      "",
      "## Pending / Approval",
      `- Pending action: ${pending.present ? pending.summary : pending.message}`,
      `- Pending held: ${pending.held ? "true" : "false"}`,
      `- Approval/action required: ${approval.needsAttention ? approval.summary : "No action required."}`,
      `- Approval kind/state: ${approval.kind} / ${approval.state}`,
      `- Active-run guard present: ${activeGuard.present ? "yes" : "no"}`,
      `- Active-run guard stale: ${activeGuard.stale ? "yes" : "no"}`,
      `- Active-run guard run/session: ${safeString(activeGuard.run_id || activeGuard.runID, "Unavailable")} / ${safeString(activeGuard.session_id || activeGuard.sessionID, "Unavailable")}`,
      `- Active-run guard backend PID: ${safeString(activeGuard.backend_pid || activeGuard.backendPID, "Unavailable")}`,
      `- Active-run guard currently processing: ${activeGuard.currently_processing ? "true" : "false"}`,
      `- Active-run guard waiting at safe point: ${activeGuard.waiting_at_safe_point ? "true" : "false"}`,
      `- Active-run guard last progress at: ${safeString(activeGuard.last_progress_at, "Unavailable")}`,
      `- Recovery recommendation: ${activeGuard.stale ? "Use Recover Backend / Unlock Repo. It preserves run history and artifacts." : "No stale active-run recovery indicated."}`,
      `- Side Chat affects active run: no`,
      `- Side Chat queued control messages: no`,
      `- Last side-chat action timestamp: ${lastSideChatAction}`,
      "",
      "## Artifacts",
      artifactPaths,
      "",
      "## Recent Live Output",
      recentEvents,
      "",
      "## Git Status",
      `- ${gitStatus}`,
    ].join("\n");

    return redactDiagnosticText(bundle);
  }

  function buildModelHealthBundleText(snapshot, options = {}) {
    const runtime = runtimeSnapshot(snapshot);
    const backend = snapshot && snapshot.backend ? snapshot.backend : {};
    const health = modelHealthSnapshot(snapshot);
    const repoBinding = repoBindingViewModel(snapshot, options);
    const planner = health.planner || {};
    const executor = health.executor || {};
    const plannerVerified = plannerComponentVerified(planner);
    const executorVerified = executorComponentVerified(executor) && !runFailureNewerThanComponent(runSnapshot(snapshot), executor);
    const recommended = plannerVerified && executorVerified
      ? "No model-health action required. Planner and Codex requirements are verified for this control-server environment."
      : safeString(executor.recommended_action || planner.recommended_action || health.message, "Run model health checks and fix any reported model/access issue before unattended work.");
    const lines = [
      "# Orchestrator V2 Model Health",
      "",
      `Generated at: ${safeString(options.now || new Date().toISOString())}`,
      "Secrets/API keys/auth tokens/passwords are intentionally excluded.",
      "",
      "## Backend",
      `- Repo root: ${safeString(runtime.repo_root || backend.repo_root, "Unavailable")}`,
      `- Expected repo path: ${repoBinding.expected || "Unavailable"}`,
      `- Repo match: ${repoBinding.matches ? "yes" : "no"}`,
      `- Control server address: ${safeString(options.address, "Unavailable")}`,
      `- Backend PID: ${safeString(backend.pid, "Unavailable")}`,
      `- Backend started at: ${safeString(backend.started_at, "Unavailable")}`,
      `- Backend binary path: ${safeString(backend.binary_path, "Unavailable")}`,
      `- Backend binary modified at: ${safeString(backend.binary_modified_at, "Unavailable")}`,
      `- Backend version: ${safeString(backend.binary_version || runtime.version, "Unavailable")}`,
      `- Backend revision: ${safeString(backend.binary_revision, "Unavailable")}`,
      `- Backend build time: ${safeString(backend.binary_build_time, "Unavailable")}`,
      `- Backend stale: ${backend.stale ? "yes" : "no"}`,
      `- Backend stale reason: ${safeString(backend.stale_reason, "None")}`,
      "",
      "## Planner",
      `- Configured model: ${safeString(planner.configured_model, "Unavailable")}`,
      `- Requested model: ${safeString(planner.requested_model, "Unavailable")}`,
      `- Resolved model: ${safeString(planner.resolved_model, "Unavailable")}`,
      `- Verified model: ${safeString(planner.verified_model, "Unavailable")}`,
      `- Verification state: ${safeString(planner.verification_state, "not_checked")}`,
      `- Last tested at: ${safeString(planner.last_tested_at, "Not checked yet")}`,
      `- Last error: ${safeString(planner.last_error, "None")}`,
      "",
      "## Codex Executor",
      `- Configured model: ${safeString(executor.configured_model || executor.codex_model_configured, "Unavailable")}`,
      `- Requested model: ${safeString(executor.requested_model, "Unavailable")}`,
      `- Verified model: ${safeString(executor.verified_model, "Unavailable")}`,
      `- Verification state: ${safeString(executor.verification_state, "not_checked")}`,
      `- Last tested at: ${safeString(executor.last_tested_at, "Not checked yet")}`,
      `- Codex model verified: ${executor.codex_model_verified ? "yes" : "no"}`,
      `- Codex full access verified: ${executor.codex_permission_mode_verified ? "yes" : "no"}`,
      `- Access mode: ${safeString(executor.access_mode, "Unavailable")}`,
      `- Effort: ${safeString(executor.effort, "Unavailable")}`,
      `- Codex binary path: ${safeString(executor.codex_executable_path, "Unavailable")}`,
      `- Codex version: ${safeString(executor.codex_version, "Unavailable")}`,
      `- Codex config source: ${safeString(executor.codex_config_source, "Unavailable")}`,
      `- Codex last error: ${safeString(executor.last_error || executor.codex_last_probe_error, "None")}`,
      "",
      "## Overall",
      `- Planner verified: ${plannerVerified ? "yes" : "no"}`,
      `- Codex verified: ${executorVerified ? "yes" : "no"}`,
      `- Needs attention: ${health.needs_attention ? "yes" : "no"}`,
      `- Blocking: ${health.blocking ? "yes" : "no"}`,
      `- Message: ${safeString(health.message, "Unavailable")}`,
      `- Recommended action: ${recommended}`,
    ];
    return redactDiagnosticText(lines.join("\n"));
  }

  function buildCodexReadinessViewModel(snapshot) {
    const runtime = runtimeSnapshot(snapshot);
    const hasRuntime = Boolean(snapshot && snapshot.runtime);
    const executorReady = Boolean(runtime.executor_ready);
    const modelHealth = modelHealthSnapshot(snapshot);
    const executor = modelHealth.executor || {};
    const invalid = executorModelInvalid(snapshot);
    const verified = lower(executor.verification_state) === "verified";
    const permissionVerified = Boolean(executor.codex_permission_mode_verified);
    const title = invalid
      ? "Codex Model: Unavailable"
      : verified && permissionVerified
        ? "Codex Full Access: Verified"
        : executorReady
          ? "Codex App-Server: Reachable, Probe Needed"
        : (hasRuntime ? "Codex App-Server: Not Ready" : "Codex App-Server: Not Verified");
    return {
      executorReady,
      title,
      accessMode: safeString(executor.access_mode, executorReady
        ? "Required executor path is gpt-5.5 with danger-full-access and approval never; use Test Codex Config to verify it in this environment."
        : "Not verified. The engine does not currently report executor readiness."),
      model: safeString(executor.requested_model || executor.configured_model, "Not verified by the shell protocol yet."),
      effort: safeString(executor.effort, "Not verified by the shell protocol yet."),
      fullAccessReady: permissionVerified ? "Yes" : (hasRuntime ? "Not verified" : "Not verified"),
      verificationState: safeString(executor.verification_state, "not_verified"),
      lastError: safeString(executor.last_error, "None"),
      codexPath: safeString(executor.codex_executable_path, "Not detected"),
      codexVersion: safeString(executor.codex_version, "Not detected"),
      codexConfigSource: safeString(executor.codex_config_source, "Not detected"),
      codexModelVerified: Boolean(executor.codex_model_verified),
      codexPermissionModeVerified: permissionVerified,
      modelInvalid: invalid,
      needsAttention: invalid || (hasRuntime && (!executorReady || !verified || !permissionVerified)),
      plainEnglish: safeString(executor.plain_english, invalid
        ? "Codex could not start because the configured model is unavailable to this account. No code changes were made."
        : "Codex model and full autonomous access are not verified yet."),
      recommendedAction: invalid
        ? "Change or test the configured Codex model. No silent fallback will be used."
        : safeString(executor.recommended_action, executorReady
          ? "Use Test Codex Config before a serious autonomous build, especially after updating Codex."
          : "Run `orchestrator doctor` and confirm Codex is installed, signed in, and available on PATH. Do not paste secrets into the shell."),
    };
  }

  function buildVerbosityViewModel(value) {
    const normalized = lower(value) || "normal";
    const descriptions = {
      quiet: "Major state changes, blockers, approvals, and failures only.",
      normal: "Readable progress updates without raw protocol payloads.",
      verbose: "Planner/executor focus, artifact, worker, and command summaries.",
      trace: "Raw safe event payloads and technical details.",
    };
    return {
      value: descriptions[normalized] ? normalized : "normal",
      label: {
        quiet: "Quiet",
        normal: "Normal",
        verbose: "Verbose",
        trace: "Trace",
      }[descriptions[normalized] ? normalized : "normal"],
      description: descriptions[descriptions[normalized] ? normalized : "normal"],
    };
  }

  function buildSideChatViewModel(sideChatListing) {
    const listing = sideChatListing || {};
    const items = Array.isArray(listing.items) ? listing.items : [];

    return {
      available: listing.available !== false,
      nonInterfering: true,
      modeLabel: "Side Chat - context assistant",
      modeDescription: "Answers from observable runtime context. Side Chat only queues planner-visible notes, requests Safe Stop, or changes run control when you press an explicit audited action.",
      inputLabel: "Ask Side Chat",
      buttonLabel: "Ask Side Chat",
      count: Number.isInteger(listing.count) ? listing.count : items.length,
      message: items.length === 0
        ? safeString(listing.message, "No side-chat conversation recorded yet. Use Control Chat or Action Required when you want to affect the active run.")
        : safeString(listing.message, ""),
      items: items.map((item) => ({
        id: safeString(item.id),
        rawText: safeString(item.raw_text),
        source: safeString(item.source, "side_chat"),
        status: safeString(item.status, "recorded"),
        backendState: safeString(item.backend_state, "context_agent"),
        responseMessage: safeString(item.response_message, "Side Chat has no reply recorded for this message."),
        createdAt: safeString(item.created_at, "Unavailable"),
        runID: safeString(item.run_id, "Unavailable"),
        contextPolicy: safeString(item.context_policy, "Unavailable"),
      })),
    };
  }

  function buildWorkerPanelViewModel(workerListing, selectedWorkerID = "") {
    const listing = workerListing || {};
    const items = Array.isArray(listing.items) ? listing.items : [];
    const countsByStatus = listing.counts_by_status || {};
    const normalizedItems = items.map((item) => ({
      workerID: safeString(item.worker_id),
      workerName: safeString(item.worker_name),
      status: safeString(item.status),
      scope: safeString(item.scope, "Unavailable"),
      worktreePath: safeString(item.worktree_path, "Unavailable"),
      approvalRequired: Boolean(item.approval_required),
      approvalKind: safeString(item.approval_kind, "Unavailable"),
      approvalPreview: safeString(item.approval_preview, "Unavailable"),
      executorThreadID: safeString(item.executor_thread_id, "Unavailable"),
      executorTurnID: safeString(item.executor_turn_id, "Unavailable"),
      interruptible: Boolean(item.interruptible),
      steerable: Boolean(item.steerable),
      lastControlAction: safeString(item.last_control_action, "Unavailable"),
      workerTaskSummary: safeString(item.worker_task_summary, "Unavailable"),
      workerResultSummary: safeString(item.worker_result_summary, "Unavailable"),
      workerErrorSummary: safeString(item.worker_error_summary, "Unavailable"),
      updatedAt: safeString(item.updated_at, "Unavailable"),
      selected: safeString(item.worker_id) === String(selectedWorkerID || "").trim(),
    }));
    const selectedWorker = normalizedItems.find((item) => item.selected) || normalizedItems[0] || null;

    return {
      count: Number.isInteger(listing.count) ? listing.count : items.length,
      message: normalizedItems.length === 0 ? safeString(listing.message, "No workers exist for the current run yet. Workers appear after the planner creates or dispatches them, or after you create one through this panel.") : "",
      countsByStatus: {
        creating: Number.isInteger(countsByStatus.creating) ? countsByStatus.creating : 0,
        pending: Number.isInteger(countsByStatus.pending) ? countsByStatus.pending : 0,
        assigned: Number.isInteger(countsByStatus.assigned) ? countsByStatus.assigned : 0,
        executor_active: Number.isInteger(countsByStatus.executor_active) ? countsByStatus.executor_active : 0,
        approval_required: Number.isInteger(countsByStatus.approval_required) ? countsByStatus.approval_required : 0,
        idle: Number.isInteger(countsByStatus.idle) ? countsByStatus.idle : 0,
        completed: Number.isInteger(countsByStatus.completed) ? countsByStatus.completed : 0,
        failed: Number.isInteger(countsByStatus.failed) ? countsByStatus.failed : 0,
      },
      items: normalizedItems,
      selectedWorkerID: selectedWorker ? selectedWorker.workerID : "",
      selectedWorker,
    };
  }

  function buildAutofillViewModel(result) {
    const snapshot = result || {};
    const files = Array.isArray(snapshot.files) ? snapshot.files : [];

    return {
      available: snapshot.available !== false,
      message: safeString(snapshot.message, "No autofill draft generated yet."),
      model: safeString(snapshot.model, "Unavailable"),
      generatedAt: safeString(snapshot.generated_at, "Unavailable"),
      responseID: safeString(snapshot.response_id, "Unavailable"),
      files: files.map((file) => ({
        path: safeString(file.path),
        summary: safeString(file.summary, "No summary"),
        content: safeString(file.content, ""),
        existing: Boolean(file.existing),
        existingMTime: safeString(file.existing_mtime, "Unavailable"),
      })),
    };
  }

  function buildRepoTreeViewModel(treeListing, openFile) {
    const listing = treeListing || {};
    const items = Array.isArray(listing.items) ? listing.items : [];
    const file = openFile || null;

    return {
      path: safeString(listing.path, "repo root"),
      parentPath: String(listing.parent_path || "").trim(),
      count: Number.isInteger(listing.count) ? listing.count : items.length,
      message: items.length === 0 ? safeString(listing.message, "Repo tree is empty because the shell is disconnected or no repo root is loaded.") : "",
      items: items.map((item) => ({
        name: safeString(item.name),
        path: safeString(item.path),
        kind: safeString(item.kind, "file"),
        readOnly: item.read_only !== false,
        editableViaContractEditor: Boolean(item.editable_via_contract_editor),
        byteSize: Number.isFinite(item.byte_size) ? item.byte_size : 0,
        modifiedAt: safeString(item.modified_at, "Unavailable"),
      })),
      openFile: file ? {
        available: file.available !== false,
        path: safeString(file.path),
        contentType: safeString(file.content_type, "text/plain"),
        content: safeString(file.content, ""),
        byteSize: Number.isFinite(file.byte_size) ? file.byte_size : 0,
        truncated: Boolean(file.truncated),
        readOnly: file.read_only !== false,
        editableViaContractEditor: Boolean(file.editable_via_contract_editor),
        message: safeString(file.message, file.available === false ? "Repo file unavailable." : ""),
      } : null,
    };
  }

  function buildDogfoodIssuesViewModel(issueListing, selectedIssueID = "") {
    const listing = issueListing || {};
    const items = Array.isArray(listing.items) ? listing.items : [];
    const normalizedItems = items.map((item) => ({
      id: safeString(item.id),
      repoPath: safeString(item.repo_path, "Unavailable"),
      runID: safeString(item.run_id, "Unavailable"),
      source: safeString(item.source, "operator_shell"),
      title: safeString(item.title, "Untitled"),
      note: safeString(item.note, ""),
      createdAt: safeString(item.created_at, "Unavailable"),
      updatedAt: safeString(item.updated_at, "Unavailable"),
      selected: safeString(item.id) === String(selectedIssueID || "").trim(),
    }));
    const selectedIssue = normalizedItems.find((item) => item.selected) || normalizedItems[0] || null;

    return {
      available: listing.available !== false,
      count: Number.isInteger(listing.count) ? listing.count : normalizedItems.length,
      message: normalizedItems.length === 0
        ? safeString(listing.message, "No dogfood notes captured yet.")
        : safeString(listing.message, ""),
      items: normalizedItems,
      selectedIssueID: selectedIssue ? selectedIssue.id : "",
      selectedIssue,
    };
  }

  return {
    buildConnectionStatusViewModel,
    buildRepoBindingViewModel: repoBindingViewModel,
    buildLoopStatusViewModel,
    buildVerbosityViewModel,
    buildRecommendedActionViewModel,
    buildRunControlStateViewModel,
    buildTopStatusViewModel,
    buildHomeDashboardViewModel,
    buildContractStatusViewModel,
    translateStopReason,
    buildStatusViewModel,
    buildProgressPanelViewModel,
    buildActivityTimelineViewModel,
    buildTerminalTabsViewModel,
    classifyActivityCategory,
    formatEventTimestamp,
    formatEventSummary,
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
    buildWorkerPanelViewModel,
    buildAutofillViewModel,
    buildRepoTreeViewModel,
    buildDogfoodIssuesViewModel,
  };
});
