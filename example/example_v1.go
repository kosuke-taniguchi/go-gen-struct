//go:generate go run ../main.go
package example

import (
	"log"
	"time"
)

func add(n, m int) {
	log.Println(n + m)
}

//gen:setters
type example struct {
	Name      string
	CreatedAt time.Time
	UpdatedAt time.Time
}
