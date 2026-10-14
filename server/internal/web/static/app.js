// Small enhancements for espHog pages. Everything else is plain forms + htmx.
(function () {
  // Remember which template field was focused last so explorer clicks insert there.
  let lastField = null;
  document.addEventListener("focusin", (e) => {
    if (e.target.matches("[data-tpl]")) lastField = e.target;
  });

  document.addEventListener("click", (e) => {
    const btn = e.target.closest("button.path[data-path]");
    if (!btn) return;
    const field = lastField || document.querySelector("[data-tpl]:not([hidden])");
    if (!field) return;
    const path = btn.dataset.path;
    if (field.hasAttribute("data-raw")) {
      field.value = path;
    } else {
      const ins = "{{ " + path + " }}";
      const start = field.selectionStart ?? field.value.length;
      const end = field.selectionEnd ?? field.value.length;
      field.value = field.value.slice(0, start) + ins + field.value.slice(end);
      field.selectionStart = field.selectionEnd = start + ins.length;
    }
    field.focus();
    field.dispatchEvent(new Event("input", { bubbles: true }));
  });

  // Show only the fields that apply to the chosen layout / source type / button action.
  function sync() {
    const layout = document.querySelector('input[name="layout"]:checked');
    document.querySelectorAll("[data-layout]").forEach((el) => {
      el.hidden = !layout || !el.dataset.layout.split(" ").includes(layout.value);
    });
    const kind = document.querySelector('input[name="kind"]:checked');
    document.querySelectorAll("[data-kind]").forEach((el) => {
      el.hidden = !kind || el.dataset.kind !== kind.value;
    });
    document.querySelectorAll(".btn-row").forEach((row) => {
      const sel = row.querySelector("select");
      const url = row.querySelector(".webhook-url");
      if (sel && url) url.hidden = sel.value !== "webhook";
    });
  }
  document.addEventListener("change", sync);
  document.addEventListener("DOMContentLoaded", sync);


  // Device action results fade out after a few seconds.
  document.addEventListener("htmx:afterSwap", (e) => {
    const msg = e.target.querySelector && e.target.querySelector(".action-msg");
    if (msg) setTimeout(() => msg.remove(), 4000);
  });
})();
