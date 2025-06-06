#!/bin/bash -e

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

DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

CONFIG="${MANPAGES_CONFIG_FILE:-/app/www/config.json}"
if [[ -z "$CONFIG" ]]; then
	echo "ERROR: Configuration file not found. Please set \$MANPAGES_CONFIG_FILE."
	exit 1
fi

ARCHIVE="$(jq -r '.archive' "$CONFIG")"
DEBDIR="$(jq -r '.debdir' "$CONFIG")"
PUBLIC_HTML_DIR="$(jq -r '.public_html_dir' "$CONFIG")"
DISTROS="$(jq -r '.releases | keys | join(" ")' "$CONFIG")"
REPOS="$(jq -r '.repos | join(" ")' "$CONFIG")"
ARCH="$(jq -r '.arch' "$CONFIG")"

# Establish some locking, to keep multiple updates from running
mkdir -p "$PUBLIC_HTML_DIR/manpages"
LOCK="$PUBLIC_HTML_DIR/manpages/UPDATE_IN_PROGRESS"
if [ -e "$LOCK" ]; then
	printf "%s\n" "ERROR: Update is currently running"
	printf "%s\n" "Lock: $LOCK"
	cat "$LOCK"
	exit 1
fi
trap 'rm -f $LOCK 2>/dev/null || true' EXIT HUP INT QUIT TERM
date >"$LOCK"

FORCE="$1"

get_packages_url() {
	local dist=$1
	local repo=$2
	local arch=$3
	if [ -e "$DEBDIR/dists/$dist/$repo/binary-$arch/Packages.gz" ]; then
		echo "file://$DEBDIR/dists/$dist/$repo/binary-$arch/Packages.gz"
	else
		echo "$ARCHIVE/dists/$dist/$repo/binary-$arch/Packages.gz"
	fi
}

get_deb_url() {
	local deb=$1
	if [ -e "$DEBDIR/$deb" ]; then
		echo "file://$DEBDIR/$deb"
	else
		echo "$ARCHIVE/$deb"
	fi
}

is_pkg_cache_invalid() {
	if [ "$FORCE" = "-f" ] || [ "$FORCE" = "--force" ]; then
		return 0
	fi
	local deb
	deb="$1"
	local sum
	sum="$2"
	local distnopocket
	distnopocket="$3"
	local name
	name=$(basename "$deb" | awk -F_ '{print $1}')
	existing_sum=$(cat "$PUBLIC_HTML_DIR/manpages/$dist/.cache/$name" 2>/dev/null)

	# Take the first two digits of the existing_sum modulo 28 to
	# compare to the current day of month.
	#
	# Reasoning: this will invalidate the cache for everything ~
	# once per month (days: 1-28)
	day_mod=$((0x$(echo "$existing_sum" | cut -b 1-2) % 27 + 1))
	if [ "$day_mod" -eq "$(date +%d)" ]; then
		echo "INFO ($(date '+%H:%M:%S.%N')) - ${distnopocket}: date_mod match, regnerating: $deb ($day_mod)"
		return 0
	fi

	# Of course, if the sum found in the packages file for this
	# package does not equal the sum I have on disk, regenerate.
	if [ "$existing_sum" = "$sum" ]; then
		echo "INFO ($(date '+%H:%M:%S.%N')) - ${distnopocket}: cksum skip: $deb"
		return 1
	else
		echo "INFO ($(date '+%H:%M:%S.%N')) - ${distnopocket}: cksum mismatch: $deb"
		return 0
	fi
}

handle_deb() {
	local distnopocket
	distnopocket="$1"
	local deb
	deb="$2"
	local sum
	sum="$3"
	local deburl
	deburl=$(get_deb_url "$deb")
	# FIXME: the || true needs to bubble up to a list of things wrong obviously.
	# shellcheck disable=SC2015
	is_pkg_cache_invalid "$deb" "$sum" "$distnopocket" && "$DIR/fetch-man-pages.sh" "$distnopocket" "$deburl" || true
}

link_en_locale() {
	local distnopocket
	distnopocket="$1"
	mkdir -p "$PUBLIC_HTML_DIR/manpages/$distnopocket/en"
	for i in $(seq 1 9); do
		for j in "manpages" "manpages.gz"; do
			mkdir -p "$PUBLIC_HTML_DIR/$j/$distnopocket/en"
			dir="$PUBLIC_HTML_DIR/$j/$distnopocket/en/man$i"
			if [ -L "$dir" ]; then
				# link exists: we're good
				continue
			elif [ -d "$dir" ]; then
				# dir exists: mv, ln, restore
				mv -f "$dir" "$dir.bak"
				ln -s "../man$i" "$dir"
				mv -f "$dir.bak"/* "$dir"
				rmdir "$dir.bak"
			else
				# link does not exist: establish the link
				ln -s "../man$i" "$dir"
			fi
		done
	done
	return 0
}

handle_series() {
	local dist
	dist="${1}"
	# On one hand in some cases we do not want to know the pocket, as it
	# would show up in paths, URLs and bug links
	distnopocket="${dist}"
	# Yet on the other hand we need to process all pockets. Without -release
	# we'd miss content that never got an update, without -updates we'd not
	# pick up changes.
	# It orders by likely most up-to-date pocket first and only re-renders if
	# a newer version of the same source package is found later (even single
	# Packages files can list the same source multiple times).
	declare -A pkg_handled
	pkg_handled=()
	for pocket in "-updates" "-security" ""; do
		mkdir -p "$PUBLIC_HTML_DIR/manpages/$distnopocket/.cache" "$PUBLIC_HTML_DIR/manpages.gz/$distnopocket" || true
		link_en_locale "$distnopocket"
		for repo in $REPOS; do
			for arch in $ARCH; do
				file=$(get_packages_url "${dist}${pocket}" "$repo" "$arch")
				echo "INFO ($(date '+%H:%M:%S.%N')) - ${dist}: Packages.gz: $file"
				plist=$(mktemp "/tmp/XXXXXXX.manpages.${dist}${pocket}.$repo.$arch.plist")
				curl -s "$file" |
					gunzip -c |
					grep -E "(^Package: |^Version: |^Filename: |^SHA1: )" |
					awk '{print $2}' |
					sed 'N;N;N;s/\n/ /g' |
					sort -u >"${plist}"
				while read -r binpkg version deb sum; do
					if dpkg --compare-versions "${version}" gt "${pkg_handled["$binpkg"]}"; then
						if [[ -n "${pkg_handled[$binpkg]}" ]]; then
							echo "INFO ($(date '+%H:%M:%S.%N')) - ${dist}: binpkg: $binpkg ${version} > ${pkg_handled["$binpkg"]} (processing it again)"
						else
							echo "INFO ($(date '+%H:%M:%S.%N')) - ${dist}: First encounter of binpkg: $binpkg ${version} (processing)"
						fi
						pkg_handled["$binpkg"]="${version}"
						handle_deb "$distnopocket" "$deb" "$sum"
					else
						echo "INFO ($(date '+%H:%M:%S.%N')) - ${dist}: binpkg: $binpkg ${version} < ${pkg_handled["$binpkg"]} (not processing)"
					fi
				done <"${plist}"
				rm -f "${plist}"
			done
		done
	done

}

# Simple parallelization on the level of releases; they do not overlap
# in regard to directories/files, but doing so help to keep the network
# connection utilized as one can fetch while the other is converting.
# Furthermore it avoids that issues, or a lot of new content, in one release
# (e.g. -dev opened) will make the regular update on the others take ages.
for dist in $DISTROS; do
	handle_series "${dist}" &
done
wait

"$DIR/make-sitemaps.sh"
