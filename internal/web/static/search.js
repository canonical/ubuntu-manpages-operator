// Client-side search tab switching — fetches results via /api/search
// and swaps them into the DOM without a full page reload.
// Progressive enhancement: if JS fails, tabs are regular <a> links.
;(function () {
  var container = document.getElementById("search-results")
  if (!container) return

  var query = container.dataset.query
  if (!query) return

  var tabs = container.querySelectorAll(".p-tabs__link")
  if (!tabs.length) return

  var defaultLimit = 20
  var limitSelect = document.getElementById("search-limit")
  function getLimit() {
    return limitSelect ? parseInt(limitSelect.value, 10) || defaultLimit : defaultLimit
  }

  // Separators used by the Go SplitManpageTitle function, tried in order.
  var titleSeparators = [" -- ", " - ", " \u2013 ", " \u2014 "]

  function splitTitle(title) {
    for (var i = 0; i < titleSeparators.length; i++) {
      var idx = title.indexOf(titleSeparators[i])
      if (idx >= 0) {
        return {
          name: title.substring(0, idx).trim(),
          desc: title.substring(idx + titleSeparators[i].length).trim(),
        }
      }
    }
    return { name: title, desc: "" }
  }

  function escapeHTML(str) {
    var div = document.createElement("div")
    div.appendChild(document.createTextNode(str))
    return div.innerHTML
  }

  function renderResultItem(r) {
    var parts = splitTitle(r.title)
    var meta = "man(" + r.section + ")"
    if (parts.desc) {
      meta += " &middot; " + escapeHTML(parts.desc)
    }
    return (
      '<li class="p-list__item">' +
      '<a href="' +
      escapeHTML(r.path) +
      '">' +
      escapeHTML(parts.name) +
      "</a>" +
      '<div class="mp-search-meta">' +
      meta +
      "</div>" +
      "</li>"
    )
  }

  function renderResults(data) {
    // Split results into non-fuzzy and fuzzy.
    var exact = []
    var fuzzy = []
    for (var i = 0; i < data.results.length; i++) {
      var r = data.results[i]
      if (r.match_type === "fuzzy") {
        fuzzy.push(r)
      } else {
        exact.push(r)
      }
    }

    var html = ""
    var nonFuzzyTotal = exact.length

    if (nonFuzzyTotal > 0) {
      html +=
        '<div class="u-text--muted mp-search-summary">' +
        nonFuzzyTotal +
        " result" +
        (nonFuzzyTotal !== 1 ? "s" : "") +
        " found.</div>"
      html += '<div role="tabpanel" tabindex="0"><ul class="p-list">'
      for (var i = 0; i < exact.length; i++) {
        html += renderResultItem(exact[i])
      }
      html += "</ul>"
      if (fuzzy.length > 0) {
        html += '<p class="p-heading--5">Similar matches</p>'
        html += '<ul class="p-list">'
        for (var j = 0; j < fuzzy.length; j++) {
          html += renderResultItem(fuzzy[j])
        }
        html += "</ul>"
      }
      html += "</div>"
    } else if (fuzzy.length > 0) {
      html += '<p class="u-text--muted mp-search-summary">No exact results found.</p>'
      html += '<div role="tabpanel" tabindex="0">'
      html += '<p class="p-heading--5">Similar matches</p>'
      html += '<ul class="p-list">'
      for (var j = 0; j < fuzzy.length; j++) {
        html += renderResultItem(fuzzy[j])
      }
      html += "</ul></div>"
    } else {
      html += '<p class="u-text--muted mp-search-summary">No results found.</p>'
    }

    return html
  }

  function showSpinner() {
    var target = container.querySelector("[data-search-results-area]")
    if (!target) return
    target.innerHTML =
      '<div class="u-align--center" style="padding: 2rem 0">' +
      '<i class="p-icon--spinner u-animation--spin"></i>' +
      " Loading\u2026" +
      "</div>"
  }

  function showError() {
    var target = container.querySelector("[data-search-results-area]")
    if (!target) return
    target.innerHTML = '<p class="u-text--muted mp-search-summary">Search is unavailable.</p>'
  }

  // buildSearchURL constructs a URL with query, release, and limit params.
  // The limit param is omitted when it equals the default.
  function buildSearchURL(release, limit) {
    var params = new URLSearchParams(window.location.search)
    params.set("q", query)
    params.set("release", release)
    if (limit !== defaultLimit) {
      params.set("limit", limit)
    } else {
      params.delete("limit")
    }
    return window.location.pathname + "?" + params.toString()
  }

  function updateResults(release) {
    var target = container.querySelector("[data-search-results-area]")
    if (!target) return

    showSpinner()

    var url =
      "/api/search?q=" +
      encodeURIComponent(query) +
      "&release=" +
      encodeURIComponent(release) +
      "&limit=" +
      getLimit()

    fetch(url)
      .then(function (resp) {
        if (!resp.ok) throw new Error("HTTP " + resp.status)
        return resp.json()
      })
      .then(function (data) {
        if (!data.results) data.results = []
        target.innerHTML = renderResults(data)
      })
      .catch(function () {
        showError()
      })
  }

  function setActiveTab(release) {
    tabs.forEach(function (tab) {
      var tabRelease = tab.dataset.release
      tab.setAttribute("aria-selected", tabRelease === release ? "true" : "false")
    })
  }

  function handleTabClick(e) {
    e.preventDefault()
    var release = this.dataset.release
    if (!release) return

    setActiveTab(release)
    container.dataset.release = release

    var limit = getLimit()
    history.pushState(
      { query: query, release: release, limit: limit },
      "",
      buildSearchURL(release, limit)
    )

    updateResults(release)
  }

  // Attach click handlers to all tabs.
  tabs.forEach(function (tab) {
    tab.addEventListener("click", handleTabClick)
  })

  // Handle limit selector changes.
  if (limitSelect) {
    limitSelect.addEventListener("change", function () {
      var release = container.dataset.release
      var limit = getLimit()
      container.dataset.limit = limit

      history.pushState(
        { query: query, release: release, limit: limit },
        "",
        buildSearchURL(release, limit)
      )

      updateResults(release)
    })
  }

  // Handle back/forward navigation.
  window.addEventListener("popstate", function (e) {
    var release
    var limit
    if (e.state && e.state.release) {
      release = e.state.release
      limit = e.state.limit
    } else {
      // Fall back to URL query params.
      var params = new URLSearchParams(window.location.search)
      release = params.get("release")
      limit = parseInt(params.get("limit"), 10) || defaultLimit
    }
    if (limit && limitSelect) {
      limitSelect.value = String(limit)
      container.dataset.limit = limit
    }
    if (release) {
      setActiveTab(release)
      container.dataset.release = release
      updateResults(release)
    }
  })

  // Replace the initial history entry so popstate works correctly.
  var initialRelease = container.dataset.release
  if (initialRelease) {
    history.replaceState({ query: query, release: initialRelease, limit: getLimit() }, "")
  }
})()
