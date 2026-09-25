# sbm-webpublish

sbm-webpublish publishes an [sbm](https://github.com/equwal/sbm) bookmark
file as a list of links on a web site. The links page of
[recentlywritten.com](https://recentlywritten.com/links.html) uses it.

It is one small Go program. At run time it needs only the Go standard
library. The tests use rapid for property tests. The program keeps the
links in one plain sbm file, one line for each link:

    URL<tab>description<tab>tag tag tag<tab>feed

so `bm`, `cut`, `awk` and `grep` can read the same file. The feed is the
address of the RSS or Atom feed of the site. It is optional: a line without
a feed has the three fields of `bm`. `bm` reads only the first three
fields. `bm --merge` does not copy the feed.

## Get the links

The program makes each output from the file at each request. The outputs
contain only http and https links.

| Path | Format |
|---|---|
| `/links/links.txt` | The links as sbm lines, oldest first. The fourth field is the feed of the site, if there is one. |
| `/links/atom.xml` | An Atom feed, newest first. Each tag of a link is a category. |
| `/links/list.html` | The links as an HTML fragment, under each of their tags, with an "rss" link after each link that has a feed. A page of the site includes it with nginx SSI. |

Each output sends `Last-Modified` and answers `If-Modified-Since` with
`304 Not Modified`.

To follow the links in [sfeed](https://codemadness.org/sfeed.html), add
a line to `feeds()` in your sfeedrc:

    feed 'recentlywritten links' 'https://recentlywritten.com/links/atom.xml'

The plain text works with the usual tools:

    curl -s https://recentlywritten.com/links/links.txt | cut -f1
    curl -s https://recentlywritten.com/links/links.txt | bm --merge
    curl -s https://recentlywritten.com/links/links.txt | dmenu -l 20 | cut -f1 | xargs xdg-open

To follow the sites of the links, make a `feed` line for sfeedrc from each
link that has a feed. The name of each feed comes from its URL:

    curl -s https://recentlywritten.com/links/links.txt | awk -F'\t' '$4 {
        n = $1; sub(/^[a-z]+:\/\//, "", n); gsub(/[^A-Za-z0-9._-]/, "-", n)
        printf "\tfeed '\''%s'\'' '\''%s'\''\n", n, $4 }'

## Dates

A feed needs the time of each link, and an sbm line has no time. So the
admin page writes a date line above the links that it adds:

    # 2026-09-25T08:00:00Z
    https://suckless.org/	software that sucks less	code unix

`bm` ignores each line that starts with `#`, so the file stays an sbm file.
In the feed, a link gets the time of the last date line above it. A link
below no date line gets the time of the file. If you add links to the file
with an editor, write a date line above them.

## The admin page

`/links/admin` lets the owner add a link with its feed, set the feed of a
link, delete a link and import a file. It uses HTTP basic auth. Any user
name works. The password is the value of `ADMIN_PASSWORD`. The server
refuses a form POST that comes from another site.

The import takes an sbm file or the Netscape HTML that browsers export.
The folders of a browser bookmark become its tags. The import skips a URL
that is a link already, but a line with a feed gives its feed to a link
that has none. Two URLs are the same link when they differ only in scheme,
a leading `www.`, trailing slashes or the case of the host, as in `bm`.

## Add links in a shell

On the server, `sbm-webpublish merge` adds the links of its input, as the
import does:

    bm --list | ssh root@example.com 'LINKS_FILE=/var/lib/sbm-webpublish/links.sbm sbm-webpublish merge'

It does not lock the file against the service, so do not use the admin
page at the same time.

## Run

    ADMIN_PASSWORD=secret go run .

Then open http://127.0.0.1:8082/links/admin.

| Variable | Default | Meaning |
|---|---|---|
| `ADMIN_PASSWORD` | none, required | The password of the admin page. |
| `LINKS_FILE` | `links.sbm` | The sbm file of the links. |
| `ADDR` | `127.0.0.1:8082` | The address to listen on. |

The feed has no settings. It gets its address and name from the request.

## Put it on a site

1. Install the program on a server with systemd and nginx. The second
   argument is the name of the site file in `/etc/nginx/sites-enabled`:

       sh deploy/deploy.sh root@example.com example

   The script builds the program, installs `deploy/sbm-webpublish.service`,
   makes an admin password when there is none, and installs
   `deploy/nginx-location.conf` as `/etc/nginx/snippets/sbm-webpublish.conf`.
2. The script adds `include snippets/sbm-webpublish.conf;` to each HTTPS
   server block of the site file, runs `nginx -t` and reloads nginx. If
   `nginx -t` fails, it puts the old site file back. The snippet sends
   `/links/` to the program and turns on SSI for `/links.html`.
3. Put this line in the page of the links:

       <!--# include virtual="/links/list.html" -->

   Put `<link rel="alternate" type="application/atom+xml" href="links/atom.xml">`
   in its head, so that feed readers and `sfeed_web` find the feed. See
   `pages/links.md` in
   [recentlywritten](https://github.com/equwal/recentlywritten) for an
   example.

## Test

    go test ./...

## License

AGPL-3.0, as sbm-sync.
