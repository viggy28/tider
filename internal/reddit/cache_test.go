package reddit

import (
	"testing"
	"time"
)

func TestCachePutGetFresh(t *testing.T) {
	c := NewCache(t.TempDir())
	if err := c.Put("golang", "about.json", []byte(`{"x":1}`), time.Hour); err != nil {
		t.Fatalf("put: %v", err)
	}
	data, fresh, err := c.Get("golang", "about.json")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !fresh {
		t.Fatal("expected fresh entry")
	}
	if string(data) != `{"x":1}` {
		t.Fatalf("data = %q", data)
	}
}

func TestCacheExpired(t *testing.T) {
	c := NewCache(t.TempDir())
	if err := c.Put("golang", "hot.json", []byte(`x`), time.Millisecond); err != nil {
		t.Fatalf("put: %v", err)
	}
	time.Sleep(10 * time.Millisecond)
	_, fresh, err := c.Get("golang", "hot.json")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if fresh {
		t.Fatal("expected stale entry")
	}
}

func TestCacheMissing(t *testing.T) {
	c := NewCache(t.TempDir())
	_, fresh, err := c.Get("nonexistent", "file.json")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if fresh {
		t.Fatal("expected miss")
	}
}

func TestCacheMultipleFiles(t *testing.T) {
	c := NewCache(t.TempDir())
	if err := c.Put("golang", "about.json", []byte(`a`), time.Hour); err != nil {
		t.Fatal(err)
	}
	if err := c.Put("golang", "rules.json", []byte(`r`), time.Hour); err != nil {
		t.Fatal(err)
	}
	a, _, _ := c.Get("golang", "about.json")
	r, _, _ := c.Get("golang", "rules.json")
	if string(a) != "a" || string(r) != "r" {
		t.Fatalf("crosstalk: a=%q r=%q", a, r)
	}
}
