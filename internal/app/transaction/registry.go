package transaction

import (
	"log"

	"github.com/go-chi/chi"
	"github.com/tunaiku/mobilebanking/internal/app/domain"
	"github.com/tunaiku/mobilebanking/internal/app/transaction/handler"
	"github.com/tunaiku/mobilebanking/internal/pkg/pg"
	"go.uber.org/dig"
)

func Register(container *dig.Container) {
	container.Provide(func(
		userSessionHelper domain.UserSessionHelper,
		accountInfoService domain.AccountInformationService,
		txInfoService domain.TransactionInformationService,
		pgWrapper *pg.CrudRepositoryWrapper,
	) *handler.TransactionEndpoint {
		return handler.NewTransactionEndpoint(
			userSessionHelper,
			accountInfoService,
			txInfoService,
			pgWrapper,
		)
	})
}

func Invoke(container *dig.Container) {
	err := container.Invoke(func(router chi.Router, endpoint *handler.TransactionEndpoint) {
		log.Println("invoke transaction startup ...")
		endpoint.BindRoutes(router)
	})
	if err != nil {
		log.Fatal(err)
	}
}
