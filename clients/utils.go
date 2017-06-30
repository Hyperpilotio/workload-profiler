package clients

import (
	"net/url"
)

func UrlBasePath(u *url.URL) string {
	return u.Scheme + "://" + u.Host + "/"
}
