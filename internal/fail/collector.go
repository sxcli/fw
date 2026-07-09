package fail

import "fmt"

// Fail records one formatted violation.
func (c *Collector) Fail(format string, args ...any) {
	c.errs = append(c.errs, fmt.Errorf(format, args...))
}

// Add records one pre-built error.
func (c *Collector) Add(err error) {
	c.errs = append(c.errs, err)
}

// All returns every recorded violation in occurrence order.
func (c *Collector) All() []error {
	return c.errs
}

// Len returns the number of recorded violations.
func (c *Collector) Len() int {
	return len(c.errs)
}
