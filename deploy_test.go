package main

import (
	"os/exec"
	"strings"
	"testing"
)

// TestIncludeAWK checks deploy/include.awk. deploy.sh runs it on the nginx
// site file of the server, so a mistake in it breaks the site. The test
// needs awk.
func TestIncludeAWK(t *testing.T) {
	awk, err := exec.LookPath("awk")
	if err != nil {
		t.Skip("no awk:", err)
	}
	const inc = "include snippets/sbm-webpublish.conf;"
	certbot := `server {
    server_name a.com;
    root /var/www/a;
    location / {
        try_files $uri $uri/ =404;
    }
    listen [::]:443 ssl ipv6only=on; # managed by Certbot
    listen 443 ssl; # managed by Certbot
}
server {
    if ($host = a.com) {
        return 301 https://$host$request_uri;
    } # managed by Certbot
    listen 80;
    server_name a.com;
    return 404; # managed by Certbot
}
`
	plain := "server {\n    listen 80;\n    listen 8443 ssl;\n    # listen 443 ssl;\n    server_name a.com;\n}\n"
	for _, c := range []struct{ name, in, want string }{
		// One line in the HTTPS block, above its first listen line. The
		// block for port 80 stays the same.
		{"certbot", certbot, strings.Replace(certbot, "    listen [::]:443", "    "+inc+"\n    listen [::]:443", 1)},
		// Each HTTPS block gets the line, with the indent of its listen line.
		{"two blocks", "server\n{\n\tlisten 127.0.0.1:443 ssl;\n}\nserver {\n  listen 443;\n}\n",
			"server\n{\n\t" + inc + "\n\tlisten 127.0.0.1:443 ssl;\n}\nserver {\n  " + inc + "\n  listen 443;\n}\n"},
		// Port 8443 and a comment are not HTTPS on port 443.
		{"no https", plain, plain},
	} {
		cmd := exec.Command(awk, "-f", "deploy/include.awk")
		cmd.Stdin = strings.NewReader(c.in)
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if string(out) != c.want {
			t.Errorf("%s: got\n%s\nwant\n%s", c.name, out, c.want)
		}
	}
}
