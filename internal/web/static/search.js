// Client-side search tab switching — fetches results via /api/search
// and swaps them into the DOM without a full page reload.
// Progressive enhancement: if JS fails, tabs are regular <a> links.
;(() => {
  const container = document.getElementById("search-results")
  if (!container) return

  const query = container.dataset.query
  if (!query) return

  const tabs = container.querySelectorAll(".p-tabs__link")
  if (!tabs.length) return

  const defaultLimit = 20
  const limitSelect = document.getElementById("search-limit")
  const getLimit = () =>
    limitSelect ? parseInt(limitSelect.value, 10) || defaultLimit : defaultLimit

  // Separators used by the Go SplitManpageTitle function, tried in order.
  const titleSeparators = [" -- ", " - ", " \u2013 ", " \u2014 "]

  const splitTitle = (title) => {
    for (const sep of titleSeparators) {
      const idx = title.indexOf(sep)
      if (idx >= 0) {
        return {
          name: title.substring(0, idx).trim(),
          desc: title.substring(idx + sep.length).trim(),
        }
      }
    }
    return { name: title, desc: "" }
  }

  const escapeHTML = (str) => {
    const div = document.createElement("div")
    div.appendChild(document.createTextNode(str))
    return div.innerHTML
  }

  const renderResultItem = (r) => {
    const parts = splitTitle(r.title)
    let meta = `man(${r.section})`
    if (parts.desc) {
      meta += ` &middot; ${escapeHTML(parts.desc)}`
    }
    return (
      `<li class="p-list__item">` +
      `<a href="${escapeHTML(r.path)}">${escapeHTML(parts.name)}</a>` +
      `<div class="mp-search-meta">${meta}</div>` +
      `</li>`
    )
  }

  const renderResults = (data) => {
    const exact = data.results.filter((r) => r.match_type !== "fuzzy")
    const fuzzy = data.results.filter((r) => r.match_type === "fuzzy")

    const nonFuzzyTotal = exact.length

    if (nonFuzzyTotal > 0) {
      const summary =
        `<div class="u-text--muted mp-search-summary">` +
        `${nonFuzzyTotal} result${nonFuzzyTotal !== 1 ? "s" : ""} found.</div>`
      const exactItems = exact.map((r) => renderResultItem(r)).join("")
      let html = `${summary}<div role="tabpanel" tabindex="0"><ul class="p-list">${exactItems}</ul>`
      if (fuzzy.length > 0) {
        const fuzzyItems = fuzzy.map((r) => renderResultItem(r)).join("")
        html +=
          `<p class="p-heading--5">Similar matches</p>` +
          `<ul class="p-list">${fuzzyItems}</ul>`
      }
      return html + "</div>"
    }

    if (fuzzy.length > 0) {
      const fuzzyItems = fuzzy.map((r) => renderResultItem(r)).join("")
      return (
        `<p class="u-text--muted mp-search-summary">No exact results found.</p>` +
        `<div role="tabpanel" tabindex="0">` +
        `<p class="p-heading--5">Similar matches</p>` +
        `<ul class="p-list">${fuzzyItems}</ul></div>`
      )
    }

    return `<p class="u-text--muted mp-search-summary">No results found.</p>`
  }

  const showSpinner = () => {
    const target = container.querySelector("[data-search-results-area]")
    if (!target) return
    target.innerHTML =
      `<div class="u-align--center" style="padding: 2rem 0">` +
      `<i class="p-icon--spinner u-animation--spin"></i> Loading\u2026</div>`
  }

  const showError = () => {
    const target = container.querySelector("[data-search-results-area]")
    if (!target) return
    target.innerHTML = `<p class="u-text--muted mp-search-summary">Search is unavailable.</p>`
  }

  // buildSearchURL constructs a URL with query, release, and limit params.
  // The limit param is omitted when it equals the default.
  const buildSearchURL = (release, limit) => {
    const params = new URLSearchParams(window.location.search)
    params.set("q", query)
    params.set("release", release)
    if (limit !== defaultLimit) {
      params.set("limit", limit)
    } else {
      params.delete("limit")
    }
    return `${window.location.pathname}?${params.toString()}`
  }

  const updateResults = (release) => {
    const target = container.querySelector("[data-search-results-area]")
    if (!target) return

    showSpinner()

    const url = `/api/search?q=${encodeURIComponent(query)}&release=${encodeURIComponent(release)}&limit=${getLimit()}`

    fetch(url)
      .then((resp) => {
        if (!resp.ok) throw new Error(`HTTP ${resp.status}`)
        return resp.json()
      })
      .then((data) => {
        if (!data.results) data.results = []
        target.innerHTML = renderResults(data)
      })
      .catch(() => {
        showError()
      })
  }

  const setActiveTab = (release) => {
    tabs.forEach((tab) => {
      const tabRelease = tab.dataset.release
      tab.setAttribute("aria-selected", tabRelease === release ? "true" : "false")
    })
  }

  const handleTabClick = (e) => {
    e.preventDefault()
    const release = e.currentTarget.dataset.release
    if (!release) return

    setActiveTab(release)
    container.dataset.release = release

    const limit = getLimit()
    history.pushState(
      { query, release, limit },
      "",
      buildSearchURL(release, limit)
    )

    updateResults(release)
  }

  // Attach click handlers to all tabs.
  tabs.forEach((tab) => {
    tab.addEventListener("click", handleTabClick)
  })

  // Handle limit selector changes.
  if (limitSelect) {
    limitSelect.addEventListener("change", () => {
      const release = container.dataset.release
      const limit = getLimit()
      container.dataset.limit = limit

      history.pushState(
        { query, release, limit },
        "",
        buildSearchURL(release, limit)
      )

      updateResults(release)
    })
  }

  // Handle back/forward navigation.
  window.addEventListener("popstate", (e) => {
    let release
    let limit
    if (e.state && e.state.release) {
      release = e.state.release
      limit = e.state.limit
    } else {
      // Fall back to URL query params.
      const params = new URLSearchParams(window.location.search)
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
  const initialRelease = container.dataset.release
  if (initialRelease) {
    history.replaceState({ query, release: initialRelease, limit: getLimit() }, "")
  }
})()
