# Phase 1 -- Control Plane Foundation

## Objectives

Phase 1 focused on building the **Control Plane** foundation for the
distributed load testing platform. No workers or distributed execution
were introduced in this phase. The goal was to establish a
production-inspired backend architecture that can evolve incrementally.

### Scope

-   Bootstrap the Go project
-   Configure PostgreSQL
-   Introduce migrations with Goose
-   Build a layered architecture
-   Implement the first set of APIs
-   Build reusable API infrastructure
-   Store only test metadata

------------------------------------------------------------------------

# Architecture

    HTTP Request
          │
          ▼
     Handler
          │
          ▼
     Service
          │
          ▼
     Repository
          │
          ▼
     PostgreSQL

Responsibilities:

-   **Handler** -- HTTP parsing and response generation.
-   **Service** -- Business logic and validation.
-   **Repository** -- Database interaction.
-   **Database** -- Persistent metadata.

------------------------------------------------------------------------

# Technology Stack

  Component    Technology
  ------------ ------------
  Language     Go
  Router       Chi
  Database     PostgreSQL
  Driver       pgx
  Migrations   Goose
  Logging      slog

------------------------------------------------------------------------

# Project Structure

``` text
cmd/
    server/

internal/
    api/
        handlers/
        response/
        router.go

    apperrors/
    config/
    constants/
    db/
    models/
    repository/
    service/
    validation/

migrations/

Makefile
.env
```

------------------------------------------------------------------------

# Database

Table:

``` sql
CREATE TABLE tests (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    status TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);
```

Only metadata is stored.

Workers and metrics are intentionally excluded.

------------------------------------------------------------------------

# Migration Strategy

Sequential Goose migrations.

Example:

``` text
00001_create_tests_table.sql
00002_...
```

Commands:

``` bash
make migrate-up
make migrate-status
```

------------------------------------------------------------------------

# API Foundation

Implemented:

-   JSON response helper
-   Standard error helper
-   Validation helper
-   Sentinel application errors
-   Chi middleware
    -   RequestID
    -   Logger
    -   Recoverer
    -   Timeout

`RealIP` was intentionally omitted because forwarded headers can be
spoofed unless running behind a trusted reverse proxy.

API versioning:

    /api/v1

------------------------------------------------------------------------

# Test Lifecycle

    CREATED
       │
    STARTING
       │
    RUNNING
       │
    STOPPING
       │
    STOPPED

    or

    RUNNING
       │
    COMPLETED

Only CREATED and STOPPED are currently exercised.

------------------------------------------------------------------------

# API Endpoints

## Health

    GET /health

``` bash
curl http://localhost:8080/health
```

Response:

``` json
{
  "status":"ok"
}
```

------------------------------------------------------------------------

## Create Test

    POST /api/v1/tests

``` json
{
  "name":"Homepage Load Test"
}
```

``` bash
curl -X POST http://localhost:8080/api/v1/tests \
-H "Content-Type: application/json" \
-d '{"name":"Homepage Load Test"}'
```

Returns **201 Created**.

------------------------------------------------------------------------

## List Tests

    GET /api/v1/tests

``` bash
curl http://localhost:8080/api/v1/tests
```

Returns every test ordered by newest first.

------------------------------------------------------------------------

## Get Test

    GET /api/v1/tests/{id}

``` bash
curl http://localhost:8080/api/v1/tests/<id>
```

Returns **404** if the test does not exist.

------------------------------------------------------------------------

## Stop Test

    POST /api/v1/tests/{id}/stop

``` bash
curl -X POST http://localhost:8080/api/v1/tests/<id>/stop
```

Returns **204 No Content**.

Current implementation simply updates the status.

Future versions will notify workers before transitioning to STOPPED.

------------------------------------------------------------------------

# Design Decisions

-   Layered architecture.
-   Raw SQL instead of an ORM.
-   Repository pattern.
-   Service layer owns business rules.
-   PostgreSQL stores metadata only.
-   Workers never communicate with the database.
-   API contracts evolve incrementally.
-   Database schema evolves phase by phase.
-   No DTOs yet to avoid premature abstraction.
-   Sentinel errors for type-safe error handling.
-   Chi middleware used for common infrastructure instead of custom
    implementations.

------------------------------------------------------------------------

# Makefile

Common targets:

``` bash
make run
make migrate-up
make migrate-status
make psql
make fmt
make tidy
```

------------------------------------------------------------------------

# Manual Verification

1.  Run migrations.
2.  Start server.
3.  Create a test.
4.  List tests.
5.  Fetch by ID.
6.  Stop the test.
7.  Verify updated status.

------------------------------------------------------------------------

# Phase 1 Checklist

-   [x] Project bootstrap
-   [x] Configuration
-   [x] PostgreSQL connection
-   [x] Goose migrations
-   [x] Sequential migrations
-   [x] Makefile
-   [x] Layered architecture
-   [x] Create Test
-   [x] List Tests
-   [x] Get Test
-   [x] Stop Test
-   [x] Response helper
-   [x] Error helper
-   [x] Validation helper
-   [x] Middleware
-   [x] API versioning
-   [x] Sentinel errors

------------------------------------------------------------------------

# Next Phase

Phase 2 introduces Worker Management.

Planned work:

-   Worker registration
-   Worker heartbeat
-   Worker lifecycle
-   Worker state transitions
-   Worker metadata
-   Timeout detection
-   Scheduler integration

Implementation approach:

1.  Design
2.  Discuss trade-offs
3.  Finalize schema and APIs for the phase
4.  Implement incrementally
