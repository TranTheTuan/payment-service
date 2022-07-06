package handler

import (
	"fmt"
	"log"
	"math/big"
	"net/http"
	"time"

	"github.com/go-chi/chi"
	"github.com/go-chi/render"
	"github.com/google/uuid"
	"github.com/tunaiku/mobilebanking/internal/app/domain"
	"github.com/tunaiku/mobilebanking/internal/pkg/jwt"
	"github.com/tunaiku/mobilebanking/internal/pkg/pg"
)

type TransactionEndpoint struct {
	userSessionHelper  domain.UserSessionHelper
	accountInfoService domain.AccountInformationService
	txInfoService      domain.TransactionInformationService
	pgWrapper          *pg.CrudRepositoryWrapper
}

func NewTransactionEndpoint(
	userSessionHelper domain.UserSessionHelper,
	accountInfoService domain.AccountInformationService,
	txInfoService domain.TransactionInformationService,
	pgWrapper *pg.CrudRepositoryWrapper,
) *TransactionEndpoint {
	return &TransactionEndpoint{
		userSessionHelper:  userSessionHelper,
		accountInfoService: accountInfoService,
		txInfoService:      txInfoService,
		pgWrapper:          pgWrapper,
	}
}

func (TransactionEndpoint *TransactionEndpoint) BindRoutes(r chi.Router) {
	r.Group(func(r chi.Router) {
		r = jwt.WrapChiRouterWithAuthorization(r)
		r.Post("/transaction", TransactionEndpoint.HandleCreateTransaction)
		r.Put("/transaction/{id}/verify", TransactionEndpoint.HandleVerifyTransaction)
		r.Get("/transaction/{id}", TransactionEndpoint.HandleGetTransaction)
	})

}

func (transactionEndpoint *TransactionEndpoint) HandleCreateTransaction(w http.ResponseWriter, r *http.Request) {
	request := &CreateTransactionRequest{}
	userSession, err := transactionEndpoint.userSessionHelper.GetFromContext(r.Context())
	if err != nil {
		render.Render(w, r, &TransactionHandlerFailed{
			HttpCode: http.StatusInternalServerError,
			Message:  err.Error(),
		})
		return
	}
	log.Println(userSession.ID)
	if err := request.Bind(r); err != nil {
		render.Render(w, r, &TransactionHandlerFailed{
			HttpCode: http.StatusInternalServerError,
			Message:  err.Error(),
		})
		return
	}

	if request.DestinationAccount == userSession.AccountReference {
		render.Render(w, r, &TransactionHandlerFailed{
			HttpCode: http.StatusBadRequest,
			Message:  "destination account cannot be same as source account",
		})
		return
	}

	// init tranasction model
	transaction := &domain.Transaction{
		ID:                 uuid.New().String(),
		UserID:             userSession.ID,
		TransactionCode:    request.TransactionCode,
		DestinationAccount: request.DestinationAccount,
		SourceAccount:      userSession.AccountReference,
		State:              domain.WaitAuthorization,
		CreatedAt:          time.Now(),
	}

	isTxCodeValid, err := transactionEndpoint.checkTxAllowed(userSession.AccountReference, request.TransactionCode)
	if err != nil {
		render.Render(w, r, &TransactionHandlerFailed{
			HttpCode: http.StatusBadRequest,
			Message:  "account not found",
		})
		return
	}
	if !isTxCodeValid {
		render.Render(w, r, &TransactionHandlerFailed{
			HttpCode: http.StatusBadRequest,
			Message:  "transaction not allowed",
		})
		return
	}

	isTxCodeValid, err = transactionEndpoint.checkTxAllowed(request.DestinationAccount, request.TransactionCode)
	if err != nil {
		render.Render(w, r, &TransactionHandlerFailed{
			HttpCode: http.StatusBadRequest,
			Message:  "account not found",
		})
		return
	}
	if !isTxCodeValid {
		render.Render(w, r, &TransactionHandlerFailed{
			HttpCode: http.StatusBadRequest,
			Message:  "transaction not allowed",
		})
		return
	}

	txDetail, err := transactionEndpoint.txInfoService.FindTransactionDetailByCode(request.TransactionCode)
	if err != nil {
		render.Render(w, r, &TransactionHandlerFailed{
			HttpCode: http.StatusBadRequest,
			Message:  err.Error(),
		})
		return
	}

	isValid, txAmount := transactionEndpoint.checkTxAmount(request.Amount, txDetail.MinimumAmount)
	if !isValid {
		render.Render(w, r, &TransactionHandlerFailed{
			HttpCode: http.StatusBadRequest,
			Message:  "amount does not reach the minimum transaction amount",
		})
		return
	}
	transaction.Amount = txAmount

	switch request.AuthorizationMethod {
	case "otp":
		if !userSession.ConfiguredTransactionCredential.IsOtpConfigured() {
			render.Render(w, r, &TransactionHandlerFailed{
				HttpCode: http.StatusBadRequest,
				Message:  "authorization method not configured",
			})
			return
		}
		transaction.AuthorizationMethod = domain.OtpAuthorization
	case "pin":
		if !userSession.ConfiguredTransactionCredential.IsPinConfigured() {
			render.Render(w, r, &TransactionHandlerFailed{
				HttpCode: http.StatusBadRequest,
				Message:  "authorization method not configured",
			})
			return
		}
		transaction.AuthorizationMethod = domain.PinAuthorization
	default:
		render.Render(w, r, &TransactionHandlerFailed{
			HttpCode: http.StatusBadRequest,
			Message:  "unsupported authorization method",
		})
		return
	}

	err = transactionEndpoint.pgWrapper.Save(transaction)
	if err != nil {
		render.Render(w, r, &TransactionHandlerFailed{
			HttpCode: http.StatusInternalServerError,
			Message:  err.Error(),
		})
		return
	}

	render.JSON(w, r, &CreateTransactionSuccess{TransactionID: transaction.ID})
}

