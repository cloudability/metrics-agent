package kubernetes

import (
	"testing"
)

func TestConnection(t *testing.T) {
	t.Run("Connection bitmask handles Unreachable correctly", func(t *testing.T) {
		exampleConnection := Connection(0)
		if exampleConnection != Unreachable {
			t.Errorf("zero-value Connection should be unreachable, got %s", exampleConnection)
		}
		// make sure no weird shenanigans happen with double Unreachable
		exampleConnection.ClearMethod(Unreachable)
		if exampleConnection != Unreachable {
			t.Errorf("zero-value Connection should be unreachable, got %s", exampleConnection)
		}
		// note: Don't do this, use SetUnreachable
		exampleConnection.AddMethod(Unreachable)
		if exampleConnection != Unreachable {
			t.Errorf("zero-value Connection should be unreachable, got %s", exampleConnection)
		}

		exampleConnection.SetUnreachable()
		if exampleConnection != Unreachable {
			t.Errorf("zero-value Connection should be unreachable, got %s", exampleConnection)
		}
	})
	t.Run("Connection bitmask handles Proxy correctly", func(t *testing.T) {
		exampleConnection := Connection(0)
		exampleConnection.AddMethod(Proxy)

		if !exampleConnection.hasMethod(Proxy) {
			t.Errorf("expected Proxy, got %s", exampleConnection)
		}
		exampleConnection.ClearMethod(Proxy)
		if exampleConnection != Unreachable {
			t.Errorf("cleared connection should be unreachable, got %s", exampleConnection)
		}
	})
	t.Run("Connection bitmask handles Direct correctly", func(t *testing.T) {
		exampleConnection := Connection(0)
		exampleConnection.AddMethod(Direct)

		if !exampleConnection.hasMethod(Direct) {
			t.Errorf("expected Direct, got %s", exampleConnection)
		}
		exampleConnection.ClearMethod(Direct)
		if exampleConnection != Unreachable {
			t.Errorf("cleared connection should be unreachable, got %s", exampleConnection)
		}
	})

	t.Run("Connection bitmask handles multiple flags correctly", func(t *testing.T) {
		exampleConnection := Connection(0)
		exampleConnection.AddMethod(Direct)
		exampleConnection.AddMethod(Proxy)

		// should have both methods set
		if !exampleConnection.hasMethod(Direct) {
			t.Errorf("expected Direct, got %s", exampleConnection)
		}
		if !exampleConnection.hasMethod(Proxy) {
			t.Errorf("expected Proxy, got %s", exampleConnection)
		}
		// should not be Unreachable
		if exampleConnection.hasMethod(Unreachable) {
			t.Errorf("connection should not be unreachable, got %s", exampleConnection)
		}

		if !exampleConnection.hasMethod(Direct | Proxy) {
			t.Errorf("expected to be able to check multiple flags at once")
		}

		exampleConnection.ClearMethod(Direct)

		// should have lost direct but not proxy
		if exampleConnection.hasMethod(Direct) {
			t.Errorf("should not have Direct, got %s", exampleConnection)
		}
		if !exampleConnection.hasMethod(Proxy) {
			t.Errorf("expected Proxy, got %s", exampleConnection)
		}

		exampleConnection.SetUnreachable()

		if exampleConnection.hasMethod(Direct) {
			t.Errorf("expected Unreachable, got %s", exampleConnection)
		}
		if exampleConnection.hasMethod(Proxy) {
			t.Errorf("expectedUnreachable, got %s", exampleConnection)
		}

	})
}

func TestEndpointMask(t *testing.T) {
	t.Run("endpoint should report Unreachable correctly", func(t *testing.T) {
		mask := EndpointMask{}
		if !mask.Unreachable(NodeStatsSummaryEndpoint) {
			t.Error("empty mask should return all endpoints as unreachable")
		}

		// Don't do this weird stuff, use mask.SetUnreachable
		mask.SetAvailability(NodeStatsSummaryEndpoint, Unreachable, false)
		if !mask.Unreachable(NodeStatsSummaryEndpoint) {
			t.Errorf("endpoint should have remained unreachable")
		}

		mask.SetAvailability(NodeStatsSummaryEndpoint, Proxy, true)
		if !mask.ProxyAllowed(NodeStatsSummaryEndpoint) {
			t.Errorf("should have proxy method set, instead got: %s", mask.Options(NodeStatsSummaryEndpoint))
		}

		mask.SetUnreachable(NodeStatsSummaryEndpoint)
		if !mask.Unreachable(NodeStatsSummaryEndpoint) {
			t.Errorf("expected unreachable, got %s", mask.Options(NodeStatsSummaryEndpoint))
		}
	})
	t.Run("endpoint should set availability correctly", func(t *testing.T) {
		mask := EndpointMask{}
		// set as available
		mask.SetAvailability(NodeStatsSummaryEndpoint, Direct, true)
		if !mask.DirectAllowed(NodeStatsSummaryEndpoint) {
			t.Errorf("expected direct connection allowed, but got %s", mask.Options(NodeStatsSummaryEndpoint))
		}
		if mask.ProxyAllowed(NodeStatsSummaryEndpoint) {
			t.Errorf("expected direct connection allowed, but got %s", mask.Options(NodeStatsSummaryEndpoint))
		}

		// set unavailable
		mask.SetAvailability(NodeStatsSummaryEndpoint, Direct, false)
		if mask.DirectAllowed(NodeStatsSummaryEndpoint) {
			t.Errorf("expected direct connection unavailable, but got %s", mask.Options(NodeStatsSummaryEndpoint))
		}
		// an endpoint with no methods available should be unreachable
		if !mask.Unreachable(NodeStatsSummaryEndpoint) {
			t.Errorf("expected unreachable, got %s", mask.Options(NodeStatsSummaryEndpoint))
		}
	})
	t.Run("should be able to set multiple connection methods per endpoint", func(t *testing.T) {
		mask := EndpointMask{}
		if mask.ProxyAllowed(NodeStatsSummaryEndpoint) {
			t.Errorf("expected the availability of an endpoint to default to false")
		}
		mask.SetAvailability(NodeStatsSummaryEndpoint, Proxy, true)
		if !mask.ProxyAllowed(NodeStatsSummaryEndpoint) {
			t.Errorf("expected the availability of an endpoint to be true after being set as available")
		}
		// validate that setting another method doesn't unset the first
		mask.SetAvailability(NodeStatsSummaryEndpoint, Direct, true)
		if !mask.ProxyAllowed(NodeStatsSummaryEndpoint) {
			t.Errorf("expected the availability of an endpoint to be true after being set as available")
		}
		// validate that setting the same method twice does not unset
		mask.SetAvailability(NodeStatsSummaryEndpoint, Direct, true)
		if !mask.DirectAllowed(NodeStatsSummaryEndpoint) {
			t.Errorf("expected the availability of an endpoint to be true after being set as available")
		}
		if mask.Unreachable(NodeStatsSummaryEndpoint) {
			t.Errorf("endpoint should be available, instead got: %s",
				mask.Options(NodeStatsSummaryEndpoint))
		}

	})
}
