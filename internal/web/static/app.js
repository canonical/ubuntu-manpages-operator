// Drawer toggle for mobile side navigation.
;(() => {
  const drawer = document.querySelector("[class*='p-side-navigation']")
  if (!drawer) return
  document.querySelectorAll(".js-drawer-toggle").forEach((toggle) => {
    toggle.addEventListener("click", (e) => {
      e.preventDefault()
      drawer.classList.toggle("is-expanded")
    })
  })
  document.addEventListener("keyup", (e) => {
    if (e.key === "Escape" && drawer.classList.contains("is-expanded")) {
      drawer.classList.remove("is-expanded")
    }
  })
})()

// Navigation dropdown toggle.
;(() => {
  const dropdowns = document.querySelectorAll(".p-navigation__item--dropdown-toggle")
  dropdowns.forEach((dropdown) => {
    const toggle = dropdown.querySelector(".p-navigation__link")
    if (!toggle) return
    toggle.addEventListener("click", (e) => {
      e.preventDefault()
      const isOpen = dropdown.classList.contains("is-active")
      // Close all dropdowns first.
      dropdowns.forEach((d) => {
        d.classList.remove("is-active")
        const menu = d.querySelector(".p-navigation__dropdown--right, .p-navigation__dropdown")
        if (menu) menu.setAttribute("aria-hidden", "true")
      })
      if (!isOpen) {
        dropdown.classList.add("is-active")
        const menu = dropdown.querySelector(
          ".p-navigation__dropdown--right, .p-navigation__dropdown"
        )
        if (menu) menu.setAttribute("aria-hidden", "false")
      }
    })
  })
  document.addEventListener("click", (e) => {
    if (!e.target.closest(".p-navigation__item--dropdown-toggle")) {
      dropdowns.forEach((d) => {
        d.classList.remove("is-active")
        const menu = d.querySelector(".p-navigation__dropdown--right, .p-navigation__dropdown")
        if (menu) menu.setAttribute("aria-hidden", "true")
      })
    }
  })
})()

// Prefill search input from URL query parameter.
;(() => {
  const input = document.querySelector("#search-docs .p-search-box__input")
  if (!input) return
  const q = new URLSearchParams(window.location.search).get("q")
  if (q) input.value = q
})()

// Per-page selector for browse listings.
;(() => {
  const sel = document.getElementById("browse-per-page")
  if (!sel) return
  sel.addEventListener("change", () => {
    const params = new URLSearchParams(window.location.search)
    const perPage = parseInt(sel.value, 10)
    params.delete("page")
    if (perPage === 25) {
      params.delete("per_page")
    } else {
      params.set("per_page", perPage)
    }
    const qs = params.toString()
    window.location.href = sel.dataset.path + (qs ? `?${qs}` : "")
  })
})()
