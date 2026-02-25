// Drawer toggle for mobile side navigation.
;(function () {
  var drawer = document.querySelector("[class*='p-side-navigation']")
  if (!drawer) return
  document.querySelectorAll(".js-drawer-toggle").forEach(function (toggle) {
    toggle.addEventListener("click", function (e) {
      e.preventDefault()
      drawer.classList.toggle("is-expanded")
    })
  })
  document.addEventListener("keyup", function (e) {
    if (e.key === "Escape" && drawer.classList.contains("is-expanded")) {
      drawer.classList.remove("is-expanded")
    }
  })
})()

// Prefill search input from URL query parameter.
;(function () {
  var input = document.querySelector("#search-docs .p-search-box__input")
  if (!input) return
  var q = new URLSearchParams(window.location.search).get("q")
  if (q) input.value = q
})()

// Tab switching for search result groups.
;(function () {
  document.querySelectorAll("[role='tab']").forEach(function (tab) {
    tab.addEventListener("click", function () {
      var id = this.id
      document.querySelectorAll("[role='tab']").forEach(function (t) {
        var active = t.id === id
        t.setAttribute("aria-selected", active)
        t.tabIndex = active ? 0 : -1
      })
      document.querySelectorAll("[role='tabpanel']").forEach(function (p) {
        if (p.getAttribute("aria-labelledby") === id) {
          p.removeAttribute("hidden")
        } else {
          p.setAttribute("hidden", "hidden")
        }
      })
    })
  })
})()

// Per-page selector for browse listings.
;(function () {
  var sel = document.getElementById("browse-per-page")
  if (!sel) return
  sel.addEventListener("change", function () {
    var params = new URLSearchParams(window.location.search)
    var perPage = parseInt(this.value, 10)
    params.delete("page")
    if (perPage === 25) {
      params.delete("per_page")
    } else {
      params.set("per_page", perPage)
    }
    var qs = params.toString()
    window.location.href = sel.dataset.path + (qs ? "?" + qs : "")
  })
})()