func (transactionEndpoint *TransactionEndpoint) checkTxCodeValid(txCode string, privileges domain.TransactionPrivileges) bool {
	for _, code := range privileges.Codes {
		if code == txCode {
			return true
		}
	}
	return false
}

func (transactionEndpoint *TransactionEndpoint) checkTxAmount(txAmount *big.Float, minTxAmount *big.Float) (bool, float64) {
	if txAmount.Cmp(minTxAmount) < 0 {
		return false, 0
	}
	amount, _ := txAmount.Float64()
	return true, amount
}

func (transactionEndpoint *TransactionEndpoint) checkTxAllowed(userAccount string, txCode string) (bool, error) {
	txPrivileges, err := transactionEndpoint.accountInfoService.GetTransactionPrivileges(userAccount)
	if err != nil {
		return false, err
	}

	isTxCodeValid := transactionEndpoint.checkTxCodeValid(txCode, txPrivileges)
	return isTxCodeValid, nil
}

func (transactionEndpoint *TransactionEndpoint) HandleVerifyTransaction(w http.ResponseWriter, r *http.Request) {
	request := &VerifyTransactionRequest{}
	id := chi.URLParam(r, "id")
	if err := request.Bind(r); err != nil {
		render.Render(w, r, &TransactionHandlerFailed{
			HttpCode: http.StatusInternalServerError,
			Message:  err.Error(),
		})
	}
	if id != "1111" {
		render.Render(w, r, &TransactionHandlerFailed{
			HttpCode: http.StatusNotFound,
			Message:  "transaction not found",
		})
		return
	}
	if request.Credential != "123456" {
		render.Render(w, r, &TransactionHandlerFailed{
			HttpCode: http.StatusBadRequest,
			Message:  "invalid credential",
		})
		return
	}

	render.JSON(w, r, &VerifyTransactionSuccess{
		TransactionID: id,
	})
}

func (transactionEndpoint *TransactionEndpoint) HandleGetTransaction(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	fmt.Println("transaction id", id)
	render.JSON(w, r, &GetTransactionSuccess{})
}
