const config = window.uptimeConfig || {};
const initialStatus = window.uptimeInitialStatus || null;
const languages = ["en", "zh-CN"];
const $ = (id) => document.getElementById(id);
const messages = {
  en: {
    live: "LIVE",
    stale: "STALE",
    error: "ERROR",
    noServices: "No services have reported heartbeats yet.",
    storageError: "Storage error",
    date: "Date",
    uptime: "Uptime",
    slots: "Up slots",
    downtime: "Estimated downtime",
    status: "Status",
    finalized: "Finalized",
    today: "Today, not finalized",
    noData: "No data"
  },
  "zh-CN": {
    live: "运行中",
    stale: "已延迟",
    error: "错误",
    noServices: "还没有服务上报心跳。",
    storageError: "存储错误",
    date: "日期",
    uptime: "在线率",
    slots: "在线槽位",
    downtime: "估算离线时间",
    status: "状态",
    finalized: "已归档",
    today: "今日，未归档",
    noData: "无数据"
  }
};

let currentLang = detectLang();
let currentTheme = detectTheme();
let currentStatus = "live";
let lastStatus = initialStatus;
let lastSuccessAt = initialStatus ? Date.now() : 0;
let activeBar = null;

function storageGet(key) {
  try { return localStorage.getItem(key); } catch (err) { return ""; }
}

function storageSet(key, value) {
  try { localStorage.setItem(key, value); } catch (err) {}
}

function detectLang() {
  const saved = storageGet("uptime.lang");
  if (saved === "en" || saved === "zh-CN") return saved;
  return config.defaultLanguage === "zh-CN" ? "zh-CN" : "en";
}

function detectTheme() {
  const saved = storageGet("uptime.theme");
  if (saved === "light" || saved === "dark") return saved;
  return config.defaultTheme === "light" ? "light" : "dark";
}

function t(key) {
  return (messages[currentLang] && messages[currentLang][key]) || messages.en[key] || key;
}

function applyLang(lang) {
  currentLang = lang === "zh-CN" ? "zh-CN" : "en";
  storageSet("uptime.lang", currentLang);
  document.documentElement.lang = currentLang;
  $("lang-toggle").dataset.active = currentLang;
  setStatus(currentStatus);
  renderStatus(lastStatus);
}

function applyTheme(theme) {
  currentTheme = theme === "light" ? "light" : "dark";
  document.documentElement.dataset.theme = currentTheme;
  storageSet("uptime.theme", currentTheme);
  $("theme-toggle").dataset.active = currentTheme;
}

function nextTheme() {
  applyTheme(currentTheme === "dark" ? "light" : "dark");
}

function nextLang() {
  const index = languages.indexOf(currentLang);
  applyLang(languages[(index + 1) % languages.length] || "en");
}

function setStatus(status) {
  currentStatus = status;
  $("live-text").textContent = t(status);
  $("live-dot").dataset.status = status;
}

function formatTime(value) {
  if (!value) return "-";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "-";
  return date.toLocaleString(currentLang === "zh-CN" ? "zh-CN" : "en-US");
}

function cardRow(label, value) {
  const item = document.createElement("span");
  item.className = "hovercard-row";
  const left = document.createElement("span");
  left.textContent = label;
  const right = document.createElement("strong");
  right.textContent = value;
  item.append(left, right);
  return item;
}

function dayMarkup(day) {
  const button = document.createElement("button");
  button.type = "button";
  button.className = "bar " + day.status;
  button.setAttribute("aria-label", day.day + " " + percent(day.uptime_rate));
  button.dataset.day = day.day;
  button.dataset.rate = percent(day.uptime_rate);
  button.dataset.upSlots = String(day.up_slots);
  button.dataset.expectedSlots = String(day.expected_slots);
  button.dataset.downtime = formatSeconds(day.estimated_downtime_seconds);
  button.dataset.finalized = String(day.finalized);
  button.dataset.hasData = String(day.has_data);
  return button;
}

function hoverCard() {
  return $("uptime-hovercard");
}

