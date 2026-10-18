// Shared by the Studio and the CLI delivery page. A language picker is a
// .menu-anchor holding a button that opens a menu of languages, the current one
// checked. Arrow keys move through the menu; Escape or a click outside closes it.
const languageMenus = (() => {
  "use strict";
  const anchors = () => Array.from(document.querySelectorAll(".menu-anchor"));
  const setup = () => {
    for (const anchor of anchors()) {
      const toggle = anchor.querySelector("[aria-haspopup=menu]");
      const menu = anchor.querySelector("[role=menu]");
      const items = Array.from(menu.querySelectorAll("[role=menuitemradio]"));
      const set = (open, focus) => {
        menu.hidden = !open;
        toggle.setAttribute("aria-expanded", String(open));
        if (open) (items.find((item) => item.getAttribute("aria-checked") === "true") || items[0]).focus();
        else if (focus) toggle.focus();
      };
      toggle.addEventListener("click", () => set(menu.hidden, false));
      menu.addEventListener("keydown", (event) => {
        const index = items.indexOf(document.activeElement);
        if (event.key === "Escape") {
          event.preventDefault();
          set(false, true);
        } else if (event.key === "ArrowDown" || event.key === "ArrowUp") {
          event.preventDefault();
          items[(index + (event.key === "ArrowDown" ? 1 : -1) + items.length) % items.length].focus();
        } else if (event.key === "Tab") {
          set(false, false);
        }
      });
      for (const item of items) item.addEventListener("click", () => set(false, true));
      document.addEventListener("click", (event) => {
        if (!menu.hidden && !anchor.contains(event.target)) set(false, false);
      });
    }
  };
  // Shows the checked language's own name on pickers that display it.
  const label = () => {
    for (const anchor of anchors()) {
      const current = anchor.querySelector("[data-language-current]");
      const checked = anchor.querySelector("[role=menuitemradio][aria-checked=true]");
      if (!current || !checked) continue;
      current.textContent = checked.querySelector("span").textContent;
      if (checked.lang) current.lang = checked.lang;
      else current.removeAttribute("lang");
    }
  };
  return {setup, label};
})();
