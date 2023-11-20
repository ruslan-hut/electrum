package internal

import (
	"context"
	"electrum/config"
	"electrum/models"
	"electrum/services"
	"fmt"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"log"
)

const (
	collectionLog            = "payment_log"
	collectionUserTags       = "user_tags"
	collectionTransactions   = "transactions"
	collectionPaymentMethods = "payment_methods"
	collectionPaymentOrders  = "payment_orders"
	collectionPayment        = "payment"
)

type MongoDB struct {
	ctx              context.Context
	clientOptions    *options.ClientOptions
	database         string
	logRecordsNumber int64
}

func (m *MongoDB) GetTransaction(id int) (*models.Transaction, error) {
	var transaction models.Transaction
	connection, err := m.connect()
	if err != nil {
		return nil, err
	}
	defer m.disconnect(connection)

	filter := bson.D{{"transaction_id", id}}
	collection := connection.Database(m.database).Collection(collectionTransactions)
	err = collection.FindOne(m.ctx, filter).Decode(&transaction)
	if err != nil {
		return nil, err
	}
	return &transaction, nil
}

func (m *MongoDB) GetPaymentMethod(userId string) (*models.PaymentMethod, error) {
	connection, err := m.connect()
	if err != nil {
		return nil, err
	}
	defer m.disconnect(connection)

	collection := connection.Database(m.database).Collection(collectionPaymentMethods)
	filter := bson.D{{"user_id", userId}, {"is_default", true}}
	var paymentMethod *models.PaymentMethod
	err = collection.FindOne(m.ctx, filter).Decode(&paymentMethod)
	if paymentMethod == nil {
		filter = bson.D{{"user_id", userId}}
		opt := options.FindOne().SetSort(bson.D{{"fail_count", 1}})
		err = collection.FindOne(m.ctx, filter, opt).Decode(&paymentMethod)
	}
	if err != nil {
		return nil, err
	}
	return paymentMethod, nil
}

func (m *MongoDB) GetPaymentMethodByIdentifier(identifier string) (*models.PaymentMethod, error) {
	connection, err := m.connect()
	if err != nil {
		return nil, err
	}
	defer m.disconnect(connection)

	collection := connection.Database(m.database).Collection(collectionPaymentMethods)
	filter := bson.D{{"identifier", identifier}}
	var paymentMethod *models.PaymentMethod
	err = collection.FindOne(m.ctx, filter).Decode(&paymentMethod)
	if err != nil {
		return nil, err
	}
	return paymentMethod, nil
}

func (m *MongoDB) UpdatePaymentMethodFailCount(identifier string, count int) error {
	connection, err := m.connect()
	if err != nil {
		return err
	}
	defer m.disconnect(connection)

	collection := connection.Database(m.database).Collection(collectionPaymentMethods)
	filter := bson.D{{"identifier", identifier}}
	update := bson.D{
		{"$set", bson.D{
			{"fail_count", count},
		}},
	}
	if _, err = collection.UpdateOne(m.ctx, filter, update); err != nil {
		return err
	}
	return nil
}

func (m *MongoDB) GetPaymentOrderByTransaction(transactionId int) (*models.PaymentOrder, error) {
	connection, err := m.connect()
	if err != nil {
		return nil, err
	}
	defer m.disconnect(connection)

	collection := connection.Database(m.database).Collection(collectionPaymentOrders)
	filter := bson.D{{"transaction_id", transactionId}, {"is_completed", false}}
	var order models.PaymentOrder
	if err = collection.FindOne(m.ctx, filter).Decode(&order); err != nil {
		return nil, err
	}
	return &order, nil
}

func (m *MongoDB) SavePaymentOrder(order *models.PaymentOrder) error {
	connection, err := m.connect()
	if err != nil {
		return err
	}
	defer m.disconnect(connection)

	filter := bson.D{{"order", order.Order}}
	set := bson.M{"$set": order}
	collection := connection.Database(m.database).Collection(collectionPaymentOrders)
	_, err = collection.UpdateOne(m.ctx, filter, set, options.Update().SetUpsert(true))
	if err != nil {
		return err
	}
	return nil
}

