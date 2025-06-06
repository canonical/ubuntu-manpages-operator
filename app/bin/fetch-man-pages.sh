#!/bin/bash -e
# shellcheck disable=SC2129

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

CONFIG="${MANPAGES_CONFIG_FILE:-/app/www/config.json}"
if [[ -z "$CONFIG" ]]; then
	echo "ERROR: Configuration file not found. Please set \$MANPAGES_CONFIG_FILE."
	exit 1
fi

PUBLIC_HTML_DIR="$(jq -r '.public_html_dir' "$CONFIG")"

TEMPDIR=$(mktemp -d -t manpages-fetch-XXXXXX)
trap 'rm -rf $TEMPDIR 2>/dev/null || true' EXIT HUP INT QUIT TERM

DIST="$1"
PKGURL="$2"
PKG=$(basename "$PKGURL")
NAME=$(echo "$PKG" | awk -F_ '{print $1}')

#shellcheck disable=SC2001
NAME_AND_VER=$(echo "$PKG" | sed "s/\.deb$//")
DEB="$TEMPDIR/$PKG"

echo "INFO ($(date '+%H:%M:%S.%N')) - ${DIST}: fetching: $PKGURL"
curl --silent "$PKGURL" >"$DEB"

DESTDIR="$PUBLIC_HTML_DIR/manpages/$DIST"
DESTDIRGZ="$PUBLIC_HTML_DIR/manpages.gz/$DIST"

export W3MMAN_MAN='man --no-hyphenation'
export MAN_KEEP_FORMATTING=1

echo "INFO ($(date '+%H:%M:%S.%N')) - ${DIST}: Looking for manpages in [$DEB]"
# The .*man bit is to handle postgres' inane manpage installation
man=$(dpkg-deb -c "$DEB" | grep -E " \./usr/share.*/man/.*\.[0-9][a-zA-Z0-9\.\-]*\.gz$" | sed -e "s/^.*\.\//\.\//" -e "s/ \-> /\->/")

# Exit immediately if this package does not contain manpages
if [ -z "$man" ]; then
	echo "INFO ($(date '+%H:%M:%S.%N')) - ${DIST}: No manpages: [$DIST] [$PKG]"
	# Touch the cache file so we don't look again until package updated
	sha1sum "$DEB" | awk '{ print $1 }' >"$DESTDIR/.cache/$NAME"
	exit 0
fi

src_pkg=$(dpkg -I "$DEB" | grep -E "^ Package: |^ Source: " | tail -n1 | sed "s/^.*: //")

dpkg-deb -x "$DEB" "$TEMPDIR"
for i in $man; do
	#printf "%s\n" "DEBUG: Considering entry [$i]"
	i=$(printf "%s" "$i" | sed "s/^.*\.\///")
	if printf "%s" "$i" | grep -qs "\->"; then
		SYMLINK=1
		symlink_src_html=$(printf "%s" "$i" | sed -e "s/^.*\->//" -e "s/\.gz$/\.html/")
		i=$(printf "%s" "$i" | sed "s/\->.*$//")
		#printf "%s\n" "DEBUG: [$i] is a symbolic link"
	else
		SYMLINK=0
	fi
	manpage="$TEMPDIR/$i"
	i=$(printf "%s" "$i" | sed -e "s/usr\/share.*\/man\///i" -e "s/\.gz$//")
	#printf "%s\n" "DEBUG: Considering manpage [$i]"
	# shellcheck disable=SC2166
	if [ ! -s "$manpage" -o -z "$i" ] && [ "$SYMLINK" = "0" ]; then
		#printf "%s\n" "DEBUG: Skipping empty manpage [$manpage]"
		continue
	fi
	out="$DESTDIR"/"$i".html
	outgz=$(dirname "$DESTDIRGZ"/"$i")
	mkdir -p "$(dirname "$out")" "$outgz" >/dev/null || true
	if [ "$SYMLINK" = "1" ]; then
		ln -f -s "$symlink_src_html" "$out"
		printf "%s\n" "INFO ($(date '+%H:%M:%S.%N')) - ${DIST}: Created symlink [$out]"
	else
		if LN=$(zcat "$manpage" | head -n1 | grep "^\.so "); then
			LN=$(printf "%s" "$LN" | sed -e 's/^\.so /\.\.\//' -e 's/\/\.\.\//\//g' -e 's/$/\.html/')
			ln -f -s "$LN" "$out"
			printf "INFO ($(date '+%H:%M:%S.%N')) - ${DIST}: Created symlink [%s]" "$out"
		else

			# Bug when setting COLUMNS=100 https://bugs.launchpad.net/ubuntu/+source/man-db/+bug/2095541
			BODY=$(COLUMNS=115 /usr/lib/w3m/cgi-bin/w3mman2html.cgi "local=$manpage" | grep -A 1000000 "^<b>" | sed -e '/<\/body>/,+100 d' -e 's:^<b>\(.*\)</b>$:</pre><h4><b>\1</b></h4><pre>:g' -e 's:<a href="file\:///[^?]*?\([^(]*\)(\([^)]*\))">:<a href="../man\2/\1.\2.html">:g')
			TITLE=$(printf "%s" "$BODY" | head -n2 | tail -n1 | sed "s/<[^>]\+>//g")
			BIN_PKG=$(printf "%s" "$NAME_AND_VER" | sed s/_.*$//g)
			PKG_LINK="https://launchpad.net/ubuntu/$DIST/+package/$BIN_PKG"
			BUG_LINK="https://bugs.launchpad.net/ubuntu/+source/$src_pkg/+filebug-advanced"
			echo "<!--#include virtual='/above1.html' -->
$TITLE
<!--#include virtual='/above2.html' -->
Provided by: <a href='$PKG_LINK'>$NAME_AND_VER</a> <a href='$BUG_LINK' title='Report a bug in the content of this documentation'><img src='/assets/img/bug.png' alt='bug' border=0></a><br><br><pre>
$BODY
</pre><!--#include virtual='/below.html' -->" >"$out"

			printf "%s\n" "INFO ($(date '+%H:%M:%S.%N')) - ${DIST}: Created manpage [$out]"
		fi
	fi
	mv -f "$manpage" "$outgz"
	if [ ! -s "$out" ]; then
		# Remove if it's an empty file
		rm -f "$out"
	fi
done

# After extracting all manpages, cache the sha1sum, so we don't
# repeat the downloads
sha1sum "$DEB" | awk '{ print $1 }' >"$DESTDIR/.cache/$NAME"

# In the case of freakish package permissions, fix them on rm failure.
rm -rf "$TEMPDIR" 2>/dev/null || (chmod -R 700 "$TEMPDIR" && rm -rf "$TEMPDIR") || true
exit 0
