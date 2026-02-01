package internal

import (
	"context"
	"electrum/config"
	"electrum/entity"
	"electrum/services"
	"errors"
	"fmt"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const (
	collectionLog            = "payment_log"
	collectionUserTags       = "user_tags"
	collectionTransactions   = "transactions"
	collectionPaymentMethods = "payment_methods"
	collectionPaymentOrders  = "payment_orders"
	collectionPayment        = "payment"
)

// MongoDB provides database operations for the Electrum payment service.
// It maintains a persistent connection pool to MongoDB for optimal performance.
type MongoDB struct {
	client           *mongo.Client
	database         string
	logRecordsNumber int64
}

// GetTransaction retrieves a transaction by ID from the database.
func (m *MongoDB) GetTransaction(ctx context.Context, id int) (*entity.Transaction, error) {
	var transaction entity.Transaction
	filter := bson.D{{Key: "transaction_id", Value: id}}
	collection := m.client.Database(m.database).Collection(collectionTransactions)
	err := collection.FindOne(ctx, filter).Decode(&transaction)
	if err != nil {
		return nil, fmt.Errorf("get transaction %d: %w", id, err)
	}
	return &transaction, nil
}

// GetPaymentMethod retrieves the best available payment method for a user.
// It first tries to get the default payment method, then falls back to the
// method with the lowest fail count if no default exists or the default has failures.
func (m *MongoDB) GetPaymentMethod(ctx context.Context, userId string) (*entity.PaymentMethod, error) {
	coll := m.client.Database(m.database).Collection(collectionPaymentMethods)

	// 1. try to get default
	var pm entity.PaymentMethod
	err := coll.FindOne(ctx, bson.D{{Key: "user_id", Value: userId}, {Key: "is_default", Value: true}}).Decode(&pm)
	if err != nil && !errors.Is(err, mongo.ErrNoDocuments) {
		return nil, fmt.Errorf("get payment method for user %s: %w", userId, err)
	}

	// 2. if no default or fail count > 0, search for min fail count
	if errors.Is(err, mongo.ErrNoDocuments) || pm.FailCount > 0 {
		opt := options.FindOne().SetSort(bson.D{{Key: "fail_count", Value: 1}, {Key: "_id", Value: 1}})
		if err = coll.FindOne(ctx, bson.D{{Key: "user_id", Value: userId}}, opt).Decode(&pm); err != nil {
			return nil, fmt.Errorf("get payment method fallback for user %s: %w", userId, err)
		}
	}

	return &pm, nil
}

// GetPaymentMethodByIdentifier retrieves a payment method by its unique identifier.
func (m *MongoDB) GetPaymentMethodByIdentifier(ctx context.Context, identifier string) (*entity.PaymentMethod, error) {
	collection := m.client.Database(m.database).Collection(collectionPaymentMethods)
	filter := bson.D{{Key: "identifier", Value: identifier}}
	var paymentMethod *entity.PaymentMethod
	err := collection.FindOne(ctx, filter).Decode(&paymentMethod)
	if err != nil {
		return nil, fmt.Errorf("get payment method by identifier: %w", err)
	}
	return paymentMethod, nil
}

// UpdatePaymentMethodFailCount updates the fail counter for a payment method.
func (m *MongoDB) UpdatePaymentMethodFailCount(ctx context.Context, identifier string, count int) error {
	collection := m.client.Database(m.database).Collection(collectionPaymentMethods)
	filter := bson.D{{Key: "identifier", Value: identifier}}
	update := bson.D{
		{Key: "$set", Value: bson.D{
			{Key: "fail_count", Value: count},
		}},
	}
	if _, err := collection.UpdateOne(ctx, filter, update); err != nil {
		return fmt.Errorf("update payment method fail count: %w", err)
	}
	return nil
}

// GetPaymentOrderByTransaction retrieves an incomplete payment order for a transaction.
func (m *MongoDB) GetPaymentOrderByTransaction(ctx context.Context, transactionId int) (*entity.PaymentOrder, error) {
	collection := m.client.Database(m.database).Collection(collectionPaymentOrders)
	filter := bson.D{{Key: "transaction_id", Value: transactionId}, {Key: "is_completed", Value: false}}
	var order entity.PaymentOrder
	if err := collection.FindOne(ctx, filter).Decode(&order); err != nil {
		return nil, fmt.Errorf("get payment order by transaction %d: %w", transactionId, err)
	}
	return &order, nil
}

// SavePaymentOrder saves or updates a payment order using upsert.
func (m *MongoDB) SavePaymentOrder(ctx context.Context, order *entity.PaymentOrder) error {
	filter := bson.D{{Key: "order", Value: order.Order}}
	set := bson.M{"$set": order}
	collection := m.client.Database(m.database).Collection(collectionPaymentOrders)
	_, err := collection.UpdateOne(ctx, filter, set, options.Update().SetUpsert(true))
	if err != nil {
		return fmt.Errorf("save payment order %d: %w", order.Order, err)
	}
	return nil
}

// GetLastOrder retrieves the most recently opened payment order.
func (m *MongoDB) GetLastOrder(ctx context.Context) (*entity.PaymentOrder, error) {
	collection := m.client.Database(m.database).Collection(collectionPaymentOrders)
	filter := bson.D{}
	var order entity.PaymentOrder
	if err := collection.FindOne(ctx, filter, options.FindOne().SetSort(bson.D{{Key: "time_opened", Value: -1}})).Decode(&order); err != nil {
		return nil, fmt.Errorf("get last order: %w", err)
	}
	return &order, nil
}

// NewMongoClient creates a new MongoDB client with a persistent connection pool.
// The client maintains an active connection to MongoDB and should be reused throughout
// the application lifecycle. Call Disconnect() when the application shuts down.
func NewMongoClient(conf *config.Config) (*MongoDB, error) {
	if !conf.Mongo.Enabled {
		return nil, nil
	}

	ctx := context.Background()
	connectionUri := fmt.Sprintf("mongodb://%s:%s", conf.Mongo.Host, conf.Mongo.Port)
	clientOptions := options.Client().ApplyURI(connectionUri)

	if conf.Mongo.User != "" {
		clientOptions.SetAuth(options.Credential{
			Username:   conf.Mongo.User,
			Password:   conf.Mongo.Password,
			AuthSource: conf.Mongo.Database,
		})
	}

	// Establish connection once at startup
	client, err := mongo.Connect(ctx, clientOptions)
	if err != nil {
		return nil, fmt.Errorf("connect to mongodb: %w", err)
	}

	// Verify connection is working
	if err = client.Ping(ctx, nil); err != nil {
		client.Disconnect(ctx)
		return nil, fmt.Errorf("ping mongodb: %w", err)
	}

	return &MongoDB{
		client:           client,
		database:         conf.Mongo.Database,
		logRecordsNumber: conf.LogRecords,
	}, nil
}

// Disconnect closes the MongoDB connection gracefully.
// This should be called when the application shuts down.
func (m *MongoDB) Disconnect() error {
	if m.client == nil {
		return nil
	}
	ctx := context.Background()
	return m.client.Disconnect(ctx)
}

// WriteLogMessage writes a log message to the database.
func (m *MongoDB) WriteLogMessage(ctx context.Context, data services.Data) error {
	collection := m.client.Database(m.database).Collection(collectionLog)
	_, err := collection.InsertOne(ctx, data)
	if err != nil {
		return fmt.Errorf("write log message: %w", err)
	}
	return nil
}

// GetUserTag retrieves a user tag (RFID card) by its identifier.
func (m *MongoDB) GetUserTag(ctx context.Context, id string) (*entity.UserTag, error) {
	filter := bson.D{{Key: "id_tag", Value: id}}
	collection := m.client.Database(m.database).Collection(collectionUserTags)
	var userTag entity.UserTag
	err := collection.FindOne(ctx, filter).Decode(&userTag)
	if err != nil {
		return nil, fmt.Errorf("get user tag %s: %w", id, err)
	}
	return &userTag, nil
}

// SavePaymentResult stores a payment response from Redsys for audit purposes.
func (m *MongoDB) SavePaymentResult(ctx context.Context, paymentParameters *entity.PaymentParameters) error {
	collection := m.client.Database(m.database).Collection(collectionPayment)
	_, err := collection.InsertOne(ctx, paymentParameters)
	if err != nil {
		return fmt.Errorf("save payment result: %w", err)
	}
	return nil
}

// GetPaymentOrder retrieves a payment order by its order number.
func (m *MongoDB) GetPaymentOrder(ctx context.Context, id int) (*entity.PaymentOrder, error) {
	collection := m.client.Database(m.database).Collection(collectionPaymentOrders)
	filter := bson.D{{Key: "order", Value: id}}
	var order entity.PaymentOrder
	if err := collection.FindOne(ctx, filter).Decode(&order); err != nil {
		return nil, fmt.Errorf("get payment order %d: %w", id, err)
	}
	return &order, nil
}

// UpdateTransaction updates transaction payment billing data.
func (m *MongoDB) UpdateTransaction(ctx context.Context, transaction *entity.Transaction) error {
	collection := m.client.Database(m.database).Collection(collectionTransactions)
	filter := bson.D{{Key: "transaction_id", Value: transaction.Id}}
	update := bson.D{
		{Key: "$set", Value: bson.D{
			{Key: "payment_order", Value: transaction.PaymentOrder},
			{Key: "payment_error", Value: transaction.PaymentError},
			{Key: "payment_billed", Value: transaction.PaymentBilled},
			{Key: "payment_orders", Value: transaction.PaymentOrders},
		}},
	}
	if _, err := collection.UpdateOne(ctx, filter, update); err != nil {
		return fmt.Errorf("update transaction %d: %w", transaction.Id, err)
	}
	return nil
}

// GetPaymentMethodById retrieves a payment method by identifier and user ID.
func (m *MongoDB) GetPaymentMethodById(ctx context.Context, identifier, userId string) (*entity.PaymentMethod, error) {
	collection := m.client.Database(m.database).Collection(collectionPaymentMethods)
	filter := bson.D{{Key: "identifier", Value: identifier}, {Key: "user_id", Value: userId}}
	var paymentMethod entity.PaymentMethod
	if err := collection.FindOne(ctx, filter).Decode(&paymentMethod); err != nil {
		return nil, fmt.Errorf("get payment method by id: %w", err)
	}
	return &paymentMethod, nil
}

// SavePaymentMethod saves a new payment method to the database.
// Returns an error if a payment method with the same identifier already exists for the user.
func (m *MongoDB) SavePaymentMethod(ctx context.Context, paymentMethod *entity.PaymentMethod) error {
	saved, _ := m.GetPaymentMethodById(ctx, paymentMethod.Identifier, paymentMethod.UserId)
	if saved != nil {
		return fmt.Errorf("payment method with identifier %s... already exists", paymentMethod.Identifier[0:10])
	}

	collection := m.client.Database(m.database).Collection(collectionPaymentMethods)
	_, err := collection.InsertOne(ctx, paymentMethod)
	if err != nil {
		return fmt.Errorf("save payment method: %w", err)
	}
	return nil
}
