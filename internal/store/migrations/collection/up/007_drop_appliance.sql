-- The 006 migrations creates the tables for rules in the wrong db.
-- It should be main because rules don't depend on collected data.
DROP TABLE IF EXISTS rule_conditions;
DROP TABLE IF EXISTS rules;

DROP SEQUENCE IF EXISTS rule_condition_seq;
DROP SEQUENCE IF EXISTS rule_id_seq;
