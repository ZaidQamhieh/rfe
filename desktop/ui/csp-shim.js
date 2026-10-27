// The window's CSP is `style-src 'self'`, which drops style="" attributes. The attribute text is
// still in the DOM, so every element that gets one is given the same declarations through the
// CSSOM, which the policy allows. This runs before the page is painted (MutationObserver tasks
// are microtasks).
(function () {
  const apply = (el) => {
    const css = el.getAttribute && el.getAttribute("style");
    if (css && el.style && el.style.cssText !== css) el.style.cssText = css;
  };
  const walk = (node) => {
    if (node.nodeType !== 1) return;
    apply(node);
    for (const c of node.querySelectorAll("[style]")) apply(c);
  };
  new MutationObserver((list) => {
    for (const m of list) {
      if (m.type === "attributes") apply(m.target);
      else for (const n of m.addedNodes) walk(n);
    }
  }).observe(document, { subtree: true, childList: true, attributes: true, attributeFilter: ["style"] });
})();
