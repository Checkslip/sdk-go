package checkslip

import (
	"net/url"
	"strconv"
	"strings"
	"time"
)

// query builds URL query parameters, skipping empty values.
type query url.Values

func (q query) str(key, v string) {
	if v != "" {
		url.Values(q).Set(key, v)
	}
}

func (q query) int(key string, v int) {
	if v != 0 {
		url.Values(q).Set(key, strconv.Itoa(v))
	}
}

func (q query) time(key string, t time.Time) {
	if !t.IsZero() {
		url.Values(q).Set(key, t.Format(time.RFC3339Nano))
	}
}

func (q query) date(key string, d Date) {
	if !d.IsZero() {
		url.Values(q).Set(key, d.String())
	}
}

// csv sends a list as one comma-separated value (OpenAPI explode=false).
func csv[S ~string](q query, key string, vs []S) {
	if len(vs) == 0 {
		return
	}
	parts := make([]string, len(vs))
	for i, v := range vs {
		parts[i] = string(v)
	}
	url.Values(q).Set(key, strings.Join(parts, ","))
}

// multi sends a list as a repeated key (OpenAPI explode=true).
func multi[S ~string](q query, key string, vs []S) {
	for _, v := range vs {
		url.Values(q).Add(key, string(v))
	}
}

// multiInt repeats key once per value, as style=form with explode=true asks.
func multiInt(q query, key string, vs []int) {
	for _, v := range vs {
		url.Values(q).Add(key, strconv.Itoa(v))
	}
}

func (q query) page(o ListOptions) {
	q.int("limit", o.Limit)
	q.str("cursor", o.Cursor)
}

func (q query) values() url.Values { return url.Values(q) }

// pathEscape escapes one path segment such as an ID.
func pathEscape(s string) string { return url.PathEscape(s) }
