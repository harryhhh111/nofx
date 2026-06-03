package api

import (
	"fmt"
	"strings"

	"github.com/gin-gonic/gin"
)

// RouteDoc holds one registered API route.
type RouteDoc struct {
	Method string
	Path   string
}

// routeRegistry stores routes registered via s.route().
var routeRegistry []RouteDoc

func (s *Server) route(g *gin.RouterGroup, method, path string, h gin.HandlerFunc) {
	fullPath := strings.TrimSuffix(g.BasePath(), "/") + "/" + strings.TrimPrefix(path, "/")
	routeRegistry = append(routeRegistry, RouteDoc{
		Method: method,
		Path:   fullPath,
	})
	switch method {
	case "GET":
		g.GET(path, h)
	case "POST":
		g.POST(path, h)
	case "PUT":
		g.PUT(path, h)
	case "DELETE":
		g.DELETE(path, h)
	}
}

// GetAPIDocs returns a compact API route list.
func GetAPIDocs() string {
	var sb strings.Builder
	for _, r := range routeRegistry {
		sb.WriteString(fmt.Sprintf("%-8s %s\n", r.Method, r.Path))
	}
	return sb.String()
}
