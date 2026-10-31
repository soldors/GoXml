// Package starter is the handwritten half of the CLI starter example.
package starter

import "strconv"

// ViewModel is updated by UI-thread event handlers. Background writers would
// need their own synchronization; Invalidate does not protect these fields.
type ViewModel struct {
	Title string
	Count int
}

func NewViewModel() *ViewModel {
	return &ViewModel{Title: "Clicked 0 times"}
}

// Increment prepares the display string outside the Layout hot path.
func (m *ViewModel) Increment() {
	m.Count++
	m.Title = "Clicked " + strconv.Itoa(m.Count) + " times"
}
