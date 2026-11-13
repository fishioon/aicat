package provider

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/html"
)

// fetchIcon reads a public site icon during model synchronization only.
// It sends no provider key and returns a self-contained image for persistence.
func fetchIcon(ctx context.Context, base string) string {
	base = strings.TrimSpace(base)
	if base != "" && !strings.Contains(base, "://") {
		base = "https://" + base
	}
	if validateBaseURL(base) != nil {
		return ""
	}
	u, _ := url.Parse(base)
	u.Path, u.RawPath = "", ""
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	client := &http.Client{Transport: direct, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 3 {
			return http.ErrUseLastResponse
		}
		target := *req.URL
		target.RawQuery, target.Fragment = "", ""
		return validateBaseURL(target.String())
	}}
	data := siteIcon(ctx, client, u)
	if data == "" && strings.HasPrefix(u.Hostname(), "api.") && ctx.Err() == nil {
		// API hosts often have no website; try the same domain without api.
		website := *u
		website.Host = strings.TrimPrefix(website.Host, "api.")
		data = siteIcon(ctx, client, &website)
	}
	return data
}

func siteIcon(ctx context.Context, client *http.Client, origin *url.URL) string {
	if data := iconData(ctx, client, origin.ResolveReference(&url.URL{Path: "/favicon.ico"})); data != "" {
		return data
	}
	res, err := iconResponse(ctx, client, origin)
	if err != nil {
		return ""
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return ""
	}
	z := html.NewTokenizer(io.LimitReader(res.Body, 256<<10))
	var candidates []*url.URL
	for len(candidates) < 6 {
		t := z.Next()
		if t == html.ErrorToken {
			break
		}
		if t != html.StartTagToken && t != html.SelfClosingTagToken {
			continue
		}
		token := z.Token()
		if token.Data != "link" {
			continue
		}
		var rel, href string
		for _, attr := range token.Attr {
			switch attr.Key {
			case "rel":
				rel = strings.ToLower(attr.Val)
			case "href":
				href = attr.Val
			}
		}
		icon := false
		for _, value := range strings.Fields(rel) {
			icon = icon || value == "icon" || value == "apple-touch-icon"
		}
		if icon && href != "" {
			if ref, err := url.Parse(href); err == nil {
				candidates = append(candidates, res.Request.URL.ResolveReference(ref))
			}
		}
	}
	for _, candidate := range candidates {
		if data := iconData(ctx, client, candidate); data != "" {
			return data
		}
	}
	return ""
}

func iconResponse(ctx context.Context, client *http.Client, target *url.URL) (*http.Response, error) {
	// Icons may use cache query strings; validate the origin and path alone.
	check := *target
	check.RawQuery, check.Fragment = "", ""
	if err := validateBaseURL(check.String()); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "aicat")
	return client.Do(req)
}

func iconData(ctx context.Context, client *http.Client, target *url.URL) string {
	res, err := iconResponse(ctx, client, target)
	if err != nil {
		return ""
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return ""
	}
	const limit = 256 << 10
	data, err := io.ReadAll(io.LimitReader(res.Body, limit+1))
	if err != nil || len(data) == 0 || len(data) > limit {
		return ""
	}
	// Only self-contained bitmap formats: do not retain HTML or an SVG's
	// external resources. Browsers can also decode ordinary ICO files.
	mime := http.DetectContentType(data)
	switch mime {
	case "image/png", "image/jpeg", "image/gif", "image/webp", "image/x-icon", "image/vnd.microsoft.icon":
		return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data)
	}
	return ""
}
