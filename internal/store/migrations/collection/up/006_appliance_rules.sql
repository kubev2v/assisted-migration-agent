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
    name VARCHAR NOT NULL,
    vendor VARCHAR NOT NULL,
    category VARCHAR NOT NULL,
    confidence UTINYINT NOT NULL,
    min_matched INTEGER NOT NULL DEFAULT 1,
    osv_verdict UTINYINT NOT NULL DEFAULT 3,
    CHECK (min_matched >= 1)
);

-- A condition is identified either by expression or by func_name, never both
-- and never neither. Expression conditions carry a filter DSL expression and no
-- values. Func conditions carry the registered predicate name in func_name and
-- store the predicate values in exactly one value column.
CREATE SEQUENCE IF NOT EXISTS rule_condition_seq START 1;
CREATE TABLE IF NOT EXISTS rule_conditions (
    id INTEGER PRIMARY KEY DEFAULT nextval('rule_condition_seq'),
    name VARCHAR NOT NULL,
    expression VARCHAR,
    func_name VARCHAR,
    tier UTINYINT NOT NULL,
    bool_value BOOLEAN,
    string_value VARCHAR,
    int_value INTEGER,
    rule_id INTEGER REFERENCES rules(id),
    CHECK (
        (expression IS NOT NULL AND func_name IS NULL)
        OR (expression IS NULL AND func_name IS NOT NULL)
    ),
    CHECK (
        (expression IS NOT NULL AND bool_value IS NULL AND string_value IS NULL AND int_value IS NULL)
        OR (expression IS NULL AND (
            (bool_value IS NOT NULL AND string_value IS NULL AND int_value IS NULL)
            OR (bool_value IS NULL AND string_value IS NOT NULL AND int_value IS NULL)
            OR (bool_value IS NULL AND string_value IS NULL AND int_value IS NOT NULL)
        ))
    )
);
