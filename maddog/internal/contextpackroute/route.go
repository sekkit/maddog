// Package contextpackroute owns the model-visible tool-output routing states.
package contextpackroute

// Route selects whether ContextPack passes raw output through or applies a
// native lossy transform.
type Route string

const (
	// RoutePassthrough keeps the raw model-visible output unchanged.
	RoutePassthrough Route = "passthrough"
	// RouteGeneric applies the generic bounded output transform.
	RouteGeneric Route = "generic"
	// RouteProfile applies a command-specific output transform.
	RouteProfile Route = "profile"
)

// IsCompression reports whether the route selects a native lossy transform.
func (r Route) IsCompression() bool {
	return r == RouteGeneric || r == RouteProfile
}
