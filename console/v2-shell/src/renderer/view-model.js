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

  function lower(value) {
    return safeString(value, "").toLowerCase();
  }

  function runtimeSnapshot(snapshot) {
    return snapshot && snapshot.runtime ? snapshot.runtime : {};
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

  function hasApprovalRequired(snapshot) {
    const approval = approvalSnapshot(snapshot);
    const workers = snapshot && snapshot.workers ? snapshot.workers : {};
    const approvalState = lower(approval.state);
    const workerApprovalRequired = Number.isInteger(approval.worker_approval_required)
      ? approval.worker_approval_required
      : (Number.isInteger(workers.approval_required) ? workers.approval_required : 0);
    return Boolean(approval.present)
      || approvalState.includes("required")
      || workerApprovalRequired > 0;
  }

  function hasAskHumanPending(snapshot) {
    const run = runSnapshot(snapshot);
    if (!run) {
      return false;
    }
    const stopReason = lower(run.stop_reason);
    const nextAction = lower(run.next_operator_action);
    const plannerOutcome = lower(run.latest_planner_outcome);
    return stopReason.includes("ask_human")
      || nextAction.includes("ask_human")
      || nextAction.includes("answer")
      || plannerOutcome.includes("ask_human");
  }

  function hasActiveRunInProgress(snapshot) {
    const run = runSnapshot(snapshot);
    if (!run || run.completed) {
      return false;
    }
    const status = lower(run.status);
    const stopReason = lower(run.stop_reason);
    const nextAction = lower(run.next_operator_action);
    return status.includes("running")
      || status.includes("active")
      || status.includes("in_progress")
      || nextAction.includes("watch")
      || stopReason.includes("executor_in_progress")
      || stopReason.includes("planner_in_progress");
  }

  function modelHealthSnapshot(snapshot) {
    return snapshot && snapshot.model_health ? snapshot.model_health : {};
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

    const checkpoint = run.latest_checkpoint || {};
    const stop = translateStopReason(run.stop_reason);
    const stage = safeString(checkpoint.stage || checkpoint.label || run.latest_planner_outcome, "Unavailable");
    const turn = checkpoint.sequence ? `checkpoint ${checkpoint.sequence}` : safeString(run.latest_planner_outcome, "Unavailable");

    if (executorModelInvalid(snapshot) || hasApprovalRequired(snapshot) || hasAskHumanPending(snapshot)) {
      return {
        state: "needs_you",
        label: executorModelInvalid(snapshot) ? "Loop Status: Error" : "Loop Status: Needs You",
        className: executorModelInvalid(snapshot) ? "loop-error" : "loop-attention",
        detail: executorModelInvalid(snapshot)
          ? "Codex could not start because the configured model is unavailable or invalid."
          : hasApprovalRequired(snapshot)
          ? "The run is waiting for approval or worker attention."
          : "The planner needs a human answer before it can continue.",
        stage,
        turn,
        lastUpdate,
        latestActivity: safeString(latestEvent && (latestEvent.summary || latestEvent.event), executorModelInvalid(snapshot) ? "Configured Codex model is unavailable." : stop.title),
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
        detail: "The run appears active. Watch progress or request Safe Stop if you want a clean pause.",
        stage,
        turn,
        lastUpdate,
        latestActivity: safeString(latestEvent && (latestEvent.summary || latestEvent.event), "Run activity is in progress."),
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

    if (hasAskHumanPending(snapshot)) {
      return {
        state: "ask_human",
        title: "Answer the planner question.",
        detail: "The latest run is waiting for human input. Use Control Chat so the raw message reaches the planner at the next safe point.",
        primaryAction: {
          id: "open_control_chat",
          label: "Open Control Chat",
          enabled: true,
          kind: "scroll",
          target: "chat",
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

    return {
      state: "refresh_needed",
      title: "Update the dashboard.",
      detail: "The shell is connected, but the latest state does not clearly indicate a next operator action. Refresh the snapshot before acting.",
      primaryAction: {
        id: "refresh_status",
        label: "Update Dashboard",
        enabled: true,
        kind: "protocol",
      },
    };
  }

  function buildTopStatusViewModel(snapshot, options = {}) {
    const connection = options.connection || {};
    const runtime = runtimeSnapshot(snapshot);
    const run = runSnapshot(snapshot);
    const approval = approvalSnapshot(snapshot);
    const pending = pendingActionSnapshot(snapshot);
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
    });
    const stop = translateStopReason(run && run.stop_reason);
    const modelHealth = modelHealthSnapshot(snapshot);
    const blocker = executorModelInvalid(snapshot)
      ? safeString(modelHealth.message, "Configured Codex model is unavailable.")
      : hasApprovalRequired(snapshot)
      ? safeString(approval.summary || approval.message || approval.state, "Approval required")
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
      repoRoot: safeString(runtime.repo_root, "No repo loaded from control server yet"),
      repoReady: Boolean(runtime.repo_ready),
      runID: run ? safeString(run.id) : "No active run",
      runState: runStateLabel(run),
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
    const status = buildStatusViewModel(snapshot);
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
        ready: topStatus.repoReady,
        message: topStatus.repoReady
          ? "Repo contract markers look ready from the latest status snapshot."
          : "Repo contract markers are missing or not loaded yet. Refresh status or open the contract files.",
      },
      latestPlannerMessage: status.operatorMessage,
      latestArtifactPath: artifacts.latestPath,
      recentActivity: recentEvents,
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

    return {
      operatorMessage: plannerStatus.present ? safeString(plannerStatus.operator_message, "No operator message yet") : "No operator message yet",
      progressPercent,
      progressBarWidth: progressPercent === null ? "0%" : `${progressPercent}%`,
      progressConfidence: plannerStatus.present ? safeString(plannerStatus.progress_confidence, "Unavailable") : "Unavailable",
      progressBasis: plannerStatus.present ? safeString(plannerStatus.progress_basis, "Unavailable") : "Unavailable",
      currentFocus: plannerStatus.present ? safeString(plannerStatus.current_focus, "Unavailable") : "Unavailable",
      nextIntendedStep: plannerStatus.present ? safeString(plannerStatus.next_intended_step, "Unavailable") : "Unavailable",
      whyThisStep: plannerStatus.present ? safeString(plannerStatus.why_this_step, "Unavailable") : "Unavailable",
      roadmapPresent: Boolean(roadmap.present),
      roadmapPath: safeString(roadmap.path, ".orchestrator/roadmap.md"),
      roadmapAlignmentText: safeString(roadmap.alignment_text || roadmap.preview, roadmap.message || "No roadmap context available yet."),
      roadmapModifiedAt: safeString(roadmap.modified_at, "Unavailable"),
      roadmapMessage: safeString(roadmap.message, ""),
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
      case "planner_turn_started":
        return `Planner is choosing the next step${runID}.`;
      case "planner_turn_completed":
        return `Planner finished choosing the next step${runID}.`;
      case "executor_turn_started":
        return `Codex started an implementation turn${runID}.`;
      case "executor_turn_completed":
        return `Codex completed an implementation turn${runID}.`;
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
      case "control_message_queued":
        return `Your control message was queued${runID}.`;
      case "control_message_consumed":
        return `Planner received your control message${runID}.`;
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
      || name.startsWith("status_")
      || name.startsWith("safe_point_")
      || name.startsWith("verbosity_")
    ) {
      return "status";
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
      case "approval":
        return "Approval";
      case "intervention":
        return "Intervention";
      case "fault":
        return "Fault";
      case "terminal":
        return "Terminal";
      case "status":
        return "Status";
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
        return "info";
      case "terminal":
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

    return {
      totalCount: normalized.length,
      filteredCount: filtered.length,
      currentRunOnly,
      currentRunID: currentRunID || "Unavailable",
      searchText,
      verbosity,
      items: filtered,
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
    const workers = snapshot && snapshot.workers ? snapshot.workers : {};
    const workerApprovalRequired = Number.isInteger(approval.worker_approval_required)
      ? approval.worker_approval_required
      : (Number.isInteger(workers.approval_required) ? workers.approval_required : 0);
    const defaultSummary = workerApprovalRequired > 0
      ? `${workerApprovalRequired} worker approval(s) pending. Worker-specific shell controls are still deferred in this slice.`
      : "No approval is currently required";
    const present = Boolean(approval.present);
    const needsAttention = present || workerApprovalRequired > 0;
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
      present,
      needsAttention,
      badgeCount: (present ? 1 : 0) + workerApprovalRequired,
      state: safeString(approval.state, "none"),
      kind,
      summary: safeString(approval.summary || approval.message, defaultSummary),
      runID: safeString(approval.run_id, "Unavailable"),
      executorTurnID: safeString(approval.executor_turn_id, "Unavailable"),
      executorThreadID: safeString(approval.executor_thread_id, "Unavailable"),
      reason: safeString(approval.reason, "Unavailable"),
      command: safeString(approval.command, "Unavailable"),
      cwd: safeString(approval.cwd, "Unavailable"),
      grantRoot: safeString(approval.grant_root, "Unavailable"),
      lastControlAction: safeString(approval.last_control_action, "Unavailable"),
      workerApprovalRequired,
      availableActions: Array.isArray(approval.available_actions) ? approval.available_actions : [],
      message: safeString(approval.message, defaultSummary),
      plainEnglish: needsAttention
        ? {
          title: "Action Required",
          requested: present ? approvalType : `${workerApprovalRequired} worker approval(s) need attention.`,
          why: present
            ? safeString(approval.reason, "Codex paused because the engine reported an approval-required state.")
            : "A worker is waiting for a worker-specific approval. The shell shows this truthfully, but worker approval controls remain deferred.",
          approveEffect: present
            ? "Approve tells Codex it may continue with this specific request."
            : "Use the headless worker approval command for worker-specific approval in this slice.",
          denyEffect: present
            ? "Deny records the denial and lets the planner/runtime handle the next safe step."
            : "Use the headless worker denial command for worker-specific denial in this slice.",
          scope: command || safeString(approval.cwd || approval.grant_root, "Scope details are not available in the latest snapshot."),
        }
        : {
          title: "No Action Required",
          requested: "Nothing is waiting for approval right now.",
          why: "The latest status snapshot does not show primary executor approval or worker approvals.",
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

    return {
      runState: whatHappened.stateLabel,
      stopExplanation: whatHappened.stop.title,
      nextAction: whatHappened.stop.nextAction,
      operatorMessage: status.operatorMessage,
      elapsed: status.elapsedLabel,
      progress: status.progressPercent === null ? "Unavailable" : `${status.progressPercent}%`,
      pendingAction: pending.summary,
      approval: approval.summary,
      workerSummary: Number.isInteger(workers.total)
        ? `total=${workers.total} | active=${Number(workers.active || 0)} | approval_required=${Number(workers.approval_required || 0)}`
        : "Unavailable",
      latestArtifact: artifacts.latestPath,
      recentEvents: recentEvents.length > 0 ? recentEvents : ["No recent events received yet"],
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
    };
  }

  function buildCodexReadinessViewModel(snapshot) {
    const runtime = runtimeSnapshot(snapshot);
    const hasRuntime = Boolean(snapshot && snapshot.runtime);
    const executorReady = Boolean(runtime.executor_ready);
    const modelHealth = modelHealthSnapshot(snapshot);
    const executor = modelHealth.executor || {};
    const invalid = executorModelInvalid(snapshot);
    const title = invalid
      ? "Codex Model: Unavailable"
      : executorReady
        ? "Codex App-Server: Reachable"
        : (hasRuntime ? "Codex App-Server: Not Ready" : "Codex App-Server: Not Verified");
    return {
      executorReady,
      title,
      accessMode: safeString(executor.access_mode, executorReady
        ? "Workspace-write execution path is configured by the engine; full autonomous access is not separately verified by the shell yet."
        : "Not verified. The engine does not currently report executor readiness."),
      model: safeString(executor.requested_model || executor.configured_model, "Not verified by the shell protocol yet."),
      effort: safeString(executor.effort, "Not verified by the shell protocol yet."),
      fullAccessReady: executorReady ? "Not verified" : (hasRuntime ? "No" : "Not verified"),
      verificationState: safeString(executor.verification_state, "not_verified"),
      lastError: safeString(executor.last_error, "None"),
      modelInvalid: invalid,
      needsAttention: invalid || (hasRuntime && !executorReady),
      plainEnglish: safeString(executor.plain_english, invalid
        ? "Codex could not start because the configured model is unavailable to this account. No code changes were made."
        : "Codex model identity is externally managed and not verified yet."),
      recommendedAction: invalid
        ? "Change or test the configured Codex model. No silent fallback will be used."
        : safeString(executor.recommended_action, executorReady
          ? "Run `orchestrator doctor` if you need a deeper Codex model/access check before a long autonomous build."
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
      count: Number.isInteger(listing.count) ? listing.count : items.length,
      message: items.length === 0
        ? safeString(listing.message, "No side chat messages recorded yet.")
        : safeString(listing.message, ""),
      items: items.map((item) => ({
        id: safeString(item.id),
        rawText: safeString(item.raw_text),
        source: safeString(item.source, "side_chat"),
        status: safeString(item.status, "recorded"),
        backendState: safeString(item.backend_state, "unavailable"),
        responseMessage: safeString(item.response_message, "Backend reply unavailable in this slice."),
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
    buildLoopStatusViewModel,
    buildVerbosityViewModel,
    buildRecommendedActionViewModel,
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
    buildArtifactListViewModel,
    buildApprovalViewModel,
    buildRunSummaryViewModel,
    buildWhatHappenedViewModel,
    buildCodexReadinessViewModel,
    modelUnavailableFromText,
    buildSideChatViewModel,
    buildWorkerPanelViewModel,
    buildAutofillViewModel,
    buildRepoTreeViewModel,
    buildDogfoodIssuesViewModel,
  };
});
