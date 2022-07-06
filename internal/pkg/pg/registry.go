package pg

import (
	"github.com/go-pg/pg/v10"
	"go.uber.org/dig"
)

func Register(container *dig.Container) {

	container.Provide(func() *pg.Options {
		return &pg.Options{
			Addr:     "localhost:5432",
			Database: "mobile-banking-service",
			User:     "postgres",
			Password: "postgres",
		}
	})

	container.Provide(func(opts *pg.Options) *pg.DB {
		return pg.Connect(opts)
	})

	container.Provide(func(db *pg.DB) *CrudRepositoryWrapper {
		return Wrap(db)
	})
}
