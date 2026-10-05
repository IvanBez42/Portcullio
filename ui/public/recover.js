// Click to copy the command in the command box //
(function () {
  "use strict";
  document.querySelectorAll(".command-box").forEach(function (box) {
    var code = box.querySelector("code");
    var hint = box.querySelector(".copy-hint");
    if (!code || !hint || !navigator.clipboard) return;
    var copy = function () {
      navigator.clipboard.writeText(code.textContent).then(
        function () {
          flash("Copied");
        },
        function () {
          flash("Copy failed, select and copy manually");
        },
      );
    };
    var flash = function (message) {
      var original = hint.textContent;
      hint.textContent = message;
      setTimeout(function () {
        hint.textContent = original;
      }, 1500);
    };
    box.addEventListener("click", copy);
    box.addEventListener("keydown", function (e) {
      if (e.key === "Enter" || e.key === " ") {
        e.preventDefault();
        copy();
      }
    });
  });
})();
