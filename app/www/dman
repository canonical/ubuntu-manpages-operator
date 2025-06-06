#!/bin/sh -e

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


. /etc/lsb-release
while true; do
	case "$1" in
		--release)
			DISTRIB_CODENAME="$2"
			shift 2
			;;
		*)
			break
			;;
	esac
done
PAGE=`echo "$@" | awk '{print $NF}'`
MAN_ARGS=`echo "$@" | sed "s/\$PAGE$//"`

# Mirror support of man's languages
if [ ! -z "$LANG" ]; then
	LOCALE="$LANG"
fi
if [ ! -z "$LC_MESSAGES" ]; then
	LOCALE="$LC_MESSAGES"
fi
if echo $LOCALE | grep -q "^en"; then
	LOCALE=""
fi

URL="http://manpages.ubuntu.com/manpages.gz/"

mandir=`mktemp -d dman.XXXXXX`
trap "rm -rf $mandir" EXIT HUP INT QUIT TERM
for i in `seq 1 9`; do
	man="$mandir/$i"
	if wget -O "$man" "$URL/$DISTRIB_CODENAME/$LOCALE/man$i/$PAGE.$i.gz" 2>/dev/null; then
		man $MAN_ARGS -l "$man" || true
	fi
	rm -f "$man"
done
