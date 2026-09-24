package clock

import (
	"testing"
	"time"
)

// TestReal_NeverNil asserts Real always returns a usable Clock.
func TestReal_NeverNil(t *testing.T) {
	c := Real()
	if c == nil {
		t.Fatal("Real() returned nil")
	}
	before := time.Now()
	got := c.Now()
	if got.Before(before) {
		t.Errorf("Real().Now() = %v before %v", got, before)
	}
}

// TestFake_SetAndAdvance exercises the basic manipulation API.
func TestFake_SetAndAdvance(t *testing.T) {
	start := time.Date(2026, 9, 23, 0, 0, 0, 0, time.UTC)
	f := NewFake(start)
	if got := f.Now(); !got.Equal(start) {
		t.Errorf("initial Now() = %v, want %v", got, start)
	}
	f.Advance(2 * time.Hour)
	want := start.Add(2 * time.Hour)
	if got := f.Now(); !got.Equal(want) {
		t.Errorf("after Advance(2h) Now() = %v, want %v", got, want)
	}
	f.Set(start)
	if got := f.Now(); !got.Equal(start) {
		t.Errorf("after Set(start) Now() = %v, want %v", got, start)
	}
}

// TestFake_NegativeAdvance asserts rewind works.
func TestFake_NegativeAdvance(t *testing.T) {
	start := time.Date(2026, 9, 23, 0, 0, 0, 0, time.UTC)
	f := NewFake(start)
	f.Advance(-time.Hour)
	want := start.Add(-time.Hour)
	if got := f.Now(); !got.Equal(want) {
		t.Errorf("after rewind Now() = %v, want %v", got, want)
	}
}

// TestFake_ConcurrentSafe exercises the mutex via -race. No assertions on
// output beyond no panic / no data race.
func TestFake_ConcurrentSafe(t *testing.T) {
	f := NewFake(time.Now())
	done := make(chan struct{})
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				_ = f.Now()
				f.Advance(time.Millisecond)
			}
			done <- struct{}{}
		}()
	}
	for i := 0; i < 10; i++ {
		<-done
	}
}