function renderHoverCard(bar) {
  const card = hoverCard();
  if (!card) return;
  card.replaceChildren();
  const title = document.createElement("span");
  title.className = "hovercard-title";
  title.textContent = bar.dataset.day || "";
  card.appendChild(title);
  if (bar.dataset.hasData !== "true") {
    card.appendChild(cardRow(t("status"), t("noData")));
    return;
  }
  card.appendChild(cardRow(t("uptime"), bar.dataset.rate || "0.00%"));
  card.appendChild(cardRow(t("slots"), (bar.dataset.upSlots || "0") + " / " + (bar.dataset.expectedSlots || "0")));
  card.appendChild(cardRow(t("downtime"), bar.dataset.downtime || "0s"));
  card.appendChild(cardRow(t("status"), bar.dataset.finalized === "true" ? t("finalized") : t("today")));
}

function placeHoverCard(bar) {
  const card = hoverCard();
  if (!card || !bar || card.hidden) return;
  const rect = bar.getBoundingClientRect();
  const gap = 12;
  const margin = 12;
  const width = card.offsetWidth;
  const height = card.offsetHeight;
  let left = rect.left + rect.width / 2 - width / 2;
  let top = rect.bottom + gap;

  left = Math.max(margin, Math.min(left, window.innerWidth - width - margin));
  if (top + height > window.innerHeight - margin) {
    top = rect.top - height - gap;
  }
  top = Math.max(margin, Math.min(top, window.innerHeight - height - margin));

  card.style.left = left + "px";
  card.style.top = top + "px";
}

function showHoverCard(bar) {
  const card = hoverCard();
  if (!card) return;
  activeBar = bar;
  renderHoverCard(bar);
  card.hidden = false;
  placeHoverCard(bar);
  card.classList.add("is-visible");
}

function hideHoverCard() {
  const card = hoverCard();
  if (!card) return;
  card.classList.remove("is-visible");
  card.hidden = true;
  activeBar = null;
}

function repositionHoverCard() {
  if (activeBar) placeHoverCard(activeBar);
}

function activateBar(bar) {
  document.querySelectorAll(".bar.is-active").forEach(function(active) {
    if (active !== bar) active.classList.remove("is-active");
  });
  bar.classList.add("is-active");
  showHoverCard(bar);
}

function deactivateBar(bar) {
  bar.classList.remove("is-active");
  if (activeBar === bar) hideHoverCard();
}

function bindBars() {
  document.querySelectorAll(".bar").forEach(function(bar) {
    bar.addEventListener("pointerenter", function() { activateBar(bar); });
    bar.addEventListener("pointerleave", function() { deactivateBar(bar); });
    bar.addEventListener("focus", function() { activateBar(bar); });
    bar.addEventListener("blur", function() { deactivateBar(bar); });
    bar.addEventListener("click", function() { activateBar(bar); });
  });
}

function renderStatus(status) {
  if (!status) return;
  lastStatus = status;
  hideHoverCard();

  $("updated-at").textContent = formatTime(status.generated_at);
  setStatus(status.storage && status.storage.status === "ok" ? "live" : "error");

  let oldError = document.querySelector(".storage-error");
  if (oldError) oldError.remove();
  if (status.storage && status.storage.status !== "ok") {
    const err = document.createElement("div");
    err.className = "storage-error";
    err.textContent = t("storageError") + ": " + (status.storage.last_error || status.storage.status);
    document.querySelector(".description-card").after(err);
  }

  const root = $("services");
  root.replaceChildren();
  if (!status.services || !status.services.length) {
    const empty = document.createElement("div");
    empty.className = "empty";
    empty.textContent = t("noServices");
    root.appendChild(empty);
    return;
  }
  status.services.forEach(function(service) {
    const article = document.createElement("article");
    article.className = "service";

    const info = document.createElement("div");
    const nameLine = document.createElement("div");
    nameLine.className = "service-name";
    const dot = document.createElement("span");
    dot.className = "service-dot " + service.current_status;
    dot.setAttribute("aria-label", service.current_status);
    const name = document.createElement("div");
    name.className = "name";
    name.textContent = service.name;
    nameLine.append(dot, name);
    info.appendChild(nameLine);

    if (service.description) {
      const description = document.createElement("div");
      description.className = "description";
      description.textContent = service.description;
      info.appendChild(description);
    }
    const last = document.createElement("div");
    last.className = "last-seen";
    last.textContent = formatTime(service.last_seen_at);
    info.appendChild(last);

    const bars = document.createElement("div");
    bars.className = "bars";
    bars.setAttribute("aria-label", service.name + " daily uptime");
    (service.daily || []).forEach(function(day) {
      bars.appendChild(dayMarkup(day));
    });

    article.append(info, bars);
    root.appendChild(article);
  });
  bindBars();
}

