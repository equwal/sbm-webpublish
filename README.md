# sbm-webpublish

sbm-webpublish publishes an [sbm](https://github.com/equwal/sbm) bookmark
file as a list of links on a web site. The links page of
[recentlywritten.com](https://recentlywritten.com/links.html) uses it.

It is one small Go program with no dependencies outside the standard
library. It keeps the links in one plain sbm file: one line for each link,

    URL<tab>description<tab>tag tag tag

so `bm` can read and edit the same file.

## Pages

| Path | Who | What |
|---|---|---|
| `GET /links/links.json` | everyone | The links, newest first, as JSON: `url`, `title`, `host`, `tags`. Only http and https links are in the list. |
| `GET /links/admin` | owner | Add a link, delete a link, import a file. |

The admin pages use HTTP basic auth. Any user name works. The password is
the value of `ADMIN_PASSWORD`. The server refuses a form POST that comes
from another site.

The import takes an sbm file or the Netscape HTML that browsers export.
The folders of a browser bookmark become its tags. A URL that is a link
already is skipped. Two URLs are the same link when they differ only in
scheme, a leading `www.`, trailing slashes or the case of the host, as in
`bm`.

## Run

    ADMIN_PASSWORD=secret go run .

Then open http://127.0.0.1:8082/links/admin.

| Variable | Default | Meaning |
|---|---|---|
| `ADMIN_PASSWORD` | none, required | The password of the admin pages. |
| `LINKS_FILE` | `links.sbm` | The sbm file of the links. |
| `ADDR` | `127.0.0.1:8082` | The address to listen on. |

## Put it on a site

1. Install the service with `deploy/sbm-webpublish.service`.
2. Add `deploy/nginx-location.conf` to the nginx server block of the site.
3. On a page of the site, get `links/links.json` and show the links. See
   `pages/links.md` in
   [recentlywritten](https://github.com/equwal/recentlywritten) for an
   example.

## Test

    go test ./...

## License

AGPL-3.0, as sbm-sync.
