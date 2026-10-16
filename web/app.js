(function () {
  "use strict";

  const DEBUG = new URLSearchParams(location.search).has("debug");
  const dlog = (...a) => { if (DEBUG) console.log(...a); };
  const dwarn = (...a) => { if (DEBUG) console.warn(...a); };

  // ===== 视口高度跟踪 + 保持贴底状态 =====
  function setupViewportHeight() {
    const root = document.documentElement;
    let rafId = 0;

    function update() {
      if (rafId) return;
      rafId = requestAnimationFrame(() => {
        rafId = 0;
        const vv = window.visualViewport;
        const h = vv ? vv.height : window.innerHeight;

        const distBefore =
          messagesEl.scrollHeight - messagesEl.scrollTop - messagesEl.clientHeight;
        const wasAtBottom = distBefore < 80;

        root.style.setProperty("--app-height", h + "px");

        if (wasAtBottom) {
          requestAnimationFrame(() => {
            messagesEl.scrollTop = messagesEl.scrollHeight;
          });
        }
        scheduleStickyUpdate();
      });
    }

    if (window.visualViewport) {
      window.visualViewport.addEventListener("resize", update);
      window.visualViewport.addEventListener("scroll", () => {
        if (window.scrollY !== 0) {
          window.scrollTo(0, 0);
        }
        update();
      });
    }
    window.addEventListener("resize", update);
    window.addEventListener("orientationchange", update);
    update();
  }

  let root, ChatMessage, MessageType;

  async function initProto() {
    root = await protobuf.load("chat.proto");
    ChatMessage = root.lookupType("chat.ChatMessage");
    MessageType = root.lookupEnum("chat.MessageType");
    dlog("[init] proto loaded");
  }

  const state = {
    ws: null,
    myUserID: null,
    myName: "",
    clientID: null,
    avatarCache: new Map(),
    replyTo: null,
    onlineCount: 0,
    reconnectDelay: 1000,
    scrollLocked: false,
    maxSeen: 500,
    seenIDs: new Set(),
    renameLock: false,
    repeatTarget: null,
    hasActivity: false,
  };

  const $ = (id) => document.getElementById(id);
  const messagesEl = $("messages");
  const stickyHeaderEl = $("sticky-header");
  const inputEl = $("input");
  const sendBtn = $("send-btn");
  const imageBtn = $("image-btn");
  const imageInput = $("image-input");
  const renameBtn = $("rename-btn");
  const themeToggle = $("theme-toggle");
  const onlineCountEl = $("online-count");
  const scrollBottomBtn = $("scroll-bottom-btn");
  const replyBar = $("reply-bar");
  const replyBarPreview = $("reply-bar-preview");
  const replyCancel = $("reply-cancel");
  const lightbox = $("image-lightbox");
  const lightboxImg = $("lightbox-img");
  const myAvatarEl = $("my-avatar");
  const myNameEl = $("my-name");
  const refreshModal = $("refresh-modal");
  const refreshCancel = $("refresh-cancel");
  const refreshConfirm = $("refresh-confirm");

  // ===== 主题 =====
  function applyTheme(theme) {
    document.documentElement.setAttribute("data-theme", theme);
    localStorage.setItem("theme", theme);
  }
  function initTheme() {
    const saved = localStorage.getItem("theme");
    if (saved) applyTheme(saved);
    else {
      const prefersDark = window.matchMedia("(prefers-color-scheme: dark)").matches;
      applyTheme(prefersDark ? "dark" : "light");
    }
  }
  themeToggle.addEventListener("click", () => {
    const cur = document.documentElement.getAttribute("data-theme");
    applyTheme(cur === "dark" ? "light" : "dark");
  });

  // ===== client_id =====
  function genUUID() {
    if (typeof crypto !== "undefined" && typeof crypto.randomUUID === "function") {
      return crypto.randomUUID();
    }
    if (typeof crypto !== "undefined" && typeof crypto.getRandomValues === "function") {
      const bytes = new Uint8Array(16);
      crypto.getRandomValues(bytes);
      bytes[6] = (bytes[6] & 0x0f) | 0x40;
      bytes[8] = (bytes[8] & 0x3f) | 0x80;
      const hex = Array.from(bytes, (b) => b.toString(16).padStart(2, "0")).join("");
      return hex.slice(0,8) + "-" + hex.slice(8,12) + "-" + hex.slice(12,16) + "-" + hex.slice(16,20) + "-" + hex.slice(20);
    }
    return "xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx".replace(/[xy]/g, (c) => {
      const r = (Math.random() * 16) | 0;
      const v = c === "x" ? r : (r & 0x3) | 0x8;
      return v.toString(16);
    });
  }
  function getClientID() {
    if (!state.clientID) state.clientID = genUUID();
    return state.clientID;
  }

  // ===== 阻止点击按钮时键盘收起 =====
  function keepKeyboardOpen(el) {
    if (!el) return;
    el.addEventListener("mousedown", (e) => e.preventDefault());
  }

  // ===== 刷新拦截 =====
  document.addEventListener("keydown", (e) => {
    if (!state.hasActivity) return;
    const isRefresh =
      e.key === "F5" ||
      ((e.ctrlKey || e.metaKey) && (e.key === "r" || e.key === "R"));
    if (isRefresh) {
      e.preventDefault();
      e.stopPropagation();
      refreshModal.classList.remove("hidden");
    }
  }, true);

  refreshCancel.addEventListener("click", () => {
    refreshModal.classList.add("hidden");
  });

  refreshConfirm.addEventListener("click", () => {
    state.hasActivity = false;
    location.reload();
  });

  refreshModal.addEventListener("click", (e) => {
    if (e.target === refreshModal) refreshModal.classList.add("hidden");
  });

  window.addEventListener("beforeunload", (e) => {
    if (state.hasActivity) {
      e.preventDefault();
      e.returnValue = "";
    }
  });

  // ===== WebSocket =====
  function connect() {
    const proto = location.protocol === "https:" ? "wss:" : "ws:";
    const url = `${proto}//${location.host}/ws?client_id=${encodeURIComponent(getClientID())}`;
    dlog("[ws] connecting", url);
    const ws = new WebSocket(url);
    ws.binaryType = "arraybuffer";
    state.ws = ws;

    ws.onopen = () => {
      dlog("[ws] open");
      state.reconnectDelay = 1000;
    };

    ws.onmessage = (ev) => {
      try {
        const buf = new Uint8Array(ev.data);
        const msg = ChatMessage.decode(buf);
        handleMessage(msg);
      } catch (err) {
        console.error("[ws] decode error", err);
      }
    };

    ws.onclose = () => {
      dwarn("[ws] closed");
      scheduleReconnect();
    };
    ws.onerror = () => {};
  }

  function scheduleReconnect() {
    const delay = state.reconnectDelay;
    state.reconnectDelay = Math.min(delay * 2, 15000);
    dlog("[ws] reconnect in", delay, "ms");
    setTimeout(connect, delay);
  }

  // ===== 发送 =====
  function send(obj) {
    if (!state.ws || state.ws.readyState !== WebSocket.OPEN) return;
    const msg = ChatMessage.create(obj);
    const buf = ChatMessage.encode(msg).finish();
    state.ws.send(buf);
  }

  function sendText(text) {
    const trimmed = text.trim();
    if (!trimmed) return;
    const payload = { type: MessageType.values.MESSAGE, content: trimmed };
    if (state.replyTo) payload.replyTo = state.replyTo.messageId;
    // 用户主动发消息 → 强制跟最新
    state.scrollLocked = false;
    scrollBottomBtn.classList.add("hidden");
    send(payload);
    state.hasActivity = true;
    clearReply();
    scrollToBottom();
  }

  function sendImage(file) {
    const reader = new FileReader();
    reader.onload = () => {
      const bytes = new Uint8Array(reader.result);
      const payload = { type: MessageType.values.MESSAGE, content: "", imageData: bytes };
      if (state.replyTo) payload.replyTo = state.replyTo.messageId;
      state.scrollLocked = false;
      scrollBottomBtn.classList.add("hidden");
      send(payload);
      state.hasActivity = true;
      clearReply();
      scrollToBottom();
    };
    reader.readAsArrayBuffer(file);
  }

  // ===== 消息处理 =====
  function handleMessage(msg) {
    switch (msg.type) {
      case MessageType.values.JOIN: handleJoin(msg); break;
      case MessageType.values.LEAVE: break;
      case MessageType.values.RENAME: handleRename(msg); break;
      case MessageType.values.SYSTEM: handleSystem(msg); break;
      case MessageType.values.MESSAGE: handleChat(msg); break;
    }
  }

  function handleJoin(msg) {
    if (msg.avatarSvg) state.avatarCache.set(msg.senderId, msg.avatarSvg);
    if (msg.content === "__welcome__") {
      state.myUserID = msg.senderId;
      state.myName = msg.senderName;
      updateMyIdentity();
    }
  }

  function handleRename(msg) {
    if (msg.avatarSvg) state.avatarCache.set(msg.senderId, msg.avatarSvg);
    if (msg.replyToSenderId && msg.replyToSenderId === state.myUserID) {
      state.myUserID = msg.senderId;
      state.myName = msg.senderName;
      state.renameLock = false;
      updateMyIdentity();
    }
  }

  function updateMyIdentity() {
    myNameEl.textContent = state.myName || "—";
    const svg = state.avatarCache.get(state.myUserID);
    if (svg) myAvatarEl.innerHTML = svg;
    else myAvatarEl.textContent = (state.myName || "?").slice(0, 1);
  }

  function handleSystem(msg) {
    if (msg.content === "__archive__") {
      messagesEl.innerHTML = "";
      state.seenIDs.clear();
      state.scrollLocked = false;
      scrollBottomBtn.classList.add("hidden");
      scheduleStickyUpdate();
      return;
    }
    const n = parseInt(msg.content, 10);
    if (!isNaN(n)) {
      state.onlineCount = n;
      onlineCountEl.textContent = n;
    }
  }

  function handleChat(msg) {
    if (state.seenIDs.has(msg.messageId)) return;
    state.seenIDs.add(msg.messageId);
    if (state.seenIDs.size > state.maxSeen) {
      const arr = Array.from(state.seenIDs);
      state.seenIDs = new Set(arr.slice(arr.length / 2));
    }
    if (msg.avatarSvg) state.avatarCache.set(msg.senderId, msg.avatarSvg);
    state.hasActivity = true;
    renderMessage(msg);
  }

  // ===== 渲染 =====
  function renderMessage(msg) {
    const isOwn = msg.senderId === state.myUserID;
    const isMentioned = msg.replyToSenderId && msg.replyToSenderId === state.myUserID;

    const row = document.createElement("div");
    row.className = "msg-row" + (isOwn ? " own" : "");
    if (isMentioned) row.classList.add("mentioned");
    row.dataset.messageId = msg.messageId;
    row.dataset.senderId = msg.senderId;

    const allRows = messagesEl.querySelectorAll(".msg-row");
    if (allRows.length > 0) {
      const lastRow = allRows[allRows.length - 1];
      if (lastRow.dataset.senderId === msg.senderId && !msg.replyTo) {
        row.classList.add("continuation");
      }
    }

    const avatar = document.createElement("div");
    avatar.className = "msg-avatar";
    const svg = state.avatarCache.get(msg.senderId);
    if (svg) avatar.innerHTML = svg;
    else avatar.textContent = (msg.senderName || "?").slice(0, 1);
    row.appendChild(avatar);

    const body = document.createElement("div");
    body.className = "msg-body";

    const meta = document.createElement("div");
    meta.className = "msg-meta";
    const nameEl = document.createElement("span");
    nameEl.className = "msg-name";
    nameEl.textContent = msg.senderName || "匿名";
    const timeEl = document.createElement("span");
    timeEl.className = "msg-time";
    timeEl.textContent = formatTime(msg.timestamp);
    meta.appendChild(nameEl);
    meta.appendChild(timeEl);
    body.appendChild(meta);

    const contentRow = document.createElement("div");
    contentRow.className = "msg-content-row";

    const bubble = document.createElement("div");
    bubble.className = "msg-bubble";

    if (msg.replyTo && msg.replyToPreview) {
      const quote = document.createElement("div");
      quote.className = "msg-reply-quote";
      const idx = msg.replyToPreview.indexOf(": ");
      const qName = idx >= 0 ? msg.replyToPreview.slice(0, idx) : "";
      const qText = idx >= 0 ? msg.replyToPreview.slice(idx + 2) : msg.replyToPreview;
      quote.innerHTML =
        `<span class="quote-name">${escapeHtml(qName)}</span>` +
        `<span class="quote-text">${escapeHtml(qText)}</span>`;
      quote.addEventListener("click", () => scrollToMessage(msg.replyTo));
      bubble.appendChild(quote);
    }

    if (msg.imageData && msg.imageData.length > 0) {
      const img = document.createElement("img");
      img.className = "msg-image loading";
      const blob = new Blob([msg.imageData], { type: "image/*" });
      const url = URL.createObjectURL(blob);
      img.src = url;
      img.alt = "图片";
      img.addEventListener("load", () => img.classList.remove("loading"));
      img.addEventListener("error", () => {
        img.classList.remove("loading");
        img.alt = "图片加载失败";
      });
      img.addEventListener("click", () => {
        lightboxImg.src = url;
        lightbox.classList.remove("hidden");
      });
      bubble.appendChild(img);
    }

    if (msg.content) {
      const textNode = document.createElement("div");
      textNode.className = "msg-text";
      textNode.textContent = msg.content;
      bubble.appendChild(textNode);
    }

    bubble.addEventListener("contextmenu", (e) => {
      e.preventDefault();
      startReply(msg);
    });
    let touchTimer = null;
    bubble.addEventListener("touchstart", () => {
      touchTimer = setTimeout(() => startReply(msg), 600);
    });
    bubble.addEventListener("touchend", () => clearTimeout(touchTimer));
    bubble.addEventListener("touchmove", () => clearTimeout(touchTimer));

    contentRow.appendChild(bubble);
    body.appendChild(contentRow);
    row.appendChild(body);

    const wasAtBottom = !state.scrollLocked;
    messagesEl.appendChild(row);

    updateRepeatButton();
    scheduleStickyUpdate();

    if (wasAtBottom) scrollToBottom();
    else scrollBottomBtn.classList.remove("hidden");
  }

  function formatTime(ts) {
    if (!ts) return "";
    const d = new Date(Number(ts));
    return `${String(d.getHours()).padStart(2,"0")}:${String(d.getMinutes()).padStart(2,"0")}`;
  }

  function escapeHtml(s) {
    const div = document.createElement("div");
    div.textContent = s;
    return div.innerHTML;
  }

  // ===== Sticky Header =====
  let stickyRaf = 0;
  function scheduleStickyUpdate() {
    if (stickyRaf) return;
    stickyRaf = requestAnimationFrame(() => {
      stickyRaf = 0;
      updateStickyHeader();
    });
  }

  function updateStickyHeader() {
    const wrapTop = messagesEl.getBoundingClientRect().top;
    const rows = messagesEl.querySelectorAll(".msg-row");
    let target = null;

    // 找视口内第一条（bottom > wrapTop）
    for (let i = 0; i < rows.length; i++) {
      const rect = rows[i].getBoundingClientRect();
      if (rect.bottom > wrapTop) {
        target = rows[i];
        break;
      }
    }
    if (!target) {
      stickyHeaderEl.classList.add("hidden");
      return;
    }

    // 往上找聚合组头（第一条非 continuation）
    let head = target;
    while (
      head.previousElementSibling &&
      head.previousElementSibling.classList &&
      head.previousElementSibling.classList.contains("msg-row") &&
      head.previousElementSibling.classList.contains("continuation")
    ) {
      head = head.previousElementSibling;
    }

    // 组头仍在视口内 → 不需要浮动
    if (head.getBoundingClientRect().top >= wrapTop) {
      stickyHeaderEl.classList.add("hidden");
      return;
    }

    const avatarEl = head.querySelector(".msg-avatar");
    const nameEl = head.querySelector(".msg-name");
    if (!avatarEl || !nameEl) {
      stickyHeaderEl.classList.add("hidden");
      return;
    }

    const isOwn = head.classList.contains("own");

    stickyHeaderEl.classList.toggle("own", isOwn);

    // 只在内容变化时更新 DOM，避免每帧重建
    const key = (isOwn ? "1" : "0") + "|" + nameEl.textContent;
    if (stickyHeaderEl.dataset.key === key) {
      stickyHeaderEl.classList.remove("hidden");
      return;
    }
    stickyHeaderEl.dataset.key = key;

    stickyHeaderEl.innerHTML = "";

    const av = document.createElement("div");
    av.className = "sticky-avatar";
    av.innerHTML = avatarEl.innerHTML;

    const nm = document.createElement("span");
    nm.className = "sticky-name";
    nm.textContent = nameEl.textContent;

    if (isOwn) {
      stickyHeaderEl.appendChild(nm);
      stickyHeaderEl.appendChild(av);
    } else {
      stickyHeaderEl.appendChild(av);
      stickyHeaderEl.appendChild(nm);
    }

    stickyHeaderEl.classList.remove("hidden");
  }

  // ===== 滚动 =====
  function scrollToBottom() {
    requestAnimationFrame(() => {
      messagesEl.scrollTop = messagesEl.scrollHeight;
      scheduleStickyUpdate();
    });
  }
  messagesEl.addEventListener("scroll", () => {
    const dist = messagesEl.scrollHeight - messagesEl.scrollTop - messagesEl.clientHeight;
    state.scrollLocked = dist > 80;
    if (!state.scrollLocked) scrollBottomBtn.classList.add("hidden");
    scheduleStickyUpdate();
  });
  scrollBottomBtn.addEventListener("click", () => {
    state.scrollLocked = false;
    scrollBottomBtn.classList.add("hidden");
    scrollToBottom();
  });

  function scrollToMessage(messageId) {
    const el = messagesEl.querySelector(`[data-message-id="${messageId}"]`);
    if (el) {
      el.scrollIntoView({ behavior: "smooth", block: "center" });
      const bubble = el.querySelector(".msg-bubble");
      if (bubble) {
        const old = bubble.style.boxShadow;
        bubble.style.boxShadow = "0 0 0 3px rgba(77,124,254,.4)";
        setTimeout(() => { bubble.style.boxShadow = old; }, 800);
      }
    }
  }

  // ===== 引用 =====
  function startReply(msg) {
    state.replyTo = {
      messageId: msg.messageId,
      name: msg.senderName,
      preview: msg.content || (msg.imageData ? "[图片]" : ""),
    };
    replyBarPreview.textContent = `${msg.senderName}: ${state.replyTo.preview}`;
    replyBar.classList.remove("hidden");
    inputEl.focus();
  }
  function clearReply() {
    state.replyTo = null;
    replyBar.classList.add("hidden");
  }
  replyCancel.addEventListener("click", clearReply);

  // ===== 输入 =====
  function autoResize() {
    inputEl.style.height = "auto";
    inputEl.style.height = Math.min(inputEl.scrollHeight, 140) + "px";
  }
  inputEl.addEventListener("input", () => {
    autoResize();
    sendBtn.disabled = inputEl.value.trim() === "" && !state.replyTo;
  });
  inputEl.addEventListener("keydown", (e) => {
    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault();
      sendText(inputEl.value);
      inputEl.value = "";
      autoResize();
      sendBtn.disabled = true;
    }
  });
  sendBtn.addEventListener("click", () => {
    sendText(inputEl.value);
    inputEl.value = "";
    autoResize();
    sendBtn.disabled = true;
  });

  // ===== 图片 =====
  imageBtn.addEventListener("click", () => imageInput.click());
  imageInput.addEventListener("change", () => {
    const file = imageInput.files[0];
    if (!file) return;
    compressImage(file, 200 * 1024).then((compressed) => {
      sendImage(compressed);
    }).catch((err) => {
      console.error("compress error", err);
      if (file.size <= 2 * 1024 * 1024) sendImage(file);
      else alert("图片太大，请选择小于 2MB 的图片");
    });
    imageInput.value = "";
  });

  function compressImage(file, maxBytes) {
    return new Promise((resolve, reject) => {
      const img = new Image();
      const url = URL.createObjectURL(file);
      img.onload = () => {
        URL.revokeObjectURL(url);
        let { width, height } = img;
        const maxDim = 1200;
        if (width > maxDim || height > maxDim) {
          const ratio = Math.min(maxDim / width, maxDim / height);
          width = Math.round(width * ratio);
          height = Math.round(height * ratio);
        }
        const canvas = document.createElement("canvas");
        canvas.width = width;
        canvas.height = height;
        const ctx = canvas.getContext("2d");
        ctx.drawImage(img, 0, 0, width, height);
        let quality = 0.85;
        const tryCompress = () => {
          canvas.toBlob((blob) => {
            if (!blob) return reject(new Error("toBlob failed"));
            if (blob.size <= maxBytes || quality <= 0.3) {
              resolve(new File([blob], file.name, { type: "image/jpeg" }));
            } else {
              quality -= 0.1;
              tryCompress();
            }
          }, "image/jpeg", quality);
        };
        tryCompress();
      };
      img.onerror = () => { URL.revokeObjectURL(url); reject(new Error("image load failed")); };
      img.src = url;
    });
  }

  // ===== 换马甲 =====
  function requestRename() {
    if (state.renameLock) return;
    if (!confirm("换马甲后将获得全新身份，且不可恢复。继续吗？")) return;
    state.renameLock = true;
    send({ type: MessageType.values.RENAME, content: "" });
    setTimeout(() => { state.renameLock = false; }, 5000);
  }
  renameBtn.addEventListener("click", requestRename);

  // ===== 图片浮层 =====
  lightbox.addEventListener("click", () => {
    lightbox.classList.add("hidden");
    lightboxImg.src = "";
  });

  // ===== 复读 +1 =====
  function updateRepeatButton() {
    messagesEl.querySelectorAll(".repeat-plus1").forEach((b) => b.remove());
    state.repeatTarget = null;

    const rows = Array.from(messagesEl.querySelectorAll(".msg-row"));
    const textMsgs = [];
    for (let i = rows.length - 1; i >= 0 && textMsgs.length < 2; i--) {
      const r = rows[i];
      const textEl = r.querySelector(".msg-text");
      const imgEl = r.querySelector(".msg-image");
      if (textEl && !imgEl && textEl.textContent !== "") {
        textMsgs.unshift({ row: r, text: textEl.textContent });
      } else {
        textMsgs.unshift(null);
      }
    }

    if (textMsgs.length < 2) return;
    const [a, b] = textMsgs;
    if (!a || !b) return;
    if (a.text !== b.text) return;
    if (b.row.classList.contains("own")) return;

    state.repeatTarget = b.text;

    const btn = document.createElement("button");
    btn.type = "button";
    btn.className = "repeat-plus1";
    btn.title = "复读上一条";
    btn.textContent = "+1";

    keepKeyboardOpen(btn);

    btn.addEventListener("click", () => {
      if (state.repeatTarget !== null) {
        state.scrollLocked = false;
        scrollBottomBtn.classList.add("hidden");
        sendText(state.repeatTarget);
        scrollToBottom();
      }
    });

    const contentRow = b.row.querySelector(".msg-content-row");
    if (contentRow) contentRow.appendChild(btn);
  }

  // ===== 初始化 =====
  async function boot() {
    initTheme();
    setupViewportHeight();

    keepKeyboardOpen(scrollBottomBtn);
    keepKeyboardOpen(themeToggle);

    try {
      await initProto();
    } catch (err) {
      console.error("[init] proto failed", err);
      myNameEl.textContent = "proto 加载失败";
      return;
    }
    connect();
  }

  boot();
})();