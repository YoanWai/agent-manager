package settings

// Cache mirrors the store's settings so frames and the dialog read them
// without a store call.
type Cache struct {
	values map[string]string
	hidden map[string]bool
	// editors is the last PATH probe, kept so a dialog rebuilt from a
	// later load still lists what was found.
	editors *EditorsProbe
}

// Value is the cached value of one key.
func (c Cache) Value(key string) string { return c.values[key] }

// Values is every cached value, as the dialog copies them.
func (c Cache) Values() map[string]string { return c.values }

// Hidden is the cached hidden-CLI set.
func (c Cache) Hidden() map[string]bool { return c.hidden }

// Editors is the last PATH probe.
func (c Cache) Editors() *EditorsProbe { return c.editors }

// SetEditors keeps a probe answer for the rows a later load rebuilds.
func (c *Cache) SetEditors(probe *EditorsProbe) { c.editors = probe }

// ApplyValues mirrors the values a save just wrote.
func (c *Cache) ApplyValues(values []Value) {
	c.ensureValues()
	for _, value := range values {
		c.values[value.Key] = value.Value
	}
}

// ApplyHidden mirrors the hidden-CLI set a save just wrote.
func (c *Cache) ApplyHidden(names []string) {
	c.hidden = make(map[string]bool, len(names))
	for _, name := range names {
		c.hidden[name] = true
	}
}

// ApplyReadback mirrors a store read: a key whose read failed keeps its
// current value.
func (c *Cache) ApplyReadback(values []Readback, hiddenRaw string, hiddenErr error) {
	c.ensureValues()
	for _, value := range values {
		if value.Err == nil {
			c.values[value.Key] = value.Value
		}
	}
	if hiddenErr == nil {
		c.hidden = ParseHidden(hiddenRaw)
	}
}

func (c *Cache) ensureValues() {
	if c.values == nil {
		c.values = make(map[string]string)
	}
}