function percent(rate) {
  if (!Number.isFinite(Number(rate))) return "0.00%";
  return (Number(rate) * 100).toFixed(2) + "%";
}

function formatSeconds(seconds) {
  const value = Math.max(0, Number(seconds || 0));
  if (value < 60) return Math.round(value) + "s";
  if (value < 3600) return Math.floor(value / 60) + "m";
  const hours = Math.floor(value / 3600);
  const minutes = Math.floor((value % 3600) / 60);
  return minutes ? hours + "h " + minutes + "m" : hours + "h";
}

async function refresh() {
  const started = performance.now();
  try {
    const res = await fetch(config.apiPath || (location.pathname.replace(/\/$/, "") + "/api/status"), {
      headers: { Accept: "application/json" },
      cache: "no-store"
    });
    if (!res.ok) throw new Error("bad status: " + res.status);
    const data = await res.json();
    renderStatus(data);
    $("response-time").textContent = (performance.now() - started).toFixed(1) + " ms";
    lastSuccessAt = Date.now();
  } catch (err) {
    setStatus("error");
  }
}

function updateScrollDock() {
  const dock = $("page-scroll-dock");
  const value = $("page-scroll-dock-value");
  if (!dock || !value) return;
  const doc = document.documentElement;
  const maxScroll = Math.max(0, doc.scrollHeight - window.innerHeight);
  const top = window.scrollY || doc.scrollTop || document.body.scrollTop || 0;
  const progress = maxScroll ? Math.min(100, Math.max(0, (top / maxScroll) * 100)) : 0;
  const rounded = Math.round(progress);
  const shouldShow = window.innerWidth >= 768 && maxScroll > 320 && top > 72;
  dock.style.setProperty("--scroll-progress", rounded + "%");
  dock.setAttribute("aria-valuenow", String(rounded));
  dock.setAttribute("aria-label", "Scroll progress " + rounded + "%");
  dock.classList.toggle("page-scroll-dock--visible", shouldShow);
  value.textContent = rounded + "%";
}

function scrollUpQuarter() {
  const doc = document.documentElement;
  const maxScroll = Math.max(0, doc.scrollHeight - window.innerHeight);
  const top = window.scrollY || doc.scrollTop || document.body.scrollTop || 0;
  window.scrollTo({ top: Math.max(0, top - maxScroll * 0.25), behavior: "smooth" });
}

$("lang-toggle").addEventListener("click", nextLang);
$("theme-toggle").addEventListener("click", nextTheme);
$("page-scroll-dock").addEventListener("click", scrollUpQuarter);
window.addEventListener("scroll", function() {
  updateScrollDock();
  repositionHoverCard();
}, { passive: true });
window.addEventListener("resize", function() {
  updateScrollDock();
  repositionHoverCard();
});
setInterval(updateScrollDock, 250);
setInterval(function() {
  if (!lastSuccessAt || currentStatus === "error") return;
  if (Date.now() - lastSuccessAt > Number(config.refreshMS || 5000) * 3) setStatus("stale");
}, 1000);

applyTheme(currentTheme);
applyLang(currentLang);
renderStatus(initialStatus);
updateScrollDock();
refresh();
setInterval(refresh, Math.max(Number(config.refreshMS || 5000), 3000));
