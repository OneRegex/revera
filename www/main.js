(function () {
  "use strict";

  var root = document.documentElement;
  var themeToggle = document.getElementById("themeToggle");
  var stored = null;
  try { stored = localStorage.getItem("revera-theme"); } catch (e) { }

  if (stored === "light" || stored === "dark") {
    root.setAttribute("data-theme", stored);
  } else if (window.matchMedia && window.matchMedia("(prefers-color-scheme: light)").matches) {
    root.setAttribute("data-theme", "light");
  }

  function labelTheme() {
    if (!themeToggle) return;
    var dark = root.getAttribute("data-theme") !== "light";
    themeToggle.setAttribute("aria-label", dark ? "Switch to light theme" : "Switch to dark theme");
  }
  labelTheme();

  if (themeToggle) {
    themeToggle.addEventListener("click", function () {
      var next = root.getAttribute("data-theme") === "dark" ? "light" : "dark";
      root.setAttribute("data-theme", next);
      try { localStorage.setItem("revera-theme", next); } catch (e) { }
      labelTheme();
    });
  }

  var burger = document.getElementById("navBurger");
  var navLinks = document.getElementById("navLinks");
  if (burger && navLinks) {
    burger.addEventListener("click", function () {
      var open = navLinks.classList.toggle("is-open");
      burger.setAttribute("aria-expanded", open ? "true" : "false");
    });
    navLinks.addEventListener("click", function (e) {
      if (e.target.tagName === "A") {
        navLinks.classList.remove("is-open");
        burger.setAttribute("aria-expanded", "false");
      }
    });
  }

  var sections = Array.prototype.slice.call(document.querySelectorAll("main section[id]"));
  var navAnchors = Array.prototype.slice.call(document.querySelectorAll(".nav__links a"));
  var linkFor = {};
  navAnchors.forEach(function (a) {
    var id = a.getAttribute("href");
    if (id && id.charAt(0) === "#") linkFor[id.slice(1)] = a;
  });
  if ("IntersectionObserver" in window && sections.length) {
    var spy = new IntersectionObserver(function (entries) {
      entries.forEach(function (entry) {
        if (entry.isIntersecting) {
          navAnchors.forEach(function (a) { a.classList.remove("is-active"); });
          var active = linkFor[entry.target.id];
          if (active) active.classList.add("is-active");
        }
      });
    }, { rootMargin: "-40% 0px -55% 0px", threshold: 0 });
    sections.forEach(function (s) { spy.observe(s); });
  }

  document.querySelectorAll(".codeblock").forEach(function (block) {
    var tabs = block.querySelectorAll(".codeblock__tab");
    var panes = block.querySelectorAll(".pane");
    var copy = block.querySelector(".codeblock__copy");

    function activate(name) {
      tabs.forEach(function (t) {
        var on = t.getAttribute("data-tab") === name;
        t.classList.toggle("is-active", on);
        t.setAttribute("aria-selected", on ? "true" : "false");
      });
      panes.forEach(function (p) {
        p.classList.toggle("is-active", p.getAttribute("data-pane") === name);
      });
    }

    tabs.forEach(function (t, i) {
      t.addEventListener("click", function () { activate(t.getAttribute("data-tab")); });
      t.addEventListener("keydown", function (e) {
        var next;
        if (e.key === "ArrowRight") next = (i + 1) % tabs.length;
        else if (e.key === "ArrowLeft") next = (i - 1 + tabs.length) % tabs.length;
        else if (e.key === "Home") next = 0;
        else if (e.key === "End") next = tabs.length - 1;
        else return;
        e.preventDefault();
        tabs[next].focus();
        activate(tabs[next].getAttribute("data-tab"));
      });
    });

    if (copy) {
      var label = copy.querySelector("span");
      function flash(text) {
        if (!label) return;
        label.textContent = text;
        setTimeout(function () { label.textContent = "Copy"; }, 1400);
      }
      copy.addEventListener("click", function () {
        var pane = block.querySelector(".pane.is-active");
        if (!pane) return;
        if (!navigator.clipboard) { flash("Select and copy"); return; }
        navigator.clipboard.writeText(pane.textContent).then(
          function () { flash("Copied"); },
          function () { flash("Copy failed"); }
        );
      });
    }
  });
})();
