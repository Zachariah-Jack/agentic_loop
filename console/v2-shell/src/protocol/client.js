const defaultBaseURL = "http://127.0.0.1:44777";

function makeProtocolError(action, message, extra = {}) {
  const error = new Error(message);
  error.name = "ProtocolError";
  error.action = action;
  if (extra.code) {
    error.code = extra.code;
  }
  if (Number.isFinite(extra.status)) {
    error.status = extra.status;
  }
  if (extra.address) {
    error.address = extra.address;
  }
  return error;
}

function normalizeControlBaseURL(baseURL) {
  const trimmed = String(baseURL || "").trim();
  if (trimmed === "") {
    return defaultBaseURL;
  }
  if (trimmed.startsWith("http://") || trimmed.startsWith("https://")) {
    return trimmed.replace(/\/+$/, "");
  }
  return `http://${trimmed.replace(/\/+$/, "")}`;
}

async function callControlAction(baseURL, action, payload, options = {}) {
  const fetchImpl = options.fetchImpl || fetch;
  const address = normalizeControlBaseURL(baseURL);
  let response;
  try {
    response = await fetchImpl(`${address}/v2/control`, {
      method: "POST",
      headers: {
        "content-type": "application/json",
      },
      body: JSON.stringify({
        id: `shell_${Date.now()}`,
        type: "request",
        action,
        payload: payload || {},
      }),
    });
  } catch (error) {
    throw makeProtocolError(action, `Unable to reach control server at ${address}`, {
      address,
      code: "network_unreachable",
    });
  }

  let envelope;
  try {
    envelope = await response.json();
  } catch (_error) {
    throw makeProtocolError(action, `Control server at ${address} returned an unreadable response`, {
      address,
      status: response.status,
      code: "invalid_response",
    });
  }
  if (!response.ok || !envelope.ok) {
    const message = envelope && envelope.error && envelope.error.message
      ? envelope.error.message
      : `control action ${action} failed`;
    throw makeProtocolError(action, message, {
      address,
      status: response.status,
      code: envelope && envelope.error ? envelope.error.code : "protocol_error",
    });
  }
  return envelope.payload;
}

async function getStatusSnapshot(baseURL, runID = "", options = {}) {
  return callControlAction(baseURL, "get_status_snapshot", { run_id: runID }, options);
}

async function startRun(baseURL, payload = {}, options = {}) {
  return callControlAction(baseURL, "start_run", payload, options);
}

async function continueRun(baseURL, payload = {}, options = {}) {
  return callControlAction(baseURL, "continue_run", payload, options);
}

async function getActiveRunGuard(baseURL, options = {}) {
  return callControlAction(baseURL, "get_active_run_guard", {}, options);
}

async function recoverStaleRun(baseURL, payload = {}, options = {}) {
  return callControlAction(baseURL, "recover_stale_run", payload, options);
}

async function testPlannerModel(baseURL, payload = {}, options = {}) {
  return callControlAction(baseURL, "test_planner_model", payload, options);
}

async function testExecutorModel(baseURL, payload = {}, options = {}) {
  return callControlAction(baseURL, "test_executor_model", payload, options);
}

async function approveExecutor(baseURL, runID = "", options = {}) {
  return callControlAction(baseURL, "approve_executor", { run_id: runID }, options);
}

async function denyExecutor(baseURL, runID = "", options = {}) {
  return callControlAction(baseURL, "deny_executor", { run_id: runID }, options);
}

async function listRecentArtifacts(baseURL, payload = {}, options = {}) {
  return callControlAction(baseURL, "list_recent_artifacts", payload, options);
}

async function getArtifact(baseURL, artifactPath, options = {}) {
  return callControlAction(baseURL, "get_artifact", { artifact_path: artifactPath }, options);
}

async function listContractFiles(baseURL, repoPath = "", options = {}) {
  return callControlAction(baseURL, "list_contract_files", { repo_path: repoPath }, options);
}

async function openContractFile(baseURL, payload, options = {}) {
  return callControlAction(baseURL, "open_contract_file", payload, options);
}

async function saveContractFile(baseURL, payload, options = {}) {
  return callControlAction(baseURL, "save_contract_file", payload, options);
}

async function runAIAutofill(baseURL, payload, options = {}) {
  return callControlAction(baseURL, "run_ai_autofill", payload, options);
}

async function listRepoTree(baseURL, payload = {}, options = {}) {
  return callControlAction(baseURL, "list_repo_tree", payload, options);
}

async function openRepoFile(baseURL, payload, options = {}) {
  return callControlAction(baseURL, "open_repo_file", payload, options);
}

async function injectControlMessage(baseURL, payload, options = {}) {
  return callControlAction(baseURL, "inject_control_message", payload, options);
}

async function sendSideChatMessage(baseURL, payload, options = {}) {
  return callControlAction(baseURL, "send_side_chat_message", payload, options);
}

async function listSideChatMessages(baseURL, payload = {}, options = {}) {
  return callControlAction(baseURL, "list_side_chat_messages", payload, options);
}

async function sideChatContextSnapshot(baseURL, payload = {}, options = {}) {
  return callControlAction(baseURL, "side_chat_context_snapshot", payload, options);
}

