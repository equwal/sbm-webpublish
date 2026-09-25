# include.awk: add "include snippets/sbm-webpublish.conf;" to each HTTPS
# server block of an nginx site file. The line goes above the first
# "listen ... 443" line of each server block, because only an HTTPS server
# block has that line. deploy.sh runs this only when the file does not
# include the snippet yet.
/^[ \t]*server[ \t]*(\{|$)/ { done = 0 }
!done && /^[ \t]*listen[ \t]+([^;]*:)?443([ \t;]|$)/ {
	match($0, /^[ \t]*/)
	print substr($0, 1, RLENGTH) "include snippets/sbm-webpublish.conf;"
	done = 1
}
{ print }
