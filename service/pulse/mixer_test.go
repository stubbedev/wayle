package pulse

import "testing"

func TestParseEndpointsFromPactlJSON(t *testing.T) {
	raw := []byte(`[
	 {"index":7,"state":"SUSPENDED","name":"b","description":"B","mute":true,
	  "volume":{"front-left":{"value":32768},"front-right":{"value":65536}},"active_port":null},
	 {"index":2,"state":"RUNNING","name":"a","description":"A","mute":false,
	  "volume":{"mono":{"value":65536}},"active_port":"analog-output"},
	 {"index":9,"state":"UNLINKED","name":"c","description":"C","mute":false,"volume":{}}
	]`)
	got, err := parseEndpoints(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0].Name != "a" || got[1].Name != "b" || got[2].Name != "c" {
		t.Fatalf("not in index order: %+v", got)
	}
	if got[0].Volume != 100 || got[0].State != "Running" || got[0].ActivePort != "analog-output" {
		t.Errorf("a = %+v", got[0])
	}
	if got[1].Volume != 75 || !got[1].Muted || got[1].State != "Suspended" || got[1].ActivePort != "" {
		t.Errorf("b = %+v", got[1])
	}
	if got[2].Volume != 0 || got[2].State != "Offline" {
		t.Errorf("an unlinked device is Offline with no volume: %+v", got[2])
	}
	if _, err := parseEndpoints([]byte("not json")); err == nil {
		t.Error("garbage parsed")
	}
}
