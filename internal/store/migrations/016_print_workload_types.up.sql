-- 016_print_workload_types.up.sql
-- Registers print_traditional and print_3d with the PostgreSQL
-- workload_type enum so the dispatcher can write print jobs to the
-- jobs table. Schema-only; no existing rows reference these values.
-- Dispatcher logic (B4 commit 3), opt-out filtering, and agent
-- routing already reference these strings in application code and
-- will become active once the enum values exist.
ALTER TYPE workload_type ADD VALUE IF NOT EXISTS 'print_traditional';
ALTER TYPE workload_type ADD VALUE IF NOT EXISTS 'print_3d';
