-- +goose Up
CREATE INDEX IF NOT EXISTS change_set_committed_at_idx ON change_set (committed_at DESC, public_id DESC);
CREATE INDEX IF NOT EXISTS change_set_actor_idx ON change_set (actor);
CREATE INDEX IF NOT EXISTS change_set_operation_type_idx ON change_set (operation_type);
CREATE INDEX IF NOT EXISTS change_set_item_public_id_idx ON change_set_item (public_id);

-- +goose Down
DROP INDEX IF EXISTS change_set_item_public_id_idx;
DROP INDEX IF EXISTS change_set_operation_type_idx;
DROP INDEX IF EXISTS change_set_actor_idx;
DROP INDEX IF EXISTS change_set_committed_at_idx;
