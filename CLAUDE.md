# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Electrum is a payment service implementation using the Redsys payment platform. It handles payment transactions for electric vehicle charging sessions, managing payment methods, transaction processing, and integrations with MongoDB for persistence.

## Build and Development Commands

### Building
```bash
go build -v -o electrum
```

### Running
```bash
# Run with default config (config.yml)
go run main.go

# Run with custom config
go run main.go -conf path/to/config.yml

# Run built binary
./electrum -conf config.yml
```

### Testing
```bash
# Run all tests
go test ./...

# Run tests for a specific package
go test ./internal

# Run tests with verbose output
go test -v ./...
```

### Dependencies
```bash
# Download dependencies
go mod download

# Tidy dependencies
go mod tidy
```

## Architecture Overview

### Core Components

**main.go**: Application entry point that initializes the server, payment service, database connection, and logger.

**internal/**: Core business logic
- `server.go`: HTTP server with httprouter handling REST endpoints
- `payments.go`: Main payment processing logic including Redsys API integration
- `mongo.go`: MongoDB database implementation
- `encryptor.go`: Cryptographic functions for Redsys signature generation (3DES + HMAC-SHA256)
- `logger.go`: Logging service with optional MongoDB persistence

**entity/**: Data models
- `transaction.go`: Electric vehicle charging transaction with payment tracking
- `payment_order.go`: Individual payment order linked to transactions
- `payment_method.go`: Stored payment method with tokenization (COF - Credential on File)
- `payment_parameters.go`: Redsys request/response parameters
- `merchant_parameters.go`: Merchant-specific payment parameters

**services/**: Interface definitions
- `payments_service.go`: Payment operations interface
- `database_service.go`: Database operations interface
- `log_handler.go`: Logging interface

**config/**: Configuration management using cleanenv library with YAML support

### Payment Flow Architecture

1. **Transaction Creation**: Charging transactions are created with `PaymentAmount` representing the total amount to be charged
2. **Payment Processing** (`PayTransaction`):
   - Retrieves user tag to get user ID
   - Fetches payment method (prioritizes default, falls back to method with lowest fail count)
   - Creates payment order with incremental order number
   - Encrypts parameters and creates signature using merchant secret
   - Sends payment request to Redsys API (async via goroutine)
3. **Payment Response** (`processResponse`):
   - Validates response codes (0000 for authorization, 0900 for refund)
   - Updates transaction with `PaymentBilled` amount (accumulated)
   - Tracks payment errors and fail counts on payment methods
   - Stores payment method tokens (COF) for future use
4. **Payment Method Fallback**: If transaction has errors or payment method has issues, attempts to load alternative payment method from database

### Key Design Patterns

**Mutex-based Concurrency Control**: Both `Payments` and `Transaction` use mutexes to prevent race conditions during payment processing.

**Async Request Processing**: Payment API calls are executed in goroutines to avoid blocking the HTTP handlers.

**Fail Counter Strategy**: Payment methods track `FailCount` to automatically switch to more reliable payment methods when issues occur.

**Base64 + HMAC Signature**: Redsys requires Base64-encoded JSON parameters with HMAC-SHA256 signature using 3DES-encrypted order number.

**Singleton Configuration**: Config is loaded once using sync.Once pattern.

## API Endpoints

- `GET /pay/:transaction_id` - Initiate payment for a charging transaction
- `GET /return/:transaction_id` - Process refund for a transaction
- `POST /return/order/:order_id` - Process partial/full refund by order ID (requires JSON body with `amount`)
- `POST /notify` - Webhook endpoint for Redsys payment notifications

## Configuration

Configuration is loaded from YAML files (default: `config.yml`). Template: `electrum.yml`.

Key configuration sections:
- `is_debug`: Enable debug logging
- `disable_payment`: Bypass Redsys calls for testing (marks transactions as paid)
- `listen`: Server binding (IP, port, TLS settings)
- `mongo`: MongoDB connection parameters
- `merchant`: Redsys merchant credentials (secret, code, terminal, request_url)

## MongoDB Collections

- `payment_log`: Application logs
- `user_tags`: User RFID tags linked to user accounts
- `transactions`: Charging session transactions with payment status
- `payment_methods`: Stored payment methods with tokenization
- `payment_orders`: Individual payment orders (one transaction may have multiple orders)
- `payment`: Raw payment results from Redsys

## Redsys Integration Details

**Transaction Types**:
- Type 0: Authorization (payment)
- Type 3: Refund

**Expected Response Codes**:
- 0000: Successful authorization
- 0900: Successful refund
- SIS####: Error codes from Redsys

**COF (Credential on File) Parameters**:
- `DS_MERCHANT_COF_INI`: N (not initial, using stored method)
- `DS_MERCHANT_COF_TYPE`: C (CIT - Cardholder Initiated Transaction)
- `DS_MERCHANT_COF_TXNID`: Transaction ID from previous tokenization
- `DS_MERCHANT_DIRECTPAYMENT`: true (direct payment without redirect)
- `DS_MERCHANT_EXCEP_SCA`: MIT (Merchant Initiated Transaction exemption)

## Important Notes

- Payment amount accumulation: `PaymentBilled` tracks total amount successfully charged; new payment attempts only charge `PaymentAmount - PaymentBilled`
- Order numbers are sequential and must be unique across the system
- Payment methods with `FailCount > 0` trigger fallback to alternative methods
- The `secret` function redacts sensitive data in logs (shows first 5 chars + ***)
- All async operations (payment requests, response processing) are executed in goroutines
