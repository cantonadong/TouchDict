"use strict";
(() => {
  const $ = id => document.getElementById(id);
  const input = $("search"), list = $("history-list");
  const send = (action, payload = {}) => window.chrome.webview.postMessage(JSON.stringify({ action, ...payload }));
  let snapshot = null, inputRevision = -1, toastTimer = null, measureFrame = 0, lastHeight = -1;
  let composing = false, selectedKey = "", historySignature = "";
  const historyNodes = new Map();
  const text = (id, value) => { const element = $(id); const content = value || ""; if (element.textContent !== content) element.textContent = content; };
  const show = (id, visible) => { $(id).hidden = !visible; };

  const retryQuery = () => snapshot ? (snapshot.state.Selection || snapshot.state.Definition?.term || snapshot.currentQuery || input.value).trim() : "";

  function buttons() {
    if (!snapshot) return;
    const loading = snapshot.state.Kind === 1;
    const q = input.value.trim();
    $("lookup").disabled = loading && q === snapshot.currentQuery;
    $("retry").disabled = !retryQuery();
    $("speak").disabled = snapshot.state.Kind !== 2 || !snapshot.state.Definition.term;
  }

  function scheduleMeasure() {
    if (measureFrame) cancelAnimationFrame(measureFrame);
    measureFrame = requestAnimationFrame(() => {
      measureFrame = 0;
      if (!snapshot) return;
      const areaStyle = getComputedStyle(document.querySelector(".result-area"));
      const workspaceStyle = getComputedStyle(document.querySelector(".workspace"));
      const padding = [workspaceStyle.paddingTop, workspaceStyle.paddingBottom, areaStyle.borderTopWidth, areaStyle.borderBottomWidth, areaStyle.paddingTop, areaStyle.paddingBottom, areaStyle.marginTop, areaStyle.marginBottom, getComputedStyle(document.querySelector(".result-footer")).marginTop].reduce((total, value) => total + (parseFloat(value) || 0), 0);
      const height = Math.ceil(document.querySelector(".topbar").offsetHeight + $("result-content").offsetHeight + document.querySelector(".result-footer").offsetHeight + padding);
      if (height !== lastHeight) { lastHeight = height; send("content-height", { height }); }
    });
  }

  function renderHistory(entries) {
    const signature = JSON.stringify(entries.map(entry => [entry.Key, entry.Query || entry.Definition.term, entry.Definition.partOfSpeech, entry.Definition.meaningZh]));
    if (signature !== historySignature) {
      historySignature = signature;
      const scroll = list.scrollTop, keys = new Set(entries.map(entry => entry.Key));
      for (const [key, node] of historyNodes) {
        if (!keys.has(key)) { node.remove(); historyNodes.delete(key); }
      }
      entries.forEach((entry, index) => {
        let node = historyNodes.get(entry.Key);
        if (!node) {
          node = document.createElement("div");
          node.className = "history-item";
          node.setAttribute("role", "option");
          node.id = `history-option-${index}`;
          node.dataset.key = entry.Key;
          const word = document.createElement("span");
          word.className = "word";
          const heading = document.createElement("div"), pos = document.createElement("span"), meaning = document.createElement("div");
          heading.className = "history-word-row";
          pos.className = "history-pos";
          meaning.className = "history-meaning";
          heading.append(word, pos);
          node.append(heading, meaning);
          node.addEventListener("click", () => selectHistory(node.dataset.key, false));
          historyNodes.set(entry.Key, node);
        }
        node.id = `history-option-${index}`;
        const label = entry.Query || entry.Definition.term || "";
        const pos = entry.Definition.partOfSpeech || "", meaning = entry.Definition.meaningZh || "";
        node.querySelector(".word").textContent = label;
        node.querySelector(".history-pos").textContent = pos;
        node.querySelector(".history-pos").hidden = !pos;
        node.querySelector(".history-meaning").textContent = meaning;
        node.querySelector(".history-meaning").hidden = !meaning;
        const description = [label, pos, meaning].filter(Boolean).join(" · ");
        node.title = description;
        node.setAttribute("aria-label", description);
        // Moving existing nodes only when their order changes preserves focus.
        if (list.children[index] !== node) list.insertBefore(node, list.children[index] || null);
      });
      list.scrollTop = scroll;
    }
    highlightHistory();
    show("history-empty", entries.length === 0);
    $("clear").disabled = !entries.length;
    $("export").disabled = !entries.length;
  }

  function highlightHistory() {
    let active = null;
    for (const [key, node] of historyNodes) {
      const selected = key === selectedKey;
      node.setAttribute("aria-selected", String(selected));
      if (selected) active = node;
    }
    if (active) list.setAttribute("aria-activedescendant", active.id);
    else list.removeAttribute("aria-activedescendant");
  }

  function selectHistory(key, scrollIntoView) {
    if (!snapshot || !historyNodes.has(key)) return;
    selectedKey = key;
    highlightHistory();
    list.focus({ preventScroll: true });
    if (scrollIntoView) historyNodes.get(key).scrollIntoView({ block: "nearest" });
    send("history-select", { key });
  }

  function renderContext(sentence, query) {
    const element = $("context-sentence");
    const signature = JSON.stringify([sentence, query]);
    if (element.dataset.signature === signature) return;
    element.dataset.signature = signature;
    element.replaceChildren();
    const lower = sentence.toLowerCase(), needle = query.trim().toLowerCase();
    let offset = 0, index;
    while (needle && (index = lower.indexOf(needle, offset)) !== -1) {
      element.append(document.createTextNode(sentence.slice(offset, index)));
      const mark = document.createElement("mark");
      mark.textContent = sentence.slice(index, index + needle.length);
      element.append(mark);
      offset = index + needle.length;
    }
    element.append(document.createTextNode(sentence.slice(offset)));
    show("context-card", !!sentence);
  }

  function renderResult(state) {
    const kind = state.Kind, definition = state.Definition || {};
    const empty = kind === 0 || kind === 3;
    renderContext(state.Context || "", state.Selection || snapshot.currentQuery || definition.term || "");
    show("welcome", empty); show("definition", !empty);
    text("welcome-message", state.Message || "在顶部输入英文开始查询");
    text("term", kind === 2 ? definition.term : state.Selection);
    // Definition uses its existing JSON field tags; ViewState retains Go names.
    text("pos", kind === 2 ? definition.partOfSpeech : "");
    show("pos", kind === 2 && !!definition.partOfSpeech);
    show("loading", kind === 1);
    text("meaning", kind === 2 ? definition.meaningZh : "");
    show("meaning-card", kind === 2 && !!definition.meaningZh);
    text("example", kind === 2 ? definition.exampleEn : "");
    text("translation", kind === 2 ? definition.exampleZh : "");
    show("example-card", kind === 2 && (!!definition.exampleEn || !!definition.exampleZh));
    show("translation", kind === 2 && !!definition.exampleZh);
    show("copy", kind === 2 && !!definition.exampleEn);
    text("error", state.Message || "查询失败，请重试");
    show("error", kind === 4);
    text("status", state.Message);
    show("status", kind === 2 && !!state.Message);
    show("suggestion-card", kind === 5);
    const suggestions = $("suggestions");
    const values = state.Suggestions || [];
    if (suggestions.dataset.values !== JSON.stringify(values)) {
      suggestions.dataset.values = JSON.stringify(values);
      suggestions.replaceChildren();
      for (const value of values) {
        const button = document.createElement("button");
        button.type = "button";
        button.textContent = value;
        button.addEventListener("click", () => send("suggestion", { text: value }));
        suggestions.append(button);
      }
    }
  }

  window.touchdict = {
    applyState(state) {
      const previous = snapshot;
      snapshot = state;
      if (state.inputRevision !== inputRevision) {
        inputRevision = state.inputRevision;
        if (input.value !== state.input) input.value = state.input;
      }
      input.disabled = false;
      selectedKey = state.selectedKey;
      renderHistory(state.history || []);
      renderResult(state.state);
      document.documentElement.style.setProperty("--term-size", `${state.termSize}pt`);
      document.documentElement.style.setProperty("--content-size", `${state.contentSize}pt`);
      $("pin").setAttribute("aria-pressed", String(state.pinned));
      text("pin-text", state.pinned ? "取消固顶" : "固顶");
      buttons();
      if (!previous || previous.state.Selection !== state.state.Selection || previous.state.Definition.term !== state.state.Definition.term || previous.state.Kind !== state.state.Kind) $("result-scroll").scrollTop = 0;
      scheduleMeasure();
    },
    focusSearch() { if (!input.disabled) { input.focus({ preventScroll: true }); input.select(); } },
    notice(message) {
      clearTimeout(toastTimer);
      text("toast", message); show("toast", true);
      toastTimer = setTimeout(() => show("toast", false), 2800);
    }
  };

  input.addEventListener("click", () => { if (!composing) input.select(); });
  input.addEventListener("focus", () => { if (!composing) input.select(); });
  input.addEventListener("compositionstart", () => { composing = true; });
  input.addEventListener("compositionend", () => { composing = false; send("input", { text: input.value }); buttons(); });
  input.addEventListener("input", () => { send("input", { text: input.value }); buttons(); });
  input.addEventListener("keydown", event => { if (event.key === "Enter" && (event.isComposing || composing || event.keyCode === 229)) event.preventDefault(); });
  $("search-form").addEventListener("submit", event => {
    event.preventDefault();
    if (composing || !snapshot) return;
    if (!input.value.trim()) { window.touchdict.notice("请输入要查询的英文内容"); input.focus(); return; }
    if (!$("lookup").disabled) send("lookup", { text: input.value });
  });
  $("retry").addEventListener("click", () => send("retry", { text: retryQuery() }));
  $("speak").addEventListener("click", () => send("speak"));
  $("copy").addEventListener("click", () => send("copy-example"));
  $("pin").addEventListener("click", () => send("pin"));
  $("export").addEventListener("click", () => send("history-export"));
  $("clear").addEventListener("click", () => send("history-clear"));
  list.addEventListener("keydown", event => {
    if (!snapshot) return;
    const entries = snapshot.history || [];
    if (event.key === "Delete" && selectedKey) { event.preventDefault(); send("history-delete", { key: selectedKey }); }
    if (!entries.length) return;
    const index = entries.findIndex(entry => entry.Key === selectedKey);
    if (event.key === "ArrowDown" || event.key === "ArrowUp" || event.key === "Home" || event.key === "End") {
      event.preventDefault();
      let next = index < 0 ? 0 : index + (event.key === "ArrowDown" ? 1 : -1);
      if (event.key === "Home") next = 0;
      if (event.key === "End") next = entries.length - 1;
      selectHistory(entries[Math.max(0, Math.min(entries.length - 1, next))].Key, true);
    }
  });
  document.addEventListener("keydown", event => {
    if (!event.ctrlKey || event.altKey || event.isComposing) return;
    const delta = ["+", "=", "Add"].includes(event.key) || event.code === "NumpadAdd" ? 1 : ["-", "Subtract"].includes(event.key) || event.code === "NumpadSubtract" ? -1 : 0;
    const reset = event.key === "0" || event.code === "Numpad0";
    if (delta || reset) { event.preventDefault(); send("zoom", { delta, reset }); }
  });
  document.addEventListener("contextmenu", event => event.preventDefault());
  document.addEventListener("click", event => { if (event.target.closest("a")) event.preventDefault(); });
  new ResizeObserver(scheduleMeasure).observe($("result-content"));
  window.addEventListener("resize", scheduleMeasure);
  send("ready");
})();
