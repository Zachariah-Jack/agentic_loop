const { contextBridge, ipcRenderer } = require("electron");

contextBridge.exposeInMainWorld("auroraLauncher", {
  getInfo() {
    return ipcRenderer.invoke("launcher:get-info");
  },
  openReadme() {
    return ipcRenderer.invoke("launcher:open-readme");
  },
  selectRepo() {
    return ipcRenderer.invoke("launcher:select-repo");
  },
  checkUpdates() {
    return ipcRenderer.invoke("launcher:check-updates");
  },
  start(repoPath = "") {
    return ipcRenderer.invoke("launcher:start", { repoPath });
  },
});
