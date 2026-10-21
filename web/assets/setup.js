(async () => {
  try {
    const status = await api("/api/setup/status");
    if (!status.setup_required) location.replace("/login");
  } catch (e) { showNotice(`Cannot verify setup state: ${e.message}`, "danger"); }
})();
document.getElementById("setup-form").addEventListener("submit", async (event) => {
  event.preventDefault(); hideNotice();
  const password = document.getElementById("password").value;
  if (password !== document.getElementById("password-confirm").value) {
    showNotice("Passwords do not match.", "warning"); return;
  }
  const button = document.getElementById("submit"); button.disabled = true;
  try {
    await api("/api/setup/complete", {
      method: "POST",
      headers: { "X-Setup-Token": document.getElementById("setup-token").value },
      body: JSON.stringify({
        username: document.getElementById("username").value,
        password,
        app_name: document.getElementById("app-name").value
      })
    });
    showNotice("Setup complete. Redirecting to login.", "success");
    setTimeout(() => location.replace("/login"), 900);
  } catch (e) { showNotice(e.message, "danger"); }
  finally { button.disabled = false; }
});
