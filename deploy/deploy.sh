#!/bin/sh
# deploy.sh: build sbm-webpublish for Linux and install it on a server with
# systemd and nginx.
#
#     sh deploy/deploy.sh root@example.com
#
# When the server has no admin password, the script makes one. Read it on
# the server with: cat /etc/sbm-webpublish.env
set -e
host=${1:?usage: sh deploy/deploy.sh user@host}
cd "$(dirname "$0")/.."
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -o sbm-webpublish .
scp sbm-webpublish deploy/sbm-webpublish.service deploy/nginx-location.conf "$host:/tmp/"
rm sbm-webpublish
ssh "$host" sh -s << 'EOF'
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
if grep -qs 'snippets/sbm-webpublish.conf' /etc/nginx/sites-enabled/*; then
    nginx -t
    systemctl reload nginx
    echo "nginx reloaded"
else
    echo "Add this line to the server block of the site:"
    echo "    include snippets/sbm-webpublish.conf;"
    echo "Then run: nginx -t && systemctl reload nginx"
fi
EOF
