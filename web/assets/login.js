(async () => {
  try {
    const status = await api("/api/setup/status");
    if (status.setup_required) { location.replace("/setup"); return; }
    const me = await api("/api/auth/me");
    if (me.username) location.replace("/");
  } catch (_) {}
})();
document.getElementById("login-form").addEventListener("submit", async (event) => {
  event.preventDefault(); hideNotice();
  const button = document.getElementById("submit"); button.disabled = true;
  try {
    await api("/api/auth/login", { method: "POST", body: JSON.stringify({
      username: document.getElementById("username").value,
      password: document.getElementById("password").value
    })});
    location.replace("/");
  } catch (e) { showNotice(e.message, "danger"); }
  finally { button.disabled = false; }
});