async function sideChatActionRequest(baseURL, payload = {}, options = {}) {
  return callControlAction(baseURL, "side_chat_action_request", payload, options);
}

async function captureDogfoodIssue(baseURL, payload, options = {}) {
  return callControlAction(baseURL, "capture_dogfood_issue", payload, options);
}

async function listDogfoodIssues(baseURL, payload = {}, options = {}) {
  return callControlAction(baseURL, "list_dogfood_issues", payload, options);
}

async function listWorkers(baseURL, payload = {}, options = {}) {
  return callControlAction(baseURL, "list_workers", payload, options);
}

async function createWorker(baseURL, payload, options = {}) {
  return callControlAction(baseURL, "create_worker", payload, options);
}

async function dispatchWorker(baseURL, payload, options = {}) {
  return callControlAction(baseURL, "dispatch_worker", payload, options);
}

async function removeWorker(baseURL, payload, options = {}) {
  return callControlAction(baseURL, "remove_worker", payload, options);
}

async function integrateWorkers(baseURL, payload, options = {}) {
  return callControlAction(baseURL, "integrate_workers", payload, options);
}

async function getRuntimeConfig(baseURL, options = {}) {
  return callControlAction(baseURL, "get_runtime_config", {}, options);
}

async function setRuntimeConfig(baseURL, payload, options = {}) {
  return callControlAction(baseURL, "set_runtime_config", payload, options);
}

async function checkForUpdates(baseURL, payload = {}, options = {}) {
  return callControlAction(baseURL, "check_for_updates", payload, options);
}

async function getUpdateStatus(baseURL, options = {}) {
  return callControlAction(baseURL, "get_update_status", {}, options);
}

async function installUpdate(baseURL, payload = {}, options = {}) {
  return callControlAction(baseURL, "install_update", payload, options);
}

async function skipUpdate(baseURL, payload = {}, options = {}) {
  return callControlAction(baseURL, "skip_update", payload, options);
}

async function getUpdateChangelog(baseURL, payload = {}, options = {}) {
  return callControlAction(baseURL, "get_update_changelog", payload, options);
}

async function setVerbosity(baseURL, verbosity, options = {}) {
  return callControlAction(baseURL, "set_verbosity", { scope: "runtime", verbosity }, options);
}

async function setStopSafe(baseURL, runID = "", reason = "", options = {}) {
  return callControlAction(baseURL, "stop_safe", { run_id: runID, reason }, options);
}

async function clearStopFlag(baseURL, runID = "", options = {}) {
  return callControlAction(baseURL, "clear_stop_flag", { run_id: runID }, options);
}

async function streamControlEvents(baseURL, params, onEvent, options = {}) {
  if (typeof onEvent !== "function") {
    throw new Error("event handler is required");
  }

  const fetchImpl = options.fetchImpl || fetch;
  const address = normalizeControlBaseURL(baseURL);
  const url = new URL(`${address}/v2/events`);
  const fromSequence = params && Number.isFinite(params.fromSequence) ? params.fromSequence : 0;
  const runID = params && params.runID ? String(params.runID).trim() : "";
  if (fromSequence > 0) {
    url.searchParams.set("from_sequence", String(fromSequence));
  }
  if (runID !== "") {
    url.searchParams.set("run_id", runID);
  }

  let response;
  try {
    response = await fetchImpl(url.toString(), {
      method: "GET",
      signal: options.signal,
    });
  } catch (_error) {
    throw makeProtocolError("stream_control_events", `Unable to reach control server at ${address}`, {
      address,
      code: "network_unreachable",
    });
  }
  if (!response.ok) {
    throw makeProtocolError("stream_control_events", `Event stream returned HTTP ${response.status}`, {
      address,
      status: response.status,
      code: "event_stream_failed",
    });
  }
  if (!response.body) {
    throw makeProtocolError("stream_control_events", "Event stream response body is unavailable", {
      address,
      status: response.status,
      code: "missing_stream_body",
    });
  }

  await parseNDJSONStream(response.body, onEvent, options.signal);
}

async function parseNDJSONStream(stream, onRecord, signal) {
  const reader = stream.getReader();
  const decoder = new TextDecoder();
  let buffer = "";

  try {
    while (true) {
      if (signal && signal.aborted) {
        return;
      }
      const { value, done } = await reader.read();
      if (done) {
        break;
      }

      buffer += decoder.decode(value, { stream: true });
      const lines = buffer.split(/\r?\n/);
      buffer = lines.pop() || "";
      for (const line of lines) {
        const trimmed = line.trim();
        if (trimmed === "") {
          continue;
        }
        onRecord(JSON.parse(trimmed));
      }
    }

    const finalChunk = (buffer + decoder.decode()).trim();
    if (finalChunk !== "") {
      onRecord(JSON.parse(finalChunk));
    }
  } finally {
    reader.releaseLock();
  }
}

module.exports = {
  normalizeControlBaseURL,
  callControlAction,
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
  installUpdate,
  skipUpdate,
  getUpdateChangelog,
  setVerbosity,
  setStopSafe,
  clearStopFlag,
  streamControlEvents,
  parseNDJSONStream,
};
