// Updates the slider for size_mb //
(function () {
  "use strict";
  var slider = document.getElementById("size_mb_slider");
  var box = document.getElementById("size_mb");
  if (!slider || !box) return;
  slider.addEventListener("input", function () {
    box.value = slider.value;
  });
  box.addEventListener("input", function () {
    slider.value = box.value;
  });

  // Re-syncs the size control's max/hint to whichever locker is selected //
  var lockerSelect = document.getElementById("locker");
  var hint = document.getElementById("space-hint");
  if (!lockerSelect) return;
  lockerSelect.addEventListener("change", function () {
    var option = lockerSelect.selectedOptions && lockerSelect.selectedOptions[0];
    var raw = option ? option.dataset.available : "";
    var availableMB = parseInt(raw, 10);
    var known = Number.isFinite(availableMB) && availableMB > 0;
    var max = known ? availableMB : 1048576; // 1 TB fallback, unconstrained in practice

    slider.max = max;
    box.max = max;
    if (Number(slider.value) > max) slider.value = max;
    if (Number(box.value) > max) box.value = max;

    if (hint) {
      hint.textContent = known
        ? formatMB(availableMB) + " free on the vault storage disk"
        : "Free space unavailable -- size isn't checked against it here.";
    }
  });

  function formatMB(mb) {
    if (mb >= 1024) return (mb / 1024).toFixed(1) + " GB";
    return mb + " MB";
  }
})();
