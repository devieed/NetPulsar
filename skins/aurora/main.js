(() => {
  const root = document.querySelector(".aurora");
  if (!root) return;
  root.addEventListener("mousemove", (e) => {
    const rect = root.getBoundingClientRect();
    root.style.setProperty("--mx", (e.clientX - rect.left) + "px");
    root.style.setProperty("--my", (e.clientY - rect.top) + "px");
  });
})();
