package domain

import (
	"fmt"
	"time"
)

type Panel struct {
	ID     string      `json:"id"`
	Title  string      `json:"title"`
	Signal SignalQuery `json:"signal"`
	// Width is in grid columns, 1-12.
	Width int `json:"width"`
}

type Dashboard struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Panels    []Panel   `json:"panels"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (d Dashboard) Validate() error {
	if d.Title == "" {
		return fmt.Errorf("%w: title is required", ErrInvalidQuery)
	}
	if len(d.Panels) > 50 {
		return fmt.Errorf("%w: at most 50 panels", ErrInvalidQuery)
	}
	for i, p := range d.Panels {
		if p.Width < 1 || p.Width > 12 {
			return fmt.Errorf("%w: panel %d: width must be 1-12", ErrInvalidQuery, i+1)
		}
		if err := p.Signal.Validate(); err != nil {
			return fmt.Errorf("panel %d: %w", i+1, err)
		}
	}
	return nil
}
