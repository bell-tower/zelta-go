package zfs

import (
	"encoding/json"
	"testing"
)

func TestUnmarshalGetJSON(t *testing.T) {
	data := []byte(`{
  "output_version": {"command":"zfs get","vers_major":0,"vers_minor":1},
  "datasets": {
    "apool": {
      "name":"apool", "type":"FILESYSTEM", "pool":"apool", "createtxg":"1",
      "properties": {
        "compression": {"value":"off", "source":{"type":"LOCAL","data":"-"}},
        "recordsize": {"value":"131072", "source":{"type":"DEFAULT","data":"-"}},
        "mountpoint": {"value":"/apool", "source":{"type":"DEFAULT","data":"-"}}
      }
    }
  }
}`)

	var got GetJSON
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	dataset, ok := got.Datasets["apool"]
	if !ok {
		t.Fatal("missing apool")
	}
	if dataset.Type != DatasetFilesystem || dataset.CreateTXG != 1 {
		t.Fatalf("dataset=%+v", dataset)
	}
	compression, ok := dataset.Properties.Get("compression")
	if !ok {
		t.Fatal("missing compression")
	}
	if value, ok := compression.String(); !ok || value != "off" {
		t.Fatalf("compression value=%q ok=%v", value, ok)
	}
	if compression.Source != PropertySourceLocal {
		t.Fatalf("compression source=%q", compression.Source)
	}
	recordsize, ok := dataset.Properties.Get("recordsize")
	if !ok {
		t.Fatal("missing recordsize")
	}
	if value, ok := recordsize.Uint64(); !ok || value != 131072 {
		t.Fatalf("recordsize value=%d ok=%v", value, ok)
	}
}

func TestUnmarshalGetJSONInt(t *testing.T) {
	data := []byte(`{"datasets":{"apool":{"name":"apool","type":"FILESYSTEM","pool":"apool","createtxg":7,"properties":{"guid":{"value":123,"source":{"type":"NONE","data":"-"}}}}}}`)
	var got GetJSON
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	guid, ok := got.Datasets["apool"].Properties.Get("guid")
	if !ok {
		t.Fatal("missing guid")
	}
	if value, ok := guid.Uint64(); !ok || value != 123 {
		t.Fatalf("guid value=%d ok=%v", value, ok)
	}
}

func TestPropertyUint64RejectsNonNumeric(t *testing.T) {
	property := Property{Value: json.RawMessage(`"off"`)}
	if _, ok := property.Uint64(); ok {
		t.Fatal("non-numeric property accepted")
	}
}
