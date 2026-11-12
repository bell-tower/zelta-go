package jsonout

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestDocument(t *testing.T) {
	d := New("zelta backup")
	d.Set("sourceEndpoint", "local\rhost:pool/source")
	d.Set("startTime", int64(1793724663))
	d.Set("replicationErrorCode", 0)
	d.Set("replicationTime", 0.1+0.2)
	d.Set("sourceUser", "1000")
	d.Set("targetSnapshot", "")
	d.Set("custom_field", "x")
	d.Set("custom_field", "y")
	d.Set("sourceHost", true)
	d.ErrorMessages = []string{`it said "no"`, "path\\x"}
	var buf bytes.Buffer
	if err := d.Write(&buf, false); err != nil {
		t.Fatal(err)
	}
	want := `{"output_version": {"command": "zelta backup","vers_major": 1,"vers_minor": 1},"startTime": 1793724663,"sourceUser": 1000,"sourceHost": "true","sourceEndpoint": "localhost:pool/source","replicationErrorCode": 0,"replicationTime": 0.3,"custom_field": "y","errorMessages": ["it said \"no\"","path\\x"]}` + "\n"
	if buf.String() != want {
		t.Errorf("got\n%s\nwant\n%s", buf.String(), want)
	}
	var v map[string]any
	if err := json.Unmarshal(buf.Bytes(), &v); err != nil {
		t.Errorf("invalid json: %v", err)
	}
	if d.Get("startTime") != "1793724663" {
		t.Errorf("Get = %q", d.Get("startTime"))
	}
}

func TestPrettyAndArrays(t *testing.T) {
	d := New("zelta backup")
	d.Set("runTime", 2)
	d.SentStreams = []string{"pool/ds@a%b", "pool/ds/c@b"}
	var buf bytes.Buffer
	d.Write(&buf, true)
	lines := strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n")
	if lines[0] != "{" || lines[1] != `  "output_version": {` || lines[2] != `    "command": "zelta backup",` || lines[len(lines)-1] != "}" {
		t.Errorf("pretty output:\n%s", buf.String())
	}
	var v struct {
		SentStreams []string `json:"sentStreams"`
	}
	if err := json.Unmarshal(buf.Bytes(), &v); err != nil || len(v.SentStreams) != 2 {
		t.Errorf("pretty json invalid: %v %#v", err, v)
	}
}

func TestString(t *testing.T) {
	tests := map[string]string{
		"plain":    `"plain"`,
		"a\tb\nc":  `"a\tb\nc"`,
		"bell\x07": `"bell\u0007"`,
		"cr\r":     `"cr"`,
		"ünï":      `"ünï"`,
		"<&>":      `"<&>"`,
	}
	for in, want := range tests {
		if got := String(in); got != want {
			t.Errorf("String(%q) = %s want %s", in, got, want)
		}
	}
	for in, want := range map[string]string{"12": "12", "-3.5": "-3.5", "1.2.3": `"1.2.3"`, "007": `"007"`, "": `""`} {
		if got := value(in); got != want {
			t.Errorf("value(%q) = %s want %s", in, got, want)
		}
	}
}