func (m *MongoDB) GetLastOrder() (*models.PaymentOrder, error) {
	connection, err := m.connect()
	if err != nil {
		return nil, err
	}
	defer m.disconnect(connection)

	collection := connection.Database(m.database).Collection(collectionPaymentOrders)
	filter := bson.D{}
	var order models.PaymentOrder
	if err = collection.FindOne(m.ctx, filter, options.FindOne().SetSort(bson.D{{"time_opened", -1}})).Decode(&order); err != nil {
		return nil, err
	}
	return &order, nil
}

func (m *MongoDB) connect() (*mongo.Client, error) {
	connection, err := mongo.Connect(m.ctx, m.clientOptions)
	if err != nil {
		return nil, err
	}
	return connection, nil
}

func (m *MongoDB) disconnect(connection *mongo.Client) {
	err := connection.Disconnect(m.ctx)
	if err != nil {
		log.Println("mongodb disconnect error", err)
	}
}

func NewMongoClient(conf *config.Config) (*MongoDB, error) {
	if !conf.Mongo.Enabled {
		return nil, nil
	}
	connectionUri := fmt.Sprintf("mongodb://%s:%s", conf.Mongo.Host, conf.Mongo.Port)
	clientOptions := options.Client().ApplyURI(connectionUri)
	if conf.Mongo.User != "" {
		clientOptions.SetAuth(options.Credential{
			Username:   conf.Mongo.User,
			Password:   conf.Mongo.Password,
			AuthSource: conf.Mongo.Database,
		})
	}
	client := &MongoDB{
		ctx:              context.Background(),
		clientOptions:    clientOptions,
		database:         conf.Mongo.Database,
		logRecordsNumber: conf.LogRecords,
	}
	return client, nil
}

func (m *MongoDB) WriteLogMessage(data services.Data) error {
	connection, err := m.connect()
	if err != nil {
		return err
	}
	defer m.disconnect(connection)
	collection := connection.Database(m.database).Collection(collectionLog)
	_, err = collection.InsertOne(m.ctx, data)
	return err
}

func (m *MongoDB) GetUserTag(id string) (*models.UserTag, error) {
	connection, err := m.connect()
	if err != nil {
		return nil, err
	}
	defer m.disconnect(connection)

	filter := bson.D{{"id_tag", id}}
	collection := connection.Database(m.database).Collection(collectionUserTags)
	var userTag models.UserTag
	err = collection.FindOne(m.ctx, filter).Decode(&userTag)
	if err != nil {
		return nil, err
	}
	return &userTag, nil
}

func (m *MongoDB) SavePaymentResult(paymentParameters *models.PaymentParameters) error {
	connection, err := m.connect()
	if err != nil {
		return err
	}
	defer m.disconnect(connection)

	collection := connection.Database(m.database).Collection(collectionPayment)
	filter := bson.M{"order": paymentParameters.Order}
	update := bson.M{
		"$set": paymentParameters, // This will set the fields of the document to the values in paymentParameters
	}
	option := options.Update().SetUpsert(true) // If no document is found, create a new one.
	_, err = collection.UpdateOne(m.ctx, filter, update, option)
	if err != nil {
		return err
	}
	return nil
}

func (m *MongoDB) GetPaymentOrder(id int) (*models.PaymentOrder, error) {
	connection, err := m.connect()
	if err != nil {
		return nil, err
	}
	defer m.disconnect(connection)

	collection := connection.Database(m.database).Collection(collectionPaymentOrders)
	filter := bson.D{{"order", id}}
	var order models.PaymentOrder
	if err = collection.FindOne(m.ctx, filter).Decode(&order); err != nil {
		return nil, err
	}
	return &order, nil
}

// UpdateTransaction update transaction billed data
func (m *MongoDB) UpdateTransaction(transaction *models.Transaction) error {
	connection, err := m.connect()
	if err != nil {
		return err
	}
	defer m.disconnect(connection)

	collection := connection.Database(m.database).Collection(collectionTransactions)
	filter := bson.D{{"transaction_id", transaction.Id}}
	update := bson.D{
		{"$set", bson.D{
			{"payment_order", transaction.PaymentOrder},
			{"payment_billed", transaction.PaymentBilled},
		}},
	}
	if _, err = collection.UpdateOne(m.ctx, filter, update); err != nil {
		return err
	}
	return nil
}
