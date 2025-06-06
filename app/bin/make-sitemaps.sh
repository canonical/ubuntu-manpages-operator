#!/bin/bash -e

###############################################################################
# This is the Ubuntu manpage repository generator and interface.
#
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

# Generate sitemaps per sitemaps.org for google.com

CONFIG="${MANPAGES_CONFIG_FILE:-/app/www/config.json}"
if [[ -z "$CONFIG" ]]; then
	echo "ERROR: Configuration file not found. Please set \$MANPAGES_CONFIG_FILE."
	exit 1
fi

PUBLIC_HTML_DIR="$(jq -r '.public_html_dir' "$CONFIG")"
PUBLIC_HTML_DIR="$(jq -r '.site' "$CONFIG")"

printf "%s\n" "INFO: Making sitemaps"

(
	cd "$PUBLIC_HTML_DIR"
	find manpages/ -type f -name "*.html" | xargs -I {} printf "%s\n" "<url><loc>$SITE/{}</loc></url>" | split -l 50000 - manpages/sitemap_

	sitemaps=$(ls manpages/sitemap_??)
	for i in $sitemaps; do
		echo "<?xml version='1.0' encoding='UTF-8'?>
<urlset xmlns='http://www.sitemaps.org/schemas/sitemap/0.9'>" >"$i.xml"
		cat "$i" >>"$i.xml"
		echo "</urlset>" >>"$i.xml"
		rm -f "$i"
	done
)
