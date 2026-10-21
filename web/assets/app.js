function getCookie(name) {
  const prefix = `${name}=`;
  return document.cookie.split("; ").find((row) => row.startsWith(prefix))?.slice(prefix.length) || "";
}
async function api(path, options = {}) {
  const method = (options.method || "GET").toUpperCase();
  const headers = new Headers(options.headers || {});
  if (options.body && !(options.body instanceof FormData)) headers.set("Content-Type", "application/json");
  if (!["GET", "HEAD", "OPTIONS"].includes(method)) headers.set("X-CSRF-Token", decodeURIComponent(getCookie("csrf")));
  const response = await fetch(path, { ...options, method, headers, credentials: "same-origin" });
  const type = response.headers.get("content-type") || "";
  const payload = type.includes("application/json") ? await response.json() : await response.text();
  if (!response.ok) throw new Error(payload?.error || payload || `Request failed (${response.status})`);
  return payload;
}
function showNotice(message, kind = "info") {
  const el = document.getElementById("notice");
  if (!el) return;
  el.className = `alert alert-${kind}`;
  el.textContent = message;
  el.classList.remove("d-none");
}
function hideNotice() {
  const el = document.getElementById("notice");
  if (el) el.classList.add("d-none");
}
