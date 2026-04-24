(function registerShellHelpers(root, factory) {
  const api = factory();
  if (typeof module === "object" && module.exports) {
    module.exports = api;
  }
  if (root) {
    root.OrchestratorShellHelpers = api;
  }
})(typeof window !== "undefined" ? window : globalThis, function shellHelpersFactory() {
  const sessionStorageKey = "orchestrator-v2-shell.session";
  const legacyAddressKey = "orchestrator-v2-shell.address";
  const defaultAddress = "http://127.0.0.1:44777";
  const defaultActivityCategories = {
    planner: true,
    executor: true,
    worker: true,
    approval: true,
    intervention: true,
    fault: true,
    terminal: true,
    status: true,
    other: true,
  };

  function safeString(value, fallback = "") {
    const text = String(value || "").trim();
    return text === "" ? fallback : text;
  }

  function defaultShellSession() {
    return {
      address: defaultAddress,
      autoReconnect: true,
      lastConnected: false,
      verbosity: "normal",
      sideChatContextPolicy: "repo_and_latest_run_summary",
      activeTab: "home",
      selectedArtifactPath: "",
      selectedContractPath: "",
      selectedRepoPath: "",
      repoTreePath: "",
      selectedWorkerID: "",
      selectedDogfoodIssueID: "",
      activityFilters: {
        searchText: "",
        currentRunOnly: false,
        autoScroll: true,
        categories: { ...defaultActivityCategories },
      },
    };
  }

  function normalizeShellSession(raw, options = {}) {
    const defaults = defaultShellSession();
    const value = raw && typeof raw === "object" ? raw : {};
    const defaultAddr = safeString(options.defaultAddress, defaultAddress);
    const categories = value.activityFilters && typeof value.activityFilters === "object"
      ? value.activityFilters.categories || {}
      : {};

    return {
      address: safeString(value.address, defaultAddr),
      autoReconnect: value.autoReconnect !== false,
      lastConnected: Boolean(value.lastConnected),
      verbosity: safeString(value.verbosity, defaults.verbosity),
      sideChatContextPolicy: safeString(value.sideChatContextPolicy, defaults.sideChatContextPolicy),
      activeTab: safeString(value.activeTab, defaults.activeTab),
      selectedArtifactPath: safeString(value.selectedArtifactPath),
      selectedContractPath: safeString(value.selectedContractPath),
      selectedRepoPath: safeString(value.selectedRepoPath),
      repoTreePath: safeString(value.repoTreePath),
      selectedWorkerID: safeString(value.selectedWorkerID),
      selectedDogfoodIssueID: safeString(value.selectedDogfoodIssueID),
      activityFilters: {
        searchText: safeString(value.activityFilters && value.activityFilters.searchText),
        currentRunOnly: Boolean(value.activityFilters && value.activityFilters.currentRunOnly),
        autoScroll: value.activityFilters ? value.activityFilters.autoScroll !== false : true,
        categories: {
          planner: categories.planner !== false,
          executor: categories.executor !== false,
          worker: categories.worker !== false,
          approval: categories.approval !== false,
          intervention: categories.intervention !== false,
          fault: categories.fault !== false,
          terminal: categories.terminal !== false,
          status: categories.status !== false,
          other: categories.other !== false,
        },
      },
    };
  }

  function loadShellSession(storage, options = {}) {
    const defaultAddr = safeString(options.defaultAddress, defaultAddress);
    if (!storage || typeof storage.getItem !== "function") {
      return normalizeShellSession({}, { defaultAddress: defaultAddr });
    }

    const stored = storage.getItem(sessionStorageKey);
    if (stored) {
      try {
        return normalizeShellSession(JSON.parse(stored), { defaultAddress: defaultAddr });
      } catch (_error) {
        return normalizeShellSession({}, { defaultAddress: defaultAddr });
      }
    }

    const legacyAddress = safeString(storage.getItem(legacyAddressKey), defaultAddr);
    return normalizeShellSession({ address: legacyAddress }, { defaultAddress: defaultAddr });
  }

  function saveShellSession(storage, session, options = {}) {
    const normalized = normalizeShellSession(session, options);
    if (!storage || typeof storage.setItem !== "function") {
      return normalized;
    }
    storage.setItem(sessionStorageKey, JSON.stringify(normalized));
    storage.setItem(legacyAddressKey, normalized.address);
    return normalized;
  }

  function nextReconnectDelay(attempt) {
    const normalized = Number.isFinite(attempt) && attempt > 0 ? attempt : 1;
    return Math.min(15000, 1000 * Math.pow(2, Math.min(normalized-1, 4)));
  }

  function buildConnectionDetails(connection, reconnectState = {}) {
    const address = safeString(connection && connection.address, defaultAddress);
    const status = safeString(connection && connection.status, "disconnected");
    const message = safeString(connection && connection.message, "not connected");
    const reconnectEnabled = reconnectState.enabled !== false;

    if (status === "connected") {
      return `Ready at ${address}. Live status and activity updates are active.`;
    }
    if (status === "connecting") {
      return `Connecting to ${address}. Rehydrating current run state and live activity.`;
    }
    if (reconnectState.pending) {
      const seconds = Math.max(1, Math.round((reconnectState.delayMs || 1000) / 1000));
      return `Connection lost (${message}). Auto-reconnect will retry in about ${seconds}s.`;
    }
    if (!reconnectEnabled) {
      return `Not connected to ${address}. Auto-reconnect is off; use Connect when ready.`;
    }
    return `Not connected to ${address}. ${message}`;
  }

  function formatProtocolError(scope, error, hint = "") {
    const message = safeString(error && error.message, "Protocol action failed.");
    const code = safeString(error && error.code);
    const status = Number.isFinite(error && error.status) ? `HTTP ${error.status}` : "";
    const scopePrefix = safeString(scope) ? `${scope}: ` : "";
    const suffix = [code, status, safeString(hint)].filter(Boolean).join(" | ");
    return {
      scope: safeString(scope, "protocol"),
      message: `${scopePrefix}${message}${suffix ? ` (${suffix})` : ""}`,
      at: new Date().toISOString(),
    };
  }

  return {
    defaultShellSession,
    defaultActivityCategories,
    sessionStorageKey,
    legacyAddressKey,
    loadShellSession,
    saveShellSession,
    normalizeShellSession,
    nextReconnectDelay,
    buildConnectionDetails,
    formatProtocolError,
  };
});
