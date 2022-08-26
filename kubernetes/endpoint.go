package kubernetes

import (
	"strings"

	"github.com/cloudability/metrics-agent/retrieval/raw"
)

// Connection is a bitmask that describes the manner(s) in which
// the agent can connect to an endpoint
type Connection uint8

const (
	// By bitshifting each constant with iota we can use Connection as a bitmask
	Direct Connection = 1 << iota // 0001 = 1
	Proxy                         // 0010 = 2
	// Unreachable defined at end to avoid affecting iota,
	// as it should always be set to 0
	Unreachable Connection = 0
)

func (c Connection) hasMethod(method Connection) bool { return c&method != 0 }

// AddMethod adds the provided nonzero method to the bitmask (use SetUnreachable for Unreachable)
func (c *Connection) AddMethod(method Connection) { *c |= method }

// ClearMethod removes the method from the bitmask
func (c *Connection) ClearMethod(method Connection) { *c &= ^method }

// SetUnreachable sets the connection as unreachable
func (c *Connection) SetUnreachable() { *c = 0 }

func (c Connection) String() string {
	if c == Unreachable {
		return unreachable
	}
	var options []string
	if c.hasMethod(Proxy) {
		options = append(options, proxy)
	}
	if c.hasMethod(Direct) {
		options = append(options, direct)
	}
	return strings.Join(options, ",")
}

type ConnectionMethod struct {
	ConnType     Connection
	API          nodeAPI
	client       raw.Client
	FriendlyName string
}

// Endpoint represents the various metrics endpoints we hit
type Endpoint string

const (
	// NodeStatsSummaryEndpoint the /stats/summary endpoint
	NodeStatsSummaryEndpoint Endpoint = "/stats/summary"

	// NodeContainerEndpoint the /stats/container endpoint
	NodeContainerEndpoint Endpoint = "/stats/container"
)

// EndpointMask a map representing the currently active endpoints.
// The keys of the map are the currently active endpoints.
type EndpointMask map[Endpoint]Connection

// SetAvailability sets an endpoint availability state according to the supplied boolean
func (m EndpointMask) SetAvailability(endpoint Endpoint, method Connection, available bool) {
	e := m[endpoint]
	if available {
		e.AddMethod(method)
	} else {
		e.ClearMethod(method)
	}
	m[endpoint] = e
}

func (m EndpointMask) SetUnreachable(endpoint Endpoint) {
	e := m[endpoint]
	e.SetUnreachable()
	m[endpoint] = e
}

// Available gets the availability of an endpoint for the specified connection method
func (m EndpointMask) Available(endpoint Endpoint, method Connection) bool {
	return m[endpoint].hasMethod(method)
}

func (m EndpointMask) Unreachable(endpoint Endpoint) bool {
	return m[endpoint] == Unreachable
}

func (m EndpointMask) DirectAllowed(endpoint Endpoint) bool {
	return m[endpoint].hasMethod(Direct)
}

func (m EndpointMask) ProxyAllowed(endpoint Endpoint) bool {
	return m[endpoint].hasMethod(Proxy)
}

func (m EndpointMask) Options(endpoint Endpoint) string {
	return m[endpoint].String()
}
