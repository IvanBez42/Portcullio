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
})();
