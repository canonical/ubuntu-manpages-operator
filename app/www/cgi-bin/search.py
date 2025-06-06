#!/usr/bin/python3
"""This is the Ubuntu manpage repository generator and interface.
"""

###############################################################################
# Copyright (C) 2008 Canonical Ltd.
#
# This code was originally written by Dustin Kirkland <kirkland@ubuntu.com>,
# based on a framework by Kees Cook <kees@ubuntu.com>.
#
# This program is free software: you can redistribute it and/or modify
# it under the terms of the GNU General Public License as published by
# the Free Software Foundation, either version 3 of the License, or
# (at your option) any later version.
#
# This program is distributed in the hope that it will be useful,
# but WITHOUT ANY WARRANTY; without even the implied warranty of
# MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
# GNU General Public License for more details.
#
# You should have received a copy of the GNU General Public License
# along with this program.  If not, see <http://www.gnu.org/licenses/>.
#
# On Debian-based systems, the complete text of the GNU General Public
# License can be found in /usr/share/common-licenses/GPL-3
###############################################################################

import glob
import json
import os
import re
from collections import OrderedDict
from urllib.parse import parse_qsl

with open(os.environ.get("MANPAGES_CONFIG_FILE", "/app/www/config.json"), "r") as f:
    config = json.load(f)

www_root = config['public_html_dir']
versions = config['releases']
distros = versions.keys()

# Yes, there are a lot of bad variable names in this script but rather
# than touch nearly every variable in here, I think restructuring the
# script to use proper functions/methods is better, so turning off this
# lint file wide.
#
# pylint: disable=invalid-name
html = "Content-Type: text/html\n\n"

html += open(f"{www_root}/above1.html").read()
html += "Searching"
html += open(f"{www_root}/above2.html").read()

get = dict(parse_qsl(os.environ["QUERY_STRING"], encoding='utf-8'))
p1 = re.compile(r'^\.\.\/www')
p2 = re.compile(r'.*\/')
p3 = re.compile(r'\.html$')
p4 = re.compile(r'\.[0-9].*$')
p5 = re.compile(r', $')

# Title only (file name match search)
descr = [
    "",
    "Executable programs or shell commands",
    "System calls (functions provided by the kernel)",
    "Library calls (functions within program libraries)",
    "Special files (usually found in /dev)",
    "File formats and conventions eg /etc/passwd",
    "Games",
    "Miscellaneous (including macro  packages  and  conventions)",
    "System administration commands (usually only for root)",
    "Kernel routines [Non standard]"
]

t = ""
if "q" in get:
    t = get["q"]
x = 1
y = 9
extra = ""
# User might have specified the section
p = re.compile(r'(.*)\.([1-9])(.*)$')
n = p.search(t)
if n:
    t = n.group(1)
    x = int(n.group(2))
    y = x + 1
    extra = n.group(3)

p = re.compile(r'[^\.a-zA-Z0-9\/_\:\+@-]')
t = p.sub('', t)
title_html = "<script>document.forms[0].q.value='" + t + "';</script>"

if "lr" in get:
    lr = get["lr"]
    p = re.compile(r'^lang_')
    lr = p.sub('', lr)
    p = re.compile(r'[^a-zA-Z-]')
    lr = p.sub('', lr)
    p = re.compile(r'[-]')
    lr = p.sub('_', lr)
else:
    lr = "en"

title_html += ("</div></div><div class='p-strip u-no-padding--top'>"
               "<div class='row'>"
               "<br><table><tr>"
               "<td><table cellspacing=0 cellpadding=5><thead><tr>")
for d in distros:
    title_html += "<th>%s<br><small>%s</small></th>" % (
        d, versions[d])
title_html += "<th>Section Description</th></thead></tr>"
matches = 0
for i in range(x, y):
    title_html += "<tr>"
    for d in distros:
        color = "lightgrey"
        path = f"{www_root}/manpages/{d}/{lr}/man{i}/{t}.{i}{extra}*.html"
        title_html += "<td align=center>"
        dot = "."
        for g in glob.glob(path):

            matches += 1
            dot = ""
            color = "black"

            href_path = p1.sub('', g.replace(config["public_html_dir"], ""))
            page = p2.sub('', g)
            page = p3.sub('', page)
            page = p4.sub('', page)
            title_html += '<a href="%s" style="text-decoration:none">' % (
                href_path)
            title_html += '%s(%d)</a>, ' % (page, i)
        title_html = p5.sub('', title_html)
        title_html += dot + "</td>"
    title_html += '<td><font color="%s">(%d) - <small>%s</small></td></tr>' % (
        color, i, descr[i])
title_html += "</table></td></tr></table><br>"
if matches > 0:
    if "titles" in get and get["titles"] == "404":
        # If we were sent here by a 404-not-found, and we have at least one
        # match, redirect the user to the last page in our list
        html += "<script>location.replace('" + href_path + "');</script>"
    else:
        # Otherwise, a normal title search, display the title table
        html += title_html
else:
    # But if we do not find any matching titles, do a full text search
    html += "</div></div><section class='p-strip u-no-padding--top'><div class='row'><h2>No matching titles found</h2>"

html += open(f"{www_root}/below.html").read()
print(html)  # pylint: disable=superfluous-parens
