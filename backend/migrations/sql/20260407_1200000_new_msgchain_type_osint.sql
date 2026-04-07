-- +goose Up
-- +goose StatementBegin
ALTER TYPE MSGCHAIN_TYPE ADD VALUE IF NOT EXISTS 'osint';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Postgres does not support removing values from an enum directly.
-- To roll back, recreate the enum without 'osint' (mirrors the pattern in
-- 20250103_1215631_new_msgchain_type_fixer.sql).
DELETE FROM msgchains WHERE type = 'osint';

ALTER TABLE msgchains ALTER COLUMN type DROP DEFAULT;
ALTER TABLE agentlogs ALTER COLUMN initiator DROP DEFAULT;
ALTER TABLE agentlogs ALTER COLUMN executor DROP DEFAULT;
ALTER TABLE vecstorelogs ALTER COLUMN initiator DROP DEFAULT;
ALTER TABLE vecstorelogs ALTER COLUMN executor DROP DEFAULT;
ALTER TABLE searchlogs ALTER COLUMN initiator DROP DEFAULT;
ALTER TABLE searchlogs ALTER COLUMN executor DROP DEFAULT;

CREATE TYPE MSGCHAIN_TYPE_NEW AS ENUM (
  'primary_agent',
  'reporter',
  'generator',
  'refiner',
  'reflector',
  'enricher',
  'adviser',
  'coder',
  'memorist',
  'searcher',
  'installer',
  'pentester',
  'summarizer',
  'tool_call_fixer',
  'assistant'
);

ALTER TABLE msgchains
    ALTER COLUMN type TYPE MSGCHAIN_TYPE_NEW USING type::text::MSGCHAIN_TYPE_NEW;

ALTER TABLE agentlogs
    ALTER COLUMN initiator TYPE MSGCHAIN_TYPE_NEW USING initiator::text::MSGCHAIN_TYPE_NEW,
    ALTER COLUMN executor TYPE MSGCHAIN_TYPE_NEW USING executor::text::MSGCHAIN_TYPE_NEW;

ALTER TABLE vecstorelogs
    ALTER COLUMN initiator TYPE MSGCHAIN_TYPE_NEW USING initiator::text::MSGCHAIN_TYPE_NEW,
    ALTER COLUMN executor TYPE MSGCHAIN_TYPE_NEW USING executor::text::MSGCHAIN_TYPE_NEW;

ALTER TABLE searchlogs
    ALTER COLUMN initiator TYPE MSGCHAIN_TYPE_NEW USING initiator::text::MSGCHAIN_TYPE_NEW,
    ALTER COLUMN executor TYPE MSGCHAIN_TYPE_NEW USING executor::text::MSGCHAIN_TYPE_NEW;

DROP TYPE MSGCHAIN_TYPE;

ALTER TYPE MSGCHAIN_TYPE_NEW RENAME TO MSGCHAIN_TYPE;

ALTER TABLE msgchains
    ALTER COLUMN type SET NOT NULL,
    ALTER COLUMN type SET DEFAULT 'primary_agent';

ALTER TABLE agentlogs
    ALTER COLUMN initiator SET NOT NULL,
    ALTER COLUMN initiator SET DEFAULT 'primary_agent',
    ALTER COLUMN executor SET NOT NULL,
    ALTER COLUMN executor SET DEFAULT 'primary_agent';

ALTER TABLE vecstorelogs
    ALTER COLUMN initiator SET NOT NULL,
    ALTER COLUMN initiator SET DEFAULT 'primary_agent',
    ALTER COLUMN executor SET NOT NULL,
    ALTER COLUMN executor SET DEFAULT 'primary_agent';

ALTER TABLE searchlogs
    ALTER COLUMN initiator SET NOT NULL,
    ALTER COLUMN initiator SET DEFAULT 'primary_agent',
    ALTER COLUMN executor SET NOT NULL,
    ALTER COLUMN executor SET DEFAULT 'primary_agent';
-- +goose StatementEnd