package example

import (
	"time"
)

func (s *example) SetCreatedAt(v time.Time) {
	s.CreatedAt = v
}

func (s *example) SetUpdatedAt(v time.Time) {
	s.UpdatedAt = v
}
