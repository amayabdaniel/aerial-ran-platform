-- aerial-ran-platform: bootstrap schemas
-- One database, one schema per service. Each service owns its schema only.

CREATE SCHEMA IF NOT EXISTS iam;
CREATE SCHEMA IF NOT EXISTS subscriber;
CREATE SCHEMA IF NOT EXISTS esim;
CREATE SCHEMA IF NOT EXISTS provision;
CREATE SCHEMA IF NOT EXISTS ranctl;
CREATE SCHEMA IF NOT EXISTS billing;
CREATE SCHEMA IF NOT EXISTS messaging;

-- Extensions used by services
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
CREATE EXTENSION IF NOT EXISTS "pgcrypto";
CREATE EXTENSION IF NOT EXISTS "pg_stat_statements";
