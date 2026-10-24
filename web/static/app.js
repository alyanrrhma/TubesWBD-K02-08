(function () {
  "use strict";

  // Konfirmasi sebelum aksi destruktif: <form data-confirm="..."> atau <button data-confirm="...">
  document.addEventListener("submit", function (e) {
    var form = e.target;
    var btn = e.submitter;
    var msg = (btn && btn.dataset.confirm) || form.dataset.confirm;
    if (msg && !window.confirm(msg)) {
      e.preventDefault();
    }
  });

  // Editor SQL: Tab menyisipkan indentasi, bukan memindahkan fokus
  // Tekan Esc lalu Tab untuk keluar dari textarea (aksesibilitas keyboard)
  document.querySelectorAll("textarea.sql").forEach(function (ta) {
    var escaped = false;
    ta.addEventListener("keydown", function (e) {
      if (e.key === "Escape") { escaped = true; return; }
      if (e.key !== "Tab" || e.shiftKey || escaped) { escaped = false; return; }
      e.preventDefault();
      var s = ta.selectionStart, end = ta.selectionEnd;
      ta.value = ta.value.slice(0, s) + "  " + ta.value.slice(end);
      ta.selectionStart = ta.selectionEnd = s + 2;
    });
  });
})();