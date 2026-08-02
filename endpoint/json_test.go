package endpoint_test

import (
	"encoding/json"
	"testing"

	"github.com/bell-tower/zelta-go/endpoint"
)

func TestEndpointJSONObjectRoundTrip(t *testing.T) {
	ep := endpoint.MustParse("alice@host:tank/ds@snap")
	b, err := json.Marshal(ep)
	if err != nil {
		t.Fatal(err)
	}
	var got endpoint.Endpoint
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got.User != "alice" || got.Host != "host" || got.Dataset != "tank/ds" || got.Snapshot != "snap" {
		t.Fatalf("%+v", got)
	}
	if !got.Remote {
		t.Fatal("expected remote")
	}
}

func TestEndpointJSONStringImport(t *testing.T) {
	var ep endpoint.Endpoint
	if err := json.Unmarshal([]byte(`"tank/local"`), &ep); err != nil {
		t.Fatal(err)
	}
	if ep.Dataset != "tank/local" || ep.Remote {
		t.Fatalf("%+v", ep)
	}
}

func TestEndpointJSONNull(t *testing.T) {
	var ep endpoint.Endpoint
	if err := json.Unmarshal([]byte(`null`), &ep); err != nil {
		t.Fatal(err)
	}
	if !ep.IsZero() {
		t.Fatalf("%+v", ep)
	}
}
