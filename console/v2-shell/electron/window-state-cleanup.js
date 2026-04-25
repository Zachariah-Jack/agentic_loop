function shutdownWindowStateByKey(windowState, key) {
  if (!windowState || !key || !windowState.has(key)) {
    return false;
  }

  const state = windowState.get(key);
  if (state && state.abortController) {
    state.abortController.abort();
    state.abortController = null;
  }
  if (state && state.terminal && typeof state.terminal.shutdown === "function") {
    state.terminal.shutdown();
  }
  windowState.delete(key);
  return true;
}

function shutdownAllWindowStates(windowState) {
  if (!windowState) {
    return 0;
  }

  let closed = 0;
  for (const key of Array.from(windowState.keys())) {
    if (shutdownWindowStateByKey(windowState, key)) {
      closed += 1;
    }
  }
  return closed;
}

module.exports = {
  shutdownWindowStateByKey,
  shutdownAllWindowStates,
};
