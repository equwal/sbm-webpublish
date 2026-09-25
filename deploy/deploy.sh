#!/bin/sh
# deploy.sh: build sbm-webpublish for Linux and install it on a server with
# systemd and nginx.
#
#     sh deploy/deploy.sh root@example.com [site]
#
# site is the name of the nginx site file in /etc/nginx/sites-enabled. When
# that file does not include the nginx snippet, the script adds the include
# line to each HTTPS server block of the file (see include.awk). It keeps a
# copy of the file, and it puts the copy back if nginx -t fails.
#
# When the server has no admin password, the script makes one. Read it on
# the server with: cat /etc/sbm-webpublish.env
set -e
host=${1:?usage: sh deploy/deploy.sh user@host [site]}
site=$2
cd "$(dirname "$0")/.."
# -buildvcs=false: the build does not need the git status. Under Cygwin, git
# refuses a checkout that Git for Windows made ("dubious ownership"), and
# the build stops.
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -buildvcs=false -trimpath -o sbm-webpublish .
scp sbm-webpublish deploy/sbm-webpublish.service deploy/nginx-location.conf deploy/include.awk "$host:/tmp/"
rm sbm-webpublish
ssh "$host" "SITE='$site' sh -s" << 'EOF'
set -e
install -m 755 /tmp/sbm-webpublish /usr/local/bin/sbm-webpublish
install -m 644 /tmp/sbm-webpublish.service /etc/systemd/system/sbm-webpublish.service
install -m 644 /tmp/nginx-location.conf /etc/nginx/snippets/sbm-webpublish.conf
rm /tmp/sbm-webpublish /tmp/sbm-webpublish.service /tmp/nginx-location.conf
if [ ! -s /etc/sbm-webpublish.env ]; then
    (umask 077 && printf 'ADMIN_PASSWORD=%s\n' "$(head -c 18 /dev/urandom | base64)" > /etc/sbm-webpublish.env)
    echo "made an admin password: cat /etc/sbm-webpublish.env"
fi
systemctl daemon-reload
systemctl enable sbm-webpublish
systemctl restart sbm-webpublish
sleep 1
if ! systemctl is-active --quiet sbm-webpublish; then
    journalctl -u sbm-webpublish -n 20 --no-pager
    exit 1
fi
echo "sbm-webpublish is running"

conf=/etc/nginx/sites-enabled/$SITE
copy=/etc/nginx/$SITE.before-sbm-webpublish
if [ -n "$SITE" ] && [ ! -f "$conf" ]; then
    echo "no nginx site file $conf"
    exit 1
fi
if [ -n "$SITE" ] && ! grep -q 'snippets/sbm-webpublish.conf' "$conf"; then
    # The copy and the new file are outside sites-enabled, so that nginx
    # does not read them. cat writes through a symbolic link.
    cp "$conf" "$copy"
    awk -f /tmp/include.awk "$copy" > /tmp/sbm-webpublish.site
    cat /tmp/sbm-webpublish.site > "$conf"
    rm /tmp/sbm-webpublish.site
    if ! nginx -t; then
        cat "$copy" > "$conf"
        echo "nginx -t failed. $conf is as before."
        exit 1
    fi
    echo "added the include line to $conf (the old file is $copy)"
fi
rm /tmp/include.awk
if grep -qs 'snippets/sbm-webpublish.conf' /etc/nginx/sites-enabled/*; then
    nginx -t
    systemctl reload nginx
    echo "nginx reloaded"
else
    echo "No HTTPS server block got the include line. Add this line to the"
    echo "HTTPS server block of the site:"
    echo "    include snippets/sbm-webpublish.conf;"
    echo "Then run: nginx -t && systemctl reload nginx"
fi
EOF
