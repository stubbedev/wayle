package sni

import (
	"testing"
)

func TestParseCategory(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want Category
	}{
		{"ApplicationStatus", CategoryApplicationStatus},
		{"Communications", CategoryCommunications},
		{"SystemServices", CategorySystemServices},
		{"Hardware", CategoryHardware},
		{"", CategoryApplicationStatus},
		{"Nonsense", CategoryApplicationStatus},
	} {
		if got := ParseCategory(tc.raw); got != tc.want {
			t.Errorf("ParseCategory(%q) = %q, want %q", tc.raw, got, tc.want)
		}
	}
}

func TestParseStatus(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want Status
	}{
		{"Passive", StatusPassive},
		{"Active", StatusActive},
		{"NeedsAttention", StatusNeedsAttention},
		{"", StatusPassive},
		{"Nonsense", StatusPassive},
	} {
		if got := ParseStatus(tc.raw); got != tc.want {
			t.Errorf("ParseStatus(%q) = %q, want %q", tc.raw, got, tc.want)
		}
	}
}

func TestParseAddress(t *testing.T) {
	for _, tc := range []struct {
		reg, bus, path string
	}{
		{":1.100/StatusNotifierItem", ":1.100", "/StatusNotifierItem"},
		{"org.kde.StatusNotifierItem-123-1/StatusNotifierItem", "org.kde.StatusNotifierItem-123-1", "/StatusNotifierItem"},
		{"com.canonical.indicator.application/MenuBar", "com.canonical.indicator.application", "/MenuBar"},
		{"org.example.Bare", "org.example.Bare", "/StatusNotifierItem"},
	} {
		bus, path := ParseAddress(tc.reg)
		if bus != tc.bus || path != tc.path {
			t.Errorf("ParseAddress(%q) = %q, %q; want %q, %q", tc.reg, bus, path, tc.bus, tc.path)
		}
	}
}

func TestStoreOrderAndChanges(t *testing.T) {
	s := NewStore()
	feed, _ := s.Subscribe()
	a := &Item{Bus: ":1.1", Path: "/StatusNotifierItem", ID: "a"}
	b := &Item{Bus: ":1.2", Path: "/StatusNotifierItem", ID: "b"}
	s.Put(a)
	<-feed
	s.Put(b)
	<-feed

	// Registration order holds.
	items := s.Items()
	if len(items) != 2 || items[0].ID != "a" || items[1].ID != "b" {
		t.Fatalf("items = %+v", items)
	}

	// Replacing keeps the slot.
	a2 := &Item{Bus: ":1.1", Path: "/StatusNotifierItem", ID: "a", Title: "replaced"}
	s.Put(a2)
	<-feed
	items = s.Items()
	if len(items) != 2 || items[0].Title != "replaced" {
		t.Fatalf("after replace = %+v", items)
	}

	// Removal drops the slot and ticks.
	if !s.Remove(":1.1", "/StatusNotifierItem") {
		t.Fatal("remove missed the item")
	}
	<-feed
	if !s.Remove(":1.1", "/StatusNotifierItem") {
		// A double remove is false but harmless.
	} else {
		t.Fatal("double remove reported success")
	}
	items = s.Items()
	if len(items) != 1 || items[0].ID != "b" {
		t.Fatalf("after remove = %+v", items)
	}
}

func TestStoreNoTickOnMiss(t *testing.T) {
	s := NewStore()
	feed, _ := s.Subscribe()
	// Drain the (empty) buffer without blocking: a remove of a missing
	// item must not enqueue.
	if s.Remove("gone", "/x") {
		t.Fatal("removed a ghost")
	}
	select {
	case <-feed:
		t.Fatal("a missed remove ticked the store")
	default:
	}
}
