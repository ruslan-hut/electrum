package main

import (
	"electrum/config"
	"electrum/internal"
	"electrum/services"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	logger := internal.NewLogger("internal", false, nil)

	configPath := flag.String("conf", "config.yml", "path to config file")
	flag.Parse()

	logger.Info("using config file: " + *configPath)
	conf, err := config.GetConfig(*configPath)
	if err != nil {
		logger.Error("boot", err)
		return
	}

	logger.Info(fmt.Sprintf("merchant: %s; terminal: %s; request url: %s", conf.Merchant.Code, conf.Merchant.Terminal, conf.Merchant.RequestUrl))

	var mongo *internal.MongoDB
	var database services.Database // Use interface type to properly handle nil
	if conf.Mongo.Enabled {
		mongo, err = internal.NewMongoClient(conf)
		if err != nil {
			logger.Error("mongo client", err)
			return
		}
		logger.Info("mongo client initialized")
		database = mongo // Only assign to interface if not nil

		// Ensure MongoDB connection is closed on shutdown
		defer func() {
			if err := mongo.Disconnect(); err != nil {
				logger.Error("mongo disconnect", err)
			} else {
				logger.Info("mongo connection closed")
			}
		}()
	}

	// Setup signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		sig := <-sigChan
		logger.Info(fmt.Sprintf("received signal %s, shutting down gracefully", sig))
		os.Exit(0)
	}()

	payments := internal.NewPayments(conf)
	payments.SetLogger(internal.NewLogger("payments", conf.IsDebug, database))
	payments.SetDatabase(database)

	server := internal.NewServer(conf)
	server.SetLogger(internal.NewLogger("server", conf.IsDebug, database))
	server.SetPaymentsService(payments)

	err = server.Start()
	if err != nil {
		logger.Error("server start", err)
		return
	}
}
