const fs = require("node:fs");
const crypto = require("node:crypto");
const net = require("node:net");
const path = require("node:path");
const { execFile, spawn } = require("node:child_process");

const ownerMarker = "orchestrator-v2-dogfood";

function normalizeHTTPAddress(address) {
  const raw = String(address || "").trim();
  if (raw === "") {
    return "";
  }
  return raw.startsWith("http://") || raw.startsWith("https://") ? raw : `http://${raw}`;
}

function loadBackendMetadata(metaPath) {
  if (!metaPath || !fs.existsSync(metaPath)) {
    return null;
  }
  return JSON.parse(fs.readFileSync(metaPath, "utf8").replace(/^\uFEFF/, ""));
}

function writeBackendMetadata(metaPath, metadata) {
  fs.writeFileSync(metaPath, `${JSON.stringify(metadata, null, 2)}\n`, "utf8");
}

function isOwnedBackend(metadata) {
  return Boolean(metadata && metadata.owner === ownerMarker && Number.isInteger(Number(metadata.pid)));
}

function metadataMatchesAddress(metadata, address) {
  const expected = normalizeHTTPAddress(address).replace(/^https?:\/\//, "");
  const actual = normalizeHTTPAddress(metadata && metadata.control_addr).replace(/^https?:\/\//, "");
  return expected === "" || actual === "" || expected === actual;
}

async function killProcessTree(pid, execFileImpl = require("node:child_process").execFile) {
  const numericPID = Number(pid);
  if (!Number.isInteger(numericPID) || numericPID <= 0) {
    return { attempted: false, pid: numericPID, message: "invalid pid" };
  }
  return new Promise((resolve) => {
    if (process.platform === "win32") {
      execFileImpl("taskkill", ["/PID", String(numericPID), "/T", "/F"], { windowsHide: true }, (error, stdout, stderr) => {
        resolve({
          attempted: true,
          pid: numericPID,
          method: "taskkill /T /F",
          ok: !error,
          output: String(stdout || stderr || (error && error.message) || "").trim(),
        });
      });
      return;
    }
    try {
      process.kill(numericPID, "SIGTERM");
      resolve({ attempted: true, pid: numericPID, method: "SIGTERM", ok: true, output: "" });
    } catch (_error) {
      // Process may already be gone; restart can continue.
      resolve({ attempted: true, pid: numericPID, method: "SIGTERM", ok: false, output: _error.message });
    }
  });
}

function sleep(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

function fileModifiedTime(pathValue) {
  const target = String(pathValue || "").trim();
  if (target === "") {
    return "";
  }
  try {
    return fs.statSync(target).mtime.toISOString();
  } catch (_error) {
    return "";
  }
}

function parseControlPort(address) {
  const text = normalizeHTTPAddress(address).replace(/^https?:\/\//, "");
  const portText = text.split(":").pop();
  const port = Number.parseInt(portText, 10);
  return Number.isInteger(port) && port > 0 ? port : 0;
}

function execFilePromise(command, args, options = {}) {
  const execFileImpl = options.execFileImpl || execFile;
  return new Promise((resolve, reject) => {
    execFileImpl(command, args, { windowsHide: true, ...options.execOptions }, (error, stdout, stderr) => {
      if (error) {
        error.stdout = stdout;
        error.stderr = stderr;
        reject(error);
        return;
      }
      resolve({ stdout, stderr });
    });
  });
}

function repoRootPath() {
  return path.resolve(__dirname, "..", "..", "..");
}

function backendBinaryCandidates(options = {}) {
  const metadata = loadBackendMetadata(options.metaPath || process.env.ORCHESTRATOR_V2_BACKEND_META || "");
  const fromMetadata = String(metadata && metadata.binary_path || "").trim();
  const extension = process.platform === "win32" ? ".exe" : "";
  return [
    fromMetadata,
    path.join(repoRootPath(), "dist", `orchestrator${extension}`),
    path.resolve(__dirname, "..", "..", "dist", `orchestrator${extension}`),
  ].filter(Boolean);
}

function resolveBackendBinary(options = {}) {
  const candidates = backendBinaryCandidates(options);
  for (const candidate of candidates) {
    if (fs.existsSync(candidate)) {
      return candidate;
    }
  }
  return candidates[0];
}

async function ensureBackendBinary(options = {}) {
  const existing = resolveBackendBinary(options);
  if (existing && fs.existsSync(existing)) {
    return existing;
  }

  const outputPath = path.join(repoRootPath(), "dist", process.platform === "win32" ? "orchestrator.exe" : "orchestrator");
  fs.mkdirSync(path.dirname(outputPath), { recursive: true });
  await execFilePromise("go", ["build", "-o", outputPath, "./cmd/orchestrator"], {
    execFileImpl: options.execFileImpl,
    execOptions: { cwd: repoRootPath() },
  });
  if (!fs.existsSync(outputPath)) {
    throw new Error(`orchestrator backend binary was not produced at ${outputPath}`);
  }
  return outputPath;
}

async function allocateLoopbackAddress(preferredPort = 0) {
  return new Promise((resolve, reject) => {
    const server = net.createServer();
    server.unref();
    server.on("error", reject);
    server.listen({ host: "127.0.0.1", port: preferredPort }, () => {
      const address = server.address();
      const port = address && address.port ? address.port : preferredPort;
      server.close(() => resolve(`127.0.0.1:${port}`));
    });
  });
}

async function startBackendForRepo(options = {}) {
  const repoPath = String(options.repoPath || "").trim();
  if (repoPath === "") {
    throw new Error("repo path is required");
  }
  if (!fs.existsSync(repoPath)) {
    throw new Error(`repo path does not exist: ${repoPath}`);
  }
  const binaryPath = await ensureBackendBinary(options);
  await execFilePromise(binaryPath, ["init"], {
    execFileImpl: options.execFileImpl,
    execOptions: { cwd: repoPath },
  });
  const controlAddr = options.controlAddr || await allocateLoopbackAddress(0);
  const child = (options.spawnImpl || spawn)(binaryPath, ["control", "serve", "--addr", controlAddr], {
    cwd: repoPath,
    detached: false,
    windowsHide: true,
    stdio: "ignore",
  });
  if (child.unref) {
    child.unref();
  }
  return {
    repoPath,
    address: normalizeHTTPAddress(controlAddr),
    controlAddr,
    binaryPath,
    pid: child.pid,
    ownedBackend: true,
    label: path.basename(repoPath),
  };
}

function normalizePathText(pathValue) {
  return String(pathValue || "").replace(/\//g, "\\").replace(/\\+$/, "").toLowerCase();
}

function listenerMatchesOwnedMetadata(metadata, listener) {
  if (!metadata || !listener) {
    return false;
  }
  if (Number(metadata.pid) > 0 && Number(metadata.pid) === Number(listener.pid)) {
    return true;
  }
  if (Number(metadata.pid) > 0 && Number(metadata.pid) === Number(listener.parent_pid)) {
    return true;
  }
  const expectedPath = normalizePathText(metadata.binary_path);
  const actualPath = normalizePathText(listener.path);
  const commandLine = String(listener.command_line || "").toLowerCase();
  const controlAddr = normalizeHTTPAddress(metadata.control_addr).replace(/^https?:\/\//, "").toLowerCase();
  return expectedPath !== "" &&
    expectedPath === actualPath &&
    commandLine.includes("control serve") &&
    (controlAddr === "" || commandLine.includes(controlAddr));
}

function parsePowerShellJSON(raw) {
  const text = String(raw || "").trim();
  if (text === "") {
    return [];
  }
  const parsed = JSON.parse(text);
  if (!parsed) {
    return [];
  }
  return (Array.isArray(parsed) ? parsed : [parsed]).filter(Boolean);
}

async function listPortListeners(address, execFileImpl = require("node:child_process").execFile) {
  const port = parseControlPort(address);
  if (!port) {
    return [];
  }
  if (process.platform !== "win32") {
    return [];
  }
  const command = [
    "$ErrorActionPreference = 'SilentlyContinue';",
    `$items = @(Get-NetTCPConnection -LocalPort ${port} -State Listen);`,
    "$items | ForEach-Object {",
    "  $proc = Get-CimInstance Win32_Process -Filter \"ProcessId = $($_.OwningProcess)\";",
    "  [pscustomobject]@{",
    "    pid = [int]$_.OwningProcess;",
    "    path = [string]$proc.ExecutablePath;",
    "    command_line = [string]$proc.CommandLine;",
    "    parent_pid = [int]$proc.ParentProcessId",
    "  }",
    "} | ConvertTo-Json -Depth 4",
  ].join(" ");
  return new Promise((resolve) => {
    execFileImpl("powershell.exe", ["-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", command], { windowsHide: true }, (error, stdout) => {
      if (error) {
        resolve([{
          pid: 0,
          path: "Unavailable",
          command_line: `port listener query failed: ${error.message}`,
          parent_pid: 0,
        }]);
        return;
      }
      try {
        resolve(parsePowerShellJSON(stdout).map((item) => ({
          pid: Number(item.pid) || 0,
          path: String(item.path || ""),
          command_line: String(item.command_line || ""),
          parent_pid: Number(item.parent_pid) || 0,
        })));
      } catch (parseError) {
        resolve([{
          pid: 0,
          path: "Unavailable",
          command_line: `port listener query parse failed: ${parseError.message}`,
          parent_pid: 0,
        }]);
      }
    });
  });
}

function formatPortDiagnostic(metadata, listeners, attempts, context) {
  const lines = [
    `port ${metadata.control_addr || "unknown"} did not clear during ${context}.`,
    `attempted owned PID: ${metadata.pid || "unknown"}`,
    `attempted process path: ${metadata.binary_path || "unknown"}`,
    "attempted kill methods:",
  ];
  if (!attempts || attempts.length === 0) {
    lines.push("  - none recorded");
  } else {
    for (const attempt of attempts) {
      lines.push(`  - ${attempt.method || "kill"} pid=${attempt.pid || ""} ok=${attempt.ok === false ? "false" : "true"} ${attempt.output || ""}`.trim());
    }
  }
  if (!listeners || listeners.length === 0) {
    lines.push("current port holders: none observed in final check");
  } else {
    lines.push("current port holders:");
    for (const listener of listeners) {
      lines.push(`  - pid: ${listener.pid}`);
      lines.push(`    path: ${listener.path || "unknown"}`);
      lines.push(`    command_line: ${listener.command_line || "unknown"}`);
      lines.push(`    parent_pid: ${listener.parent_pid || 0}`);
      lines.push(`    matches_owned_metadata: ${listenerMatchesOwnedMetadata(metadata, listener)}`);
      lines.push(`    same_as_attempted_pid: ${Number(listener.pid) === Number(metadata.pid)}`);
    }
  }
  lines.push("next safe action: close the listed unknown process or choose another control address. Unknown processes are not killed automatically.");
  return lines.join("\n");
}

async function clearOwnedBackendPort(metadata, options = {}) {
  const timeoutMs = Number(options.timeoutMs) || 12000;
  const deadline = Date.now() + timeoutMs;
  const execFileImpl = options.execFileImpl || require("node:child_process").execFile;
  const attempts = [];
  while (Date.now() < deadline) {
    const listeners = await listPortListeners(metadata.control_addr, execFileImpl);
    if (listeners.length === 0) {
      return { cleared: true, listeners: [], attempts };
    }
    let killedAny = false;
    for (const listener of listeners) {
      if (listenerMatchesOwnedMetadata(metadata, listener)) {
        attempts.push(await killProcessTree(listener.pid, execFileImpl));
        killedAny = true;
      }
    }
    if (!killedAny) {
      return {
        cleared: false,
        listeners,
        attempts,
        message: formatPortDiagnostic(metadata, listeners, attempts, "backend recovery"),
      };
    }
    await sleep(Number(options.pollMs) || 600);
  }
  const listeners = await listPortListeners(metadata.control_addr, execFileImpl);
  return {
    cleared: listeners.length === 0,
    listeners,
    attempts,
    message: listeners.length === 0 ? "" : formatPortDiagnostic(metadata, listeners, attempts, "backend recovery"),
  };
}

async function restartOwnedBackend(options = {}) {
  const metaPath = String(options.metaPath || process.env.ORCHESTRATOR_V2_BACKEND_META || "").trim();
  const metadata = loadBackendMetadata(metaPath);
  if (!isOwnedBackend(metadata)) {
    return {
      available: false,
      restarted: false,
      message: "Restart Backend is available only when the shell was launched by the dogfood helper with owned-backend metadata.",
    };
  }
  if (!metadataMatchesAddress(metadata, options.address)) {
    return {
      available: false,
      restarted: false,
      message: `Owned backend metadata is for ${metadata.control_addr || "unknown address"}, not ${options.address || "the current address"}.`,
    };
  }

  const attempts = [];
  attempts.push(await killProcessTree(metadata.pid, options.execFileImpl));
  await sleep(Number(options.restartDelayMs) || 500);
  const portResult = await clearOwnedBackendPort(metadata, {
    execFileImpl: options.execFileImpl,
    timeoutMs: options.portClearTimeoutMs,
    pollMs: options.portClearPollMs,
  });
  attempts.push(...(portResult.attempts || []));
  if (!portResult.cleared) {
    return {
      available: true,
      restarted: false,
      blocked: true,
      pid: metadata.pid,
      listeners: portResult.listeners,
      attempts,
      message: portResult.message || "Owned backend process stopped, but the control port is still occupied.",
    };
  }

  const binaryPath = String(metadata.binary_path || "").trim();
  const repoPath = String(metadata.repo_path || "").trim();
  const controlAddr = String(metadata.control_addr || options.address || "127.0.0.1:44777").replace(/^https?:\/\//, "");
  if (binaryPath === "" || repoPath === "") {
    return {
      available: false,
      restarted: false,
      message: "Owned backend metadata is missing binary_path or repo_path.",
    };
  }

  const child = (options.spawnImpl || spawn)(binaryPath, ["control", "serve", "--addr", controlAddr], {
    cwd: repoPath,
    detached: false,
    windowsHide: true,
    stdio: "ignore",
  });
  if (child.unref) {
    child.unref();
  }

  const updated = {
    ...metadata,
    pid: child.pid,
    owner_session_id: crypto.randomUUID(),
    binary_mtime_at_launch: fileModifiedTime(binaryPath),
    started_at: new Date().toISOString(),
    restarted_at: new Date().toISOString(),
  };
  writeBackendMetadata(metaPath, updated);
  return {
    available: true,
    restarted: true,
    pid: child.pid,
    metadata: updated,
    message: `Restarted owned backend process ${child.pid}.`,
  };
}

module.exports = {
  ownerMarker,
  normalizeHTTPAddress,
  loadBackendMetadata,
  writeBackendMetadata,
  isOwnedBackend,
  metadataMatchesAddress,
  fileModifiedTime,
  killProcessTree,
  parseControlPort,
  resolveBackendBinary,
  ensureBackendBinary,
  listenerMatchesOwnedMetadata,
  listPortListeners,
  clearOwnedBackendPort,
  restartOwnedBackend,
  startBackendForRepo,
};
