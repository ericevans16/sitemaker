/* <site-search> — client-side instant search backed by /search-index.json. */
(function () {
  var indexCache = null;

  function loadIndex(url) {
    if (indexCache) return indexCache;
    indexCache = fetch(url, { credentials: "same-origin" }).then(function (r) {
      if (!r.ok) throw new Error("Failed to load search index");
      return r.json();
    });
    return indexCache;
  }

  function escapeHTML(s) {
    return String(s || "")
      .replace(/&/g, "&amp;")
      .replace(/</g, "&lt;")
      .replace(/>/g, "&gt;")
      .replace(/"/g, "&quot;");
  }

  class SiteSearch extends HTMLElement {
    connectedCallback() {
      var self = this;
      var input = this.querySelector(".search-input");
      var status = this.querySelector(".search-status");
      var results = this.querySelector(".search-results");
      if (!input || !results) return;

      var indexURL = this.getAttribute("data-index") || "/search-index.json";
      var totalText = status ? status.textContent : "";

      function render(items, query) {
        if (!query) {
          results.hidden = true;
          results.innerHTML = "";
          if (status) status.textContent = totalText;
          return;
        }
        var tokens = query.toLowerCase().split(/\s+/).filter(Boolean);
        var matched = [];
        for (var i = 0; i < items.length; i++) {
          var it = items[i];
          var hayTitle = (it.title || "").toLowerCase();
          var hayMeta = ((it.category || "") + " " + (it.tags || []).join(" ")).toLowerCase();
          var hayBody = ((it.excerpt || "") + " " + (it.content || "")).toLowerCase();
          var score = 0;
          var ok = true;
          for (var t = 0; t < tokens.length; t++) {
            var tok = tokens[t];
            var s = 0;
            if (hayTitle.indexOf(tok) !== -1) s += 10;
            if (hayMeta.indexOf(tok) !== -1) s += 5;
            if (hayBody.indexOf(tok) !== -1) s += 1;
            if (s === 0) {
              ok = false;
              break;
            }
            score += s;
          }
          if (ok) matched.push({ item: it, score: score });
        }
        matched.sort(function (a, b) { return b.score - a.score; });

        if (status) {
          status.textContent = matched.length + (matched.length === 1 ? " match" : " matches") + " for \"" + query + "\"";
        }
        if (matched.length === 0) {
          results.hidden = true;
          results.innerHTML = "";
          return;
        }
        var html = '<div class="post-list-head" aria-hidden="true"><span>Title</span><span>Date</span></div>';
        for (var j = 0; j < matched.length; j++) {
          var p = matched[j].item;
          html += '<article class="post-list-item">' +
            '<a href="' + escapeHTML(p.url) + '">' + escapeHTML(p.title) + '</a>' +
            (p.date ? '<time datetime="' + escapeHTML(p.date) + '">' + escapeHTML(p.date) + '</time>' : '<span></span>') +
            (p.excerpt ? '<p class="search-snippet">' + escapeHTML(p.excerpt) + '</p>' : '') +
            '</article>';
        }
        results.innerHTML = html;
        results.hidden = false;
      }

      input.addEventListener("input", function () {
        var q = input.value.trim();
        loadIndex(indexURL).then(function (items) {
          render(items, q);
        }).catch(function () {
          if (status) status.textContent = "Search index unavailable.";
        });
      });

      this._onKey = function (e) {
        if (e.key === "/" && document.activeElement !== input && !e.ctrlKey && !e.metaKey && !e.altKey) {
          var tag = document.activeElement && document.activeElement.tagName;
          if (tag !== "INPUT" && tag !== "TEXTAREA") {
            e.preventDefault();
            input.focus();
          }
        }
      };
      window.addEventListener("keydown", this._onKey);

      var params = new URLSearchParams(location.search);
      if (params.get("q")) {
        input.value = params.get("q");
        input.dispatchEvent(new Event("input"));
      }
      self._input = input;
    }

    disconnectedCallback() {
      if (this._onKey) {
        window.removeEventListener("keydown", this._onKey);
        this._onKey = null;
      }
    }
  }

  if (!customElements.get("site-search")) {
    customElements.define("site-search", SiteSearch);
  }
})();
