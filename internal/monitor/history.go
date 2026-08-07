package monitor

// History is a bounded chronological ring of float64 samples.
type History struct {
	values []float64
	start  int
	size   int
}

// NewHistory creates a history with capacity entries.
func NewHistory(capacity int) *History {
	if capacity < 0 {
		capacity = 0
	}
	return &History{values: make([]float64, capacity)}
}

// Add appends value, discarding the oldest sample when the history is full.
func (h *History) Add(value float64) {
	if len(h.values) == 0 {
		return
	}
	if h.size < len(h.values) {
		index := (h.start + h.size) % len(h.values)
		h.values[index] = value
		h.size++
		return
	}

	h.values[h.start] = value
	h.start = (h.start + 1) % len(h.values)
}

// Values returns samples from oldest to newest.
func (h *History) Values() []float64 {
	result := make([]float64, h.size)
	if h.size == 0 {
		return result
	}
	for i := range result {
		result[i] = h.values[(h.start+i)%len(h.values)]
	}
	return result
}

// Clear removes all samples while retaining the configured capacity.
func (h *History) Clear() {
	h.start = 0
	h.size = 0
}

// Len returns the number of samples currently stored.
func (h *History) Len() int {
	return h.size
}

// Capacity returns the maximum number of samples retained.
func (h *History) Capacity() int {
	return len(h.values)
}

// Resize changes the capacity and retains the newest samples that fit.
func (h *History) Resize(capacity int) {
	if capacity < 0 {
		capacity = 0
	}
	if capacity == h.Capacity() {
		return
	}

	old := h.Values()
	if len(old) > capacity {
		old = old[len(old)-capacity:]
	}
	resized := NewHistory(capacity)
	for _, value := range old {
		resized.Add(value)
	}
	*h = *resized
}
