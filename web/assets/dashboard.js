let table;
const modal = new bootstrap.Modal(document.getElementById("item-modal"));
const escapeHtml = $.fn.dataTable.render.text();
function fmtDate(value) { return value ? new Date(value).toLocaleString() : ""; }
async function loadItems() {
  const response = await api("/api/v1/items?limit=100&offset=0");
  table.clear().rows.add(response.data).draw();
}
function resetForm() {
  document.getElementById("item-form").reset();
  document.getElementById("item-id").value = "";
  document.getElementById("item-modal-title").textContent = "Create item";
}
(async function init() {
  try {
    const me = await api("/api/auth/me");
    document.getElementById("signed-in").textContent = `Signed in as ${me.username}`;
    table = new DataTable("#items-table", {
      data: [],
      columns: [
        { data: "id" },
        { data: "name", render: escapeHtml },
        { data: "description", render: escapeHtml, defaultContent: "" },
        { data: "updated_at", render: (v) => escapeHtml.display(fmtDate(v)) },
        { data: null, orderable: false, searchable: false, render: (_v, _t, row) =>
          `<button class="btn btn-sm btn-outline-primary me-1 edit" data-id="${Number(row.id)}" aria-label="Edit item"><i class="fa-solid fa-pen"></i></button><button class="btn btn-sm btn-outline-danger delete" data-id="${Number(row.id)}" aria-label="Delete item"><i class="fa-solid fa-trash"></i></button>` }
      ],
      order: [[0, "desc"]], pageLength: 25, responsive: false,
      language: { emptyTable: "No items yet. Create your first item." }
    });
    await loadItems();
  } catch (e) { if (e.message.includes("authentication")) location.replace("/login"); else showNotice(e.message, "danger"); }
})();
document.getElementById("create-item").addEventListener("click", () => { resetForm(); modal.show(); });
document.getElementById("item-form").addEventListener("submit", async (event) => {
  event.preventDefault(); hideNotice();
  const id = document.getElementById("item-id").value;
  const payload = { name: document.getElementById("item-name").value, description: document.getElementById("item-description").value };
  const button = document.getElementById("save-item"); button.disabled = true;
  try {
    await api(id ? `/api/v1/items/${id}` : "/api/v1/items", { method: id ? "PUT" : "POST", body: JSON.stringify(payload) });
    modal.hide(); await loadItems(); showNotice(id ? "Item updated." : "Item created.", "success");
  } catch (e) { showNotice(e.message, "danger"); }
  finally { button.disabled = false; }
});
document.querySelector("#items-table tbody").addEventListener("click", async (event) => {
  const edit = event.target.closest(".edit"); const del = event.target.closest(".delete");
  if (edit) {
    try {
      const item = await api(`/api/v1/items/${edit.dataset.id}`);
      resetForm(); document.getElementById("item-id").value = item.id;
      document.getElementById("item-name").value = item.name;
      document.getElementById("item-description").value = item.description;
      document.getElementById("item-modal-title").textContent = `Edit item #${item.id}`; modal.show();
    } catch (e) { showNotice(e.message, "danger"); }
  }
  if (del) {
    if (!confirm(`Delete item #${del.dataset.id}? This cannot be undone.`)) return;
    try { await api(`/api/v1/items/${del.dataset.id}`, { method: "DELETE" }); await loadItems(); showNotice("Item deleted.", "success"); }
    catch (e) { showNotice(e.message, "danger"); }
  }
});
document.getElementById("logout").addEventListener("click", async () => {
  try { await api("/api/auth/logout", { method: "POST" }); location.replace("/login"); }
  catch (e) { showNotice(e.message, "danger"); }
});
