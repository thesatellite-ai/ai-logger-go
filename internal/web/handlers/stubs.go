// stubs.go intentionally left empty — every handler now has a real
// implementation in its own file. Kept around so the templ-handler
// pairing convention is documented in one place even after the last
// stub disappears.
//
// To add a new route: define a const + URL builder in routes.go, wire
// it in mountRoutes, then add the handler method in a sibling file.
package handlers
