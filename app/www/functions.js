// This is the Ubuntu manpage repository generator and interface.
//
// Copyright (C) 2008 Canonical Ltd.
//
// This code was originally written by Dustin Kirkland <kirkland@ubuntu.com>,
// based on a framework by Kees Cook <kees@ubuntu.com>.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with this program.  If not, see <http://www.gnu.org/licenses/>.
//
// On Debian-based systems, the compvare text of the GNU General Public
// License can be found in /usr/share/common-licenses/GPL-3

function distroAndSection() {
  var parent = document.getElementById("distroAndSection");
  var output = "";
  if (parent) {
    var parts = window.location.pathname.split("/");
    if (parts.length < 5) {
      return;
    }
    var distro = parts[2];
    var section = parts[3];
    section = section.replace(/^man/, "");
    if (!(section >= 1 && section <= 9)) {
      section = parts[4];
      section = section.replace(/^man/, "");
    }
    if (distro.length > 0) {
      output += '<a href="../">' + distro + "</a> ";
      if (section.length > 0) {
        output += '(<a href="../man' + section + '">' + section + "</a>) ";
      }
    }
    var gz_href = location.href.replace(/\/manpages\//, "\/manpages.gz\/");
    gz_href = gz_href.replace(/\/en\//, "\/");
    gz_href = gz_href.replace(/\.html$/, "\.gz");
    var gz = gz_href.replace(/^.*\//, "");
    output += '<a href="' + gz_href + '">' + gz + "</a><br>";
    parent.innerHTML = output;
  }
}
distroAndSection();

function tocGen(id, writeTo) {
  var id = id;
  var writeOut = "";
  var parentOb = document.getElementById(id);
  var headers = parentOb.querySelectorAll("h3, h4, h5, h6");
  if (headers.length > 0) {
    writeOut += '<ul class="p-list--divided">';
    for (var i = 0; i < headers.length; i++) {
      var innerText = headers[i].innerText.toLowerCase();
      headers[i].setAttribute("id", innerText);
      writeOut += '<li class="p-list__item">';
      writeOut += '<a href="#' + innerText + '">' + innerText + "</a>";
      writeOut += "</li>";
    }
    writeOut += "</ul>";
    document.getElementById(writeTo).innerHTML = writeOut;
    document.getElementById("tableWrapper").classList.add("col-9");
    document.getElementById("toc").classList.remove("u-hide");
  }
}
tocGen("main-content", "toc");

function highlightNav() {
  var pathname = window.location.pathname;
  var pathnameSplit = pathname.split("/");
  var currentURLParical = "/" + pathnameSplit[1] + "/" + pathnameSplit[2];
  var navigationItems = document.querySelectorAll(".p-navigation__links a");
  for (var i = 0; i < navigationItems.length; i++) {
    var href = navigationItems[i].getAttribute("href");
    if (href.includes(currentURLParical)) {
      navigationItems[i].parentNode.classList.add("is-selected");
    }
  }
}

function renderNav(versions) {
  var navigationContainer = document.getElementById("navigation-container");
  var navigationOutput = "";

  for (var i = 0; i < versions.length; i++) {
    if (location.href.match("\.html$")) {
      href = location.href;
      href = href.replace(
        /\/manpages\/[^\/]*/,
        "/manpages/" + versions[i]["name"],
      );
      navigationOutput +=
        "<li class='p-navigation__link is-active'><a href='" +
        href +
        "'>" +
        versions[i]["number"] +
        "</a></li>";
    } else {
      navigationOutput +=
        "<li class='p-navigation__link'><a href='/manpages/" +
        versions[i]["name"] +
        "'>" +
        versions[i]["number"] +
        "</a></li>";
    }
  }
  navigationContainer.innerHTML = navigationOutput;
  highlightNav();
}

function updateLocalStorage() {
  return fetch("/config.json")
    .then((response) => response.json())
    .then((data) => {
      // Get the releases from the config file
      var releases = new Map(Object.entries(data.releases));

      // Mutate releases into the existing format that was statically defined here.
      var versions = Array.from(releases).map(([name, number]) => {
        // Make sure LTS versions have "LTS" appended.
        let [maj, min] = number.split(".");
        if (Number(maj) % 2 == 0 && min == "04") {
          return { name: name, number: number + " LTS" };
        }
        return { name, number };
      });
      localStorage.setItem("versions", JSON.stringify(versions));
    })
    .catch((error) => console.error(error));
}

function navbar() {
  let versions = localStorage.getItem("versions");
  // If we already have the versions in storage, render using those cached versions,
  // but also update the versions in the background for the next request.
  if (versions) {
    renderNav(JSON.parse(versions));
    updateLocalStorage();
    return;
  }

  // If we didn't find the versions in storage, fetch them, and then
  // render the nav.
  updateLocalStorage().then(() => {
    versions = localStorage.getItem("versions");
    renderNav(JSON.parse(versions));
  });
}
navbar();
