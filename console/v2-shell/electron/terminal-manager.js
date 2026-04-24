const { spawn } = require("node:child_process");

function defaultShellCommand() {
  if (process.platform === "win32") {
    return {
      command: "powershell.exe",
      args: ["-NoLogo"],
      label: "PowerShell",
    };
  }

  return {
    command: process.env.SHELL || "/bin/sh",
    args: [],
    label: "Shell",
  };
}

function trimBuffer(text, maxBufferChars) {
  if (text.length <= maxBufferChars) {
    return text;
  }
  return text.slice(text.length - maxBufferChars);
}

function createTerminalManager(options = {}) {
  const spawnImpl = options.spawnImpl || spawn;
  const maxBufferChars = Number.isInteger(options.maxBufferChars) ? options.maxBufferChars : 32768;
  const stateListeners = [];
  const dataListeners = [];
  const sessions = new Map();
  let activeSessionID = "";
  let sessionCounter = 0;

  function emitState() {
    const nextSnapshot = snapshot();
    stateListeners.forEach((listener) => listener(nextSnapshot));
    return nextSnapshot;
  }

  function emitData(payload) {
    dataListeners.forEach((listener) => listener(payload));
  }

  function currentShellLabel() {
    return defaultShellCommand().label;
  }

  function normalizedSession(session) {
    if (!session) {
      return null;
    }

    return {
      session_id: session.session_id,
      label: session.label,
      status: session.status,
      shell_label: session.shell_label,
      command: session.command,
      args: [...session.args],
      pid: session.pid,
      cwd: session.cwd,
      buffered_output: session.buffered_output,
      message: session.message,
      exit_code: session.exit_code,
      updated_at: session.updated_at,
    };
  }

  function setActiveSession(sessionID) {
    if (!sessionID || !sessions.has(sessionID)) {
      return snapshot();
    }

    activeSessionID = sessionID;
    return emitState();
  }

  function appendOutput(session, stream, chunk) {
    const text = typeof chunk === "string" ? chunk : chunk.toString("utf8");
    session.buffered_output = trimBuffer(`${session.buffered_output}${text}`, maxBufferChars);
    session.updated_at = new Date().toISOString();
    emitData({
      session_id: session.session_id,
      stream,
      chunk: text,
      snapshot: snapshot(),
    });
  }

  function bindChildProcess(session, child) {
    session.child = child;
    session.pid = child.pid || null;

    child.on("spawn", () => {
      session.status = "running";
      session.pid = child.pid || null;
      session.message = "terminal session running";
      session.updated_at = new Date().toISOString();
      emitState();
    });

    child.stdout.on("data", (chunk) => {
      appendOutput(session, "stdout", chunk);
    });

    child.stderr.on("data", (chunk) => {
      appendOutput(session, "stderr", chunk);
    });

    child.on("error", (error) => {
      session.status = "error";
      session.message = error.message;
      session.updated_at = new Date().toISOString();
      emitState();
    });

    child.on("close", (code) => {
      session.child = null;
      session.pid = null;
      session.status = "stopped";
      session.exit_code = typeof code === "number" ? code : null;
      session.message = session.exit_code === 0
        ? "terminal session exited cleanly"
        : `terminal session exited${session.exit_code === null ? "" : ` with code ${session.exit_code}`}`;
      session.updated_at = new Date().toISOString();
      emitState();
    });
  }

  function createSession({ cwd = process.cwd(), label = "" } = {}) {
    const shell = defaultShellCommand();
    sessionCounter += 1;
    const sessionID = `terminal_${sessionCounter}`;
    const session = {
      session_id: sessionID,
      label: String(label || "").trim() || `${shell.label} ${sessionCounter}`,
      status: "starting",
      shell_label: shell.label,
      command: shell.command,
      args: [...shell.args],
      pid: null,
      cwd,
      buffered_output: "",
      message: "starting terminal session",
      exit_code: null,
      updated_at: new Date().toISOString(),
      child: null,
    };

    sessions.set(sessionID, session);
    activeSessionID = sessionID;
    emitState();

    try {
      const child = spawnImpl(shell.command, shell.args, {
        cwd,
        env: process.env,
        windowsHide: true,
      });
      bindChildProcess(session, child);
    } catch (error) {
      session.status = "error";
      session.message = error.message;
      session.updated_at = new Date().toISOString();
      emitState();
    }
    return snapshot();
  }

  function start(options = {}) {
    return createSession(options);
  }

  function resolveSession(sessionID = "") {
    const resolvedID = String(sessionID || activeSessionID || "").trim();
    return resolvedID && sessions.has(resolvedID) ? sessions.get(resolvedID) : null;
  }

  function send(input, sessionID = "") {
    const session = resolveSession(sessionID);
    if (!session) {
      throw new Error("no terminal session is available");
    }
    if (session.status !== "running" || !session.child || !session.child.stdin) {
      throw new Error("terminal session is not running");
    }

    session.child.stdin.write(input);
    session.updated_at = new Date().toISOString();
    return snapshot();
  }

  function stop(sessionID = "") {
    const session = resolveSession(sessionID);
    if (!session) {
      return snapshot();
    }
    if (session.child) {
      session.message = "stopping terminal session";
      session.updated_at = new Date().toISOString();
      emitState();
      session.child.kill();
    }
    return snapshot();
  }

  function clear(sessionID = "") {
    const session = resolveSession(sessionID);
    if (!session) {
      return snapshot();
    }
    session.buffered_output = "";
    session.message = session.status === "running"
      ? "terminal output cleared"
      : session.message;
    session.updated_at = new Date().toISOString();
    return emitState();
  }

  function close(sessionID = "") {
    const session = resolveSession(sessionID);
    if (!session) {
      return snapshot();
    }

    if (session.child) {
      session.child.kill();
    }
    sessions.delete(session.session_id);

    if (activeSessionID === session.session_id) {
      const nextSession = Array.from(sessions.values()).at(-1) || null;
      activeSessionID = nextSession ? nextSession.session_id : "";
    }

    return emitState();
  }

  function shutdown() {
    Array.from(sessions.values()).forEach((session) => {
      if (session.child) {
        session.child.kill();
      }
    });
    sessions.clear();
    activeSessionID = "";
    return emitState();
  }

  function snapshot() {
    const normalizedSessions = Array.from(sessions.values()).map(normalizedSession);
    const activeSession = normalizedSessions.find((session) => session.session_id === activeSessionID) || null;

    return {
      available: true,
      count: normalizedSessions.length,
      active_session_id: activeSession ? activeSession.session_id : "",
      active_session: activeSession,
      sessions: normalizedSessions,
      status: activeSession ? activeSession.status : "stopped",
      shell_label: activeSession ? activeSession.shell_label : currentShellLabel(),
      command: activeSession ? activeSession.command : "",
      args: activeSession ? [...activeSession.args] : [],
      pid: activeSession ? activeSession.pid : null,
      cwd: activeSession ? activeSession.cwd : "",
      buffered_output: activeSession ? activeSession.buffered_output : "",
      message: activeSession ? activeSession.message : "No terminal sessions yet.",
      exit_code: activeSession ? activeSession.exit_code : null,
    };
  }

  function onState(listener) {
    stateListeners.push(listener);
  }

  function onData(listener) {
    dataListeners.push(listener);
  }

  return {
    start,
    createSession,
    activate: setActiveSession,
    send,
    stop,
    clear,
    close,
    shutdown,
    snapshot,
    onState,
    onData,
  };
}

module.exports = {
  createTerminalManager,
  defaultShellCommand,
};
