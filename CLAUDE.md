# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Electrum is a high-performance payment service implementation using the Redsys payment platform. It handles payment transactions for electric vehicle charging sessions, managing payment methods, transaction processing, and integrations with MongoDB for persistence. The service is designed for concurrent operation with proper timeout handling and request tracing.

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

### Verification
```bash
# Run go vet (should have zero warnings)
go vet ./...

# Run tests
go test ./...

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

**main.go**: Application entry point that initializes the server, payment service, database connection, and logger with graceful shutdown handling.

**internal/**: Core business logic
- `server.go`: HTTP server with httprouter handling REST endpoints and request ID injection
- `payments.go`: Main payment processing logic with concurrent operation support
- `mongo.go`: MongoDB database implementation with persistent connection pooling
- `encryptor.go`: Cryptographic functions for Redsys signature generation (3DES + HMAC-SHA256, PKCS#7 padding)
- `logger.go`: Logging service with optional MongoDB persistence
- `request_id.go`: Request ID generation and context propagation for tracing

**entity/**: Data models
- `transaction.go`: Electric vehicle charging transaction with payment tracking (value-type mutex for thread safety)
- `payment_order.go`: Individual payment order linked to transactions
- `payment_method.go`: Stored payment method with tokenization (COF - Credential on File)
- `payment_parameters.go`: Redsys request/response parameters
- `merchant_parameters.go`: Merchant-specific payment parameters

**services/**: Interface definitions (all methods accept `context.Context` for timeout/cancellation)
- `payments_service.go`: Payment operations interface
- `database_service.go`: Database operations interface
- `log_handler.go`: Logging interface

**config/**: Configuration management using cleanenv library with YAML and environment variable support

### Payment Flow Architecture

1. **Request Initiation**: HTTP request arrives with generated request ID for tracing
2. **Context Propagation**: Request context flows through all layers with timeout (30s for external calls)
3. **Transaction Processing** (`PayTransaction`):
   - **Per-transaction locking**: Allows concurrent payments for different transactions
   - Retrieves user tag to get user ID (with context timeout)
   - Fetches payment method (prioritizes default, falls back to method with lowest fail count)
   - Creates payment order with incremental order number
   - Encrypts parameters and creates signature using merchant secret
   - Sends payment request to Redsys API (async with timeout and panic recovery)
4. **Payment Response** (`processResponse`):
   - Executes asynchronously with panic recovery
   - Validates response codes (0000 for authorization, 0900 for refund)
   - Updates transaction with `PaymentBilled` amount (accumulated)
   - Tracks payment errors and fail counts on payment methods
   - Stores payment method tokens (COF) for future use
5. **Payment Method Fallback**: If transaction has errors or payment method has issues, attempts to load alternative payment method from database

### Key Design Patterns

**Context-Based Request Flow**: All operations accept `context.Context` as first parameter, enabling:
- Timeout control (30s for external API calls, configurable for DB operations)
- Request cancellation propagation
- Request ID tracing through the entire stack

**Fine-Grained Locking**: Per-transaction/order locking using `sync.Map`:
- Allows unlimited concurrent payments for different transactions
- Prevents race conditions on same transaction
- Automatic cleanup to prevent memory leaks
- Eliminates global serialization bottleneck

**Goroutine Lifecycle Management**:
- All goroutines have timeout protection (30s default)
- Panic recovery with error logging
- Context-aware HTTP requests for proper cancellation
- No unbounded goroutine spawning

**Request ID Tracking**:
- Unique ID generated for each HTTP request
- Propagated through context to all operations
- Logged with all messages for end-to-end tracing
- Format: `[request_id]` prefix in logs

**Async Request Processing**: Payment API calls are executed in managed goroutines to avoid blocking HTTP handlers while maintaining safety.

**Fail Counter Strategy**: Payment methods track `FailCount` to automatically switch to more reliable payment methods when issues occur.

**Base64 + HMAC Signature**: Redsys requires Base64-encoded JSON parameters with HMAC-SHA256 signature using 3DES-encrypted order number with PKCS#7 padding.

**Persistent Connection Pooling**: MongoDB client established once at startup and reused (10-100x performance improvement).

**Singleton Configuration**: Config is loaded once using sync.Once pattern with environment variable override support.

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

**MIT (Merchant Initiated Transaction) Flow**:

This service implements MIT transactions for recurring EV charging payments:

1. **Initial Authorization (CIT)**: First payment requires cardholder participation with 3D-Secure authentication
   - Customer authorizes card storage during initial transaction
   - Redsys returns a network transaction ID (DS_MERCHANT_COF_TXNID)
   - Payment method and COF_TXNID are stored for future use

2. **Subsequent Charges (MIT)**: Future charging sessions use stored credentials without cardholder interaction
   - System automatically charges for completed charging sessions
   - No redirect or 3D-Secure challenge required
   - Uses MIT exemption under PSD2 regulations

**COF (Credential on File) Parameters for MIT Transactions**:
- `DS_MERCHANT_COF_INI`: N (not initial - subsequent transaction using stored credentials; S = initial)
- `DS_MERCHANT_COF_TYPE`: R (Recurring payments with variable amounts and defined intervals)
  - R = Recurring (EV charging sessions with variable amounts)
  - I = Installments (fixed amounts, fixed intervals)
  - C = Others (one-time miscellaneous transactions)
- `DS_MERCHANT_COF_TXNID`: Network transaction ID from the initial cardholder authorization
- `DS_MERCHANT_DIRECTPAYMENT`: true (direct payment using stored token without cardholder redirect)
- `DS_MERCHANT_EXCEP_SCA`: MIT (Merchant Initiated Transaction exemption per PSD2)
  - Required for all merchant-initiated payments without cardholder participation
  - Ensures transactions are not declined due to missing SCA authentication

## Important Notes

- Payment amount accumulation: `PaymentBilled` tracks total amount successfully charged; new payment attempts only charge `PaymentAmount - PaymentBilled`
- Order numbers are sequential and must be unique across the system
- Payment methods with `FailCount > 0` trigger fallback to alternative methods
- The `secret` function redacts sensitive data in logs (shows first 5 chars + ***)
- All async operations (payment requests, response processing) are executed in goroutines
