package main

import (
	"electrum/config"
	"electrum/internal"
	"electrum/services"
	"flag"
	"fmt"
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

	if conf.DisablePayment {
		logger.Warn("payment disabled")
	} else {
		logger.Info("payment enabled")
	}

	logger.Info(fmt.Sprintf("merchant: %s; terminal: %s; request url: %s", conf.Merchant.Code, conf.Merchant.Terminal, conf.Merchant.RequestUrl))

	var mongo services.Database
	if conf.Mongo.Enabled {
		mongo, err = internal.NewMongoClient(conf)
		if err != nil {
			logger.Error("mongo client", err)
			return
		}
		logger.Info("mongo client initialized")
	}

	payments := internal.NewPayments(conf)
	payments.SetLogger(internal.NewLogger("payments", conf.IsDebug, mongo))
	payments.SetDatabase(mongo)

	server := internal.NewServer(conf)
	server.SetLogger(internal.NewLogger("server", conf.IsDebug, mongo))
	server.SetPaymentsService(payments)

	err = server.Start()
	if err != nil {
		logger.Error("server start", err)
		return
	}

}
