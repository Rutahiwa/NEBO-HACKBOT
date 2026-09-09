-- +goose Up
ALTER TYPE msgchain_type ADD VALUE IF NOT EXISTS 'recon';
ALTER TYPE msgchain_type ADD VALUE IF NOT EXISTS 'injection';
ALTER TYPE msgchain_type ADD VALUE IF NOT EXISTS 'xss';
ALTER TYPE msgchain_type ADD VALUE IF NOT EXISTS 'auth';
ALTER TYPE msgchain_type ADD VALUE IF NOT EXISTS 'idor';
ALTER TYPE msgchain_type ADD VALUE IF NOT EXISTS 'ssrf';
ALTER TYPE msgchain_type ADD VALUE IF NOT EXISTS 'validator';

-- +goose Down
-- PostgreSQL does not support removing enum values; no-op on downgrade.
