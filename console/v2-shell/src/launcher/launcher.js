const state = {
  repoPath: "",
  checking: false,
  starting: false,
};

const refs = {};

function $(id) {
  return document.getElementById(id);
}

function setStatus(message, tone = "info") {
  refs.status.textContent = message;
  refs.status.className = `launcher-status launcher-status-${tone}`;
}

function setStartEnabled() {
  refs.start.disabled = state.starting || state.repoPath.trim() === "";
}

async function loadInfo() {
  const info = await window.auroraLauncher.getInfo();
  refs.version.textContent = info.version || "Unavailable";
  if (info.defaultRepoPath) {
    state.repoPath = info.defaultRepoPath;
    refs.selectedRepo.textContent = info.defaultRepoPath;
    setStatus("Ready to open Aurora for the selected project.", "success");
    setStartEnabled();
  }
}

function renderUpdate(result) {
  if (!result || result.ok === false) {
    refs.updateMessage.textContent = result && result.message
      ? result.message
      : "Update status is unavailable right now.";
    refs.checkUpdates.textContent = "Check for Updates";
    return;
  }
  refs.updateMessage.textContent = result.message;
  refs.checkUpdates.textContent = "Check for Updates";
}

async function checkUpdates() {
  if (state.checking) {
    return;
  }
  state.checking = true;
  refs.checkUpdates.disabled = true;
  refs.updateMessage.textContent = "Checking release status...";
  try {
    renderUpdate(await window.auroraLauncher.checkUpdates());
  } catch (error) {
    refs.updateMessage.textContent = `Update check failed: ${error.message}`;
  } finally {
    state.checking = false;
    refs.checkUpdates.disabled = false;
  }
}

async function selectRepo() {
  const result = await window.auroraLauncher.selectRepo();
  if (!result || result.cancelled) {
    return;
  }
  state.repoPath = result.repoPath || "";
  refs.selectedRepo.textContent = state.repoPath || "No folder selected yet.";
  setStatus("Ready to open Aurora for the selected project.", "success");
  setStartEnabled();
}

async function startAurora() {
  if (state.repoPath.trim() === "" || state.starting) {
    return;
  }
  state.starting = true;
  setStartEnabled();
  refs.selectRepo.disabled = true;
  setStatus("Preparing Aurora. This may build the local engine quietly the first time.", "info");
  try {
    await window.auroraLauncher.start(state.repoPath);
  } catch (error) {
    state.starting = false;
    refs.selectRepo.disabled = false;
    setStartEnabled();
    setStatus(`Could not start Aurora: ${error.message}`, "error");
  }
}

function wire() {
  refs.version = $("launcher-version");
  refs.updateMessage = $("update-message");
  refs.checkUpdates = $("check-updates");
  refs.selectRepo = $("select-repo");
  refs.selectedRepo = $("selected-repo");
  refs.readme = $("readme");
  refs.start = $("start");
  refs.status = $("launcher-status");

  refs.checkUpdates.addEventListener("click", checkUpdates);
  refs.selectRepo.addEventListener("click", selectRepo);
  refs.readme.addEventListener("click", () => void window.auroraLauncher.openReadme());
  refs.start.addEventListener("click", startAurora);
}

window.addEventListener("DOMContentLoaded", () => {
  wire();
  void loadInfo();
  void checkUpdates();
});
