package internal

import (
	"electrum/config"
	"electrum/models"
	"electrum/services"
	"encoding/json"
	"fmt"
	"github.com/julienschmidt/httprouter"
	"io"
	"net"
	"net/http"
	"strconv"
)

const (
	payTransaction = "/pay/:transaction_id"
	returnPayment  = "/return/:transaction_id"
	returnByOrder  = "/return/order/:order_id"
)

type Server struct {
	conf       *config.Config
	httpServer *http.Server
	payments   services.Payments
	logger     services.LogHandler
}

func NewServer(conf *config.Config) *Server {

	server := Server{
		conf: conf,
	}

	// register itself as a router for httpServer handler
	router := httprouter.New()
	server.Register(router)
	server.httpServer = &http.Server{
		Handler: router,
	}

	return &server
}

func (s *Server) Register(router *httprouter.Router) {
	router.GET(payTransaction, s.payTransaction)
	router.GET(returnPayment, s.returnTransaction)
	router.POST(returnByOrder, s.returnOrder)
}

func (s *Server) SetPaymentsService(payments services.Payments) {
	s.payments = payments
}

func (s *Server) SetLogger(logger services.LogHandler) {
	s.logger = logger
}

func (s *Server) Start() error {
	if s.conf == nil {
		return fmt.Errorf("configuration not loaded")
	}

	serverAddress := fmt.Sprintf("%s:%s", s.conf.Listen.BindIP, s.conf.Listen.Port)
	listener, err := net.Listen("tcp", serverAddress)
	if err != nil {
		return err
	}

	if s.conf.Listen.TLS {
		s.logger.Info(fmt.Sprintf("starting https TLS on %s", serverAddress))
		err = s.httpServer.ServeTLS(listener, s.conf.Listen.CertFile, s.conf.Listen.KeyFile)
	} else {
		s.logger.Info(fmt.Sprintf("starting http on %s", serverAddress))
		err = s.httpServer.Serve(listener)
	}

	return err
}

func (s *Server) payTransaction(w http.ResponseWriter, _ *http.Request, ps httprouter.Params) {
	transactionId := ps.ByName("transaction_id")
	if transactionId == "" {
		s.logger.Warn("empty transaction id")
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	id, err := strconv.Atoi(transactionId)
	if err != nil {
		s.logger.Warn(fmt.Sprintf("invalid transaction id: %s; %v", transactionId, err))
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	err = s.payments.PayTransaction(id)
	if err != nil {
		s.logger.Error(fmt.Sprintf("pay transaction %v", id), err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (s *Server) returnOrder(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	orderId := ps.ByName("order_id")
	if orderId == "" {
		s.logger.Warn("return order: empty order id")
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		s.logger.Error("return order: read request body", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	var order models.PaymentOrder
	err = json.Unmarshal(body, &order)
	if err != nil {
		s.logger.Error("return order: decode request body", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	s.logger.Info(fmt.Sprintf("processing request: return order %s, amount %d", orderId, order.Amount))
	err = s.payments.ReturnByOrder(orderId, order.Amount)
	if err != nil {
		s.logger.Error(fmt.Sprintf("return order %s", orderId), err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (s *Server) returnTransaction(w http.ResponseWriter, _ *http.Request, ps httprouter.Params) {
	transactionId := ps.ByName("transaction_id")
	if transactionId == "" {
		s.logger.Warn("empty transaction id")
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	id, err := strconv.Atoi(transactionId)
	if err != nil {
		s.logger.Warn(fmt.Sprintf("invalid transaction id: %s; %v", transactionId, err))
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	err = s.payments.ReturnPayment(id)
	if err != nil {
		s.logger.Error(fmt.Sprintf("return transaction %v", id), err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}
