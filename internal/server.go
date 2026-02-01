package internal

import (
	"electrum/config"
	"electrum/entity"
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
	paymentNotify  = "/notify"
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
	router.POST(paymentNotify, s.paymentNotify)
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

func (s *Server) payTransaction(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	// Add request ID for tracing
	ctx := WithRequestID(r.Context())
	reqID := GetRequestID(ctx)

	transactionId := ps.ByName("transaction_id")
	if transactionId == "" {
		s.logger.Warn(fmt.Sprintf("[%s] empty transaction id", reqID))
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	id, err := strconv.Atoi(transactionId)
	if err != nil {
		s.logger.Warn(fmt.Sprintf("[%s] invalid transaction id: %s; %v", reqID, transactionId, err))
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	err = s.payments.PayTransaction(ctx, id)
	if err != nil {
		s.logger.Error(fmt.Sprintf("[%s] pay transaction %v", reqID, id), err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (s *Server) returnOrder(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	// Add request ID for tracing
	ctx := WithRequestID(r.Context())
	reqID := GetRequestID(ctx)

	orderId := ps.ByName("order_id")
	if orderId == "" {
		s.logger.Warn(fmt.Sprintf("[%s] return order: empty order id", reqID))
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		s.logger.Error(fmt.Sprintf("[%s] return order: read request body", reqID), err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	var order entity.PaymentOrder
	err = json.Unmarshal(body, &order)
	if err != nil {
		s.logger.Error(fmt.Sprintf("[%s] return order: decode request body", reqID), err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	s.logger.Info(fmt.Sprintf("[%s] processing request: return order %s, amount %d", reqID, orderId, order.Amount))
	err = s.payments.ReturnByOrder(ctx, orderId, order.Amount)
	if err != nil {
		s.logger.Error(fmt.Sprintf("[%s] return order %s", reqID, orderId), err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (s *Server) returnTransaction(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	// Add request ID for tracing
	ctx := WithRequestID(r.Context())
	reqID := GetRequestID(ctx)

	transactionId := ps.ByName("transaction_id")
	if transactionId == "" {
		s.logger.Warn(fmt.Sprintf("[%s] empty transaction id", reqID))
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	id, err := strconv.Atoi(transactionId)
	if err != nil {
		s.logger.Warn(fmt.Sprintf("[%s] invalid transaction id: %s; %v", reqID, transactionId, err))
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	err = s.payments.ReturnPayment(ctx, id)
	if err != nil {
		s.logger.Error(fmt.Sprintf("[%s] return transaction %v", reqID, id), err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (s *Server) paymentNotify(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	// Add request ID for tracing
	ctx := WithRequestID(r.Context())
	reqID := GetRequestID(ctx)

	body, err := io.ReadAll(r.Body)
	if err != nil {
		s.logger.Error(fmt.Sprintf("[%s] payment notify: get body", reqID), err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	err = s.payments.Notify(ctx, body)
	if err != nil {
		s.logger.Error(fmt.Sprintf("[%s] payment notify: process body", reqID), err)
	}
	w.WriteHeader(http.StatusOK)
}
