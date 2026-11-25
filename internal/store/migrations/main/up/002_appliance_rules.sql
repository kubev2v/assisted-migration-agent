CREATE SEQUENCE IF NOT EXISTS rule_id_seq START 1;

-- confidence
-- 1 low, 2 medium, 3 high

-- osv_verdict
-- OsvBlocked      0
-- OsvConditional  1
-- OsvSupported    2
-- OsvUnknown      3

CREATE TABLE IF NOT EXISTS rules (
    id INTEGER PRIMARY KEY DEFAULT nextval('rule_id_seq'),
    name VARCHAR NOT NULL UNIQUE,
    vendor VARCHAR NOT NULL,
    kind UTINYINT DEFAULT 0,
    category VARCHAR NOT NULL,
    confidence UTINYINT NOT NULL,
    min_matched INTEGER NOT NULL DEFAULT 1,
    osv_verdict UTINYINT NOT NULL DEFAULT 3,
    CHECK (min_matched >= 1)
);

-- A condition is identified either by expression or by func_name, never both
-- and never neither. Expression conditions carry a filter DSL expression and no
-- values. Func conditions carry the registered predicate name in func_name and
-- store their payload in disk_geometry_condition.
--
-- rule_condition_id is unique because a condition carries exactly one payload.
--
-- There are no foreign keys on purpose: DuckDB has no ON DELETE CASCADE and it
-- checks references against committed state, so deleting a parent whose
-- children are deleted in the same transaction fails. Callers delete children
-- first, inside one transaction.
CREATE SEQUENCE IF NOT EXISTS rule_condition_seq START 1;
CREATE TABLE IF NOT EXISTS rule_conditions (
    id INTEGER PRIMARY KEY DEFAULT nextval('rule_condition_seq'),
    name VARCHAR NOT NULL,
    expression VARCHAR,
    func_name VARCHAR,
    tier UTINYINT NOT NULL,
    rule_id INTEGER NOT NULL,
    CHECK (
        (expression IS NOT NULL AND func_name IS NULL)
        OR (expression IS NULL AND func_name IS NOT NULL)
    )
);

CREATE SEQUENCE IF NOT EXISTS disk_geometry_condition_id START 1;
CREATE TABLE IF NOT EXISTS disk_geometry_condition (
    id INTEGER PRIMARY KEY DEFAULT nextval('disk_geometry_condition_id'),
    disks BIGINT[] NOT NULL,
    tolerance INTEGER NOT NULL,
    mode VARCHAR NOT NULL,
    rule_condition_id INTEGER NOT NULL UNIQUE
);
