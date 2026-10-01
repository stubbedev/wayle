// Package sni is the StatusNotifierItem tray service: the watcher and
// host D-Bus roles, item discovery, and the snapshot store the bar
// module renders.
package sni

import (
	"strings"
	"sync"

	"github.com/stubbedev/wayle/internal/feed"
)

// Category describes the kind of application behind an item.
type Category string

// Categories.
const (
	CategoryApplicationStatus Category = "ApplicationStatus"
	CategoryCommunications    Category = "Communications"
	CategorySystemServices    Category = "SystemServices"
	CategoryHardware          Category = "Hardware"
)

// ParseCategory defaults unknown strings, per the SNI spec.
func ParseCategory(s string) Category {
	switch Category(s) {
	case CategoryCommunications, CategorySystemServices, CategoryHardware:
		return Category(s)
	}
	return CategoryApplicationStatus
}

// Status describes how prominent an item should be.
type Status string

// Statuses.
const (
	StatusPassive        Status = "Passive"
	StatusActive         Status = "Active"
	StatusNeedsAttention Status = "NeedsAttention"
)

// ParseStatus defaults unknown strings, per the SNI spec.
func ParseStatus(s string) Status {
	switch Status(s) {
	case StatusActive, StatusNeedsAttention:
		return Status(s)
	}
	return StatusPassive
}

// Pixmap is ARGB32 icon data in network byte order.
type Pixmap struct {
	Width  int32
	Height int32
	Data   []byte
}

// Tooltip is the item's hover information.
type Tooltip struct {
	IconName    string
	IconPixmap  []Pixmap
	Title       string
	Description string
}

// Item is one tray item's snapshot (the SNI properties the bar needs).
type Item struct {
	// Bus is the item's unique or well-known bus name; Path the
	// StatusNotifierItem object path.
	Bus  string
	Path string

	ID         string
	Title      string
	Category   Category
	Status     Status
	ItemIsMenu bool

	IconName        string
	IconPixmap      []Pixmap
	OverlayIconName string
	AttentionName   string
	AttentionMovie  string
	IconThemePath   string
	Tooltip         Tooltip
	MenuPath        string
}

// Key is the store's identity for an item.
func (it *Item) Key() string { return it.Bus + it.Path }

// ParseAddress splits a watcher registration ("bus/path") into its
// bus name and object path. A missing path defaults to
// /StatusNotifierItem, as most items register bare.
func ParseAddress(reg string) (bus, path string) {
	i := strings.Index(reg, "/")
	if i < 0 {
		return reg, "/StatusNotifierItem"
	}
	return reg[:i], reg[i:]
}

// Store is the item set with change feeds.
type Store struct {
	mu    sync.Mutex
	items map[string]*Item
	order []string
	ticks *feed.Tick
}

// NewStore builds an empty store.
func NewStore() *Store {
	return &Store{items: make(map[string]*Item), ticks: feed.NewTick()}
}

// Subscribe returns a feed that ticks whenever the item set or an
// item's snapshot changes. Every subscriber (one tray per output) sees
// every change; stop ends the feed.
func (s *Store) Subscribe() (<-chan struct{}, func()) { return s.ticks.Subscribe() }

// tick signals every feed, coalescing pending ticks per feed.
func (s *Store) tick() { feed.Notify(s.ticks) }

// Items snapshots the item list in registration order.
func (s *Store) Items() []Item {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Item, 0, len(s.order))
	for _, key := range s.order {
		if it, ok := s.items[key]; ok {
			out = append(out, *it)
		}
	}
	return out
}

// Get returns one item by key.
func (s *Store) Get(key string) (Item, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	it, ok := s.items[key]
	if !ok {
		return Item{}, false
	}
	return *it, true
}

// Put inserts or replaces an item, preserving registration order.
func (s *Store) Put(it *Item) {
	s.mu.Lock()
	key := it.Key()
	if _, ok := s.items[key]; !ok {
		s.order = append(s.order, key)
	}
	s.items[key] = it
	s.mu.Unlock()
	s.tick()
}

// Remove drops an item by its registration address.
func (s *Store) Remove(bus, path string) bool {
	s.mu.Lock()
	key := bus + path
	_, ok := s.items[key]
	if ok {
		delete(s.items, key)
		for i, k := range s.order {
			if k == key {
				s.order = append(s.order[:i], s.order[i+1:]...)
				break
			}
		}
	}
	s.mu.Unlock()
	if ok {
		s.tick()
	}
	return ok
}
