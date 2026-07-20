package templates

import (
	"context"
	"strings"

	"github.com/a-h/templ"
)

type pageContextKey struct{}

// ClusterOption is one entry in the global cluster switcher.
type ClusterOption struct {
	ID          string
	DisplayName string
	ClusterName string
	Namespace   string
	URL         string
	Active      bool
}

// PageContext carries request-scoped navigation and identity data into all
// templates without adding the same arguments to every component.
type PageContext struct {
	BasePath    string
	ClusterID   string
	ClusterName string
	DisplayName string
	Namespace   string
	Login       string
	IsAdmin     bool
	Clusters    []ClusterOption
}

// WithPageContext attaches navigation context before rendering.
func WithPageContext(ctx context.Context, page PageContext) context.Context {
	return context.WithValue(ctx, pageContextKey{}, page)
}

// CurrentPage returns the current page context, or a legacy single-cluster
// default for direct template tests and the compatibility router.
func CurrentPage(ctx context.Context) PageContext {
	page, _ := ctx.Value(pageContextKey{}).(PageContext)
	if page.BasePath == "" {
		page.BasePath = "/"
	}
	return page
}

// Route scopes an application path to the active cluster.
func Route(ctx context.Context, path string) templ.SafeURL {
	page := CurrentPage(ctx)
	base := strings.TrimSuffix(page.BasePath, "/")
	if path == "" || path == "/" {
		if base == "" {
			return templ.SafeURL("/")
		}
		return templ.SafeURL(base + "/")
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return templ.SafeURL(base + path)
}